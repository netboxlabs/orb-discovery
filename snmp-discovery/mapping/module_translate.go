// Copyright 2026 NetBox Labs, Inc.

// Package mapping — module_translate.go: Diode entity emission for
// module / module bay (+ ModuleType) entities. Public entry point is
// TranslateModulesWithAlias; TranslateModules is a thin wrapper for
// callers that don't carry alias data.
//
// Emission rules:
//   - "off" mode short-circuits before extraction (zero behaviour change).
//   - "linecards" mode emits chassis-slot modules + their bays only.
//     PSU / Fan are classified for labelling but never emitted as
//     module entities. Transceivers are skipped entirely.
//   - "full" mode adds transceiver sub-bays (device-rooted — see
//     sub-bay reconciler workaround at the emission site) and empty
//     bays, and populates the iface->Module attachment map.
package mapping

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TranslateModules is a thin wrapper for callers without alias data —
// the iface attachment map will be empty.
func TranslateModules(
	oids ObjectIDValueMap,
	chassisInv *ChassisInventory,
	memberDevices map[int]*diode.Device,
	options *config.Options,
	defaults *config.Defaults,
	logger *slog.Logger,
) ([]diode.Entity, map[string]*diode.Module) {
	return TranslateModulesWithAlias(oids, chassisInv, memberDevices, options, defaults, logger, nil, nil)
}

// TranslateModulesWithAlias is the full-fidelity entry point used by
// the runner. Returns (entities, ifaceModuleMap):
//   - entities: every ModuleBay + Module emitted, in extraction order.
//   - ifaceModuleMap: in "full" mode, {ifName -> *Module} so the
//     runner can attach Interface.Module on physical ports. nil in
//     "linecards" mode.
//
// Returns (nil, nil) when:
//   - mode == "off"
//   - the ENTITY-MIB walk produced no modules and no bays at all
func TranslateModulesWithAlias(
	oids ObjectIDValueMap,
	chassisInv *ChassisInventory,
	memberDevices map[int]*diode.Device,
	options *config.Options,
	defaults *config.Defaults,
	logger *slog.Logger,
	aliasMap map[string]string,
	ifIndexToName map[string]string,
) ([]diode.Entity, map[string]*diode.Module) {
	mode := options.ModuleDiscoveryMode()
	if mode == "off" {
		return nil, nil
	}

	inv := extractModuleInventory(oids, logger)
	if len(inv.Modules) == 0 && len(inv.SubModules) == 0 && len(inv.EmptyBays) == 0 {
		return nil, nil
	}

	assignMemberID(&inv, chassisInv, oids, logger)

	var entities []diode.Entity
	emittedModules := make(map[string]*diode.Module, len(inv.Modules))

	for _, m := range inv.Modules {
		// PSU / Fan are classified for labelling only — never emitted as
		// module entities (mirrors device-discovery PR #419).
		if m.Type == ModuleTypePSU || m.Type == ModuleTypeFan {
			continue
		}
		// assignMemberID stamps MemberID=-1 on entries whose chassis
		// ancestor isn't in the VC member set. Skip — already warn-logged.
		if m.MemberID < 0 {
			continue
		}
		device := memberDevices[m.MemberID]
		if device == nil {
			logger.Warn("module discovery: no device for member",
				"member", m.MemberID, "ent", m.EntIndex, "model", m.Model)
			continue
		}
		bay := emitModuleBay(device, m)
		entities = append(entities, bay)
		if c := metrics.GetModuleBaysEmitted(); c != nil {
			c.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("vendor", vendorFromDevice(device)),
			))
		}
		mod := emitModule(device, bay, m, defaults)
		entities = append(entities, mod)
		if c := metrics.GetModulesEmitted(); c != nil {
			c.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("vendor", vendorFromDevice(device)),
				attribute.String("type", string(m.Type)),
			))
		}
		emittedModules[m.EntIndex] = mod
	}

	if mode != "full" {
		// Linecards mode stops here — no transceivers, no empty bays,
		// no iface attachment map.
		return entities, nil
	}

	// Full-mode-only: transceiver sub-bays + empty bays + iface routing.
	//
	// Walk EVERY key in inv.SubModules — not just those keyed by
	// top-level inv.Modules entries. Vendors like Juniper nest optics
	// two module-levels below the chassis (Chassis -> FPC -> PIC -> optic),
	// so the optic's parent class=9 (the PIC) is itself a sub-module
	// stored under inv.SubModules[FPC.EntIndex]. Iterating only top-
	// level parents silently dropped those optics. Each transceiver
	// already carries MemberID (stamped by assignMemberID), so device
	// routing remains correct regardless of nesting depth. The
	// emittedModules guard prevents the (theoretical) double-emit if
	// the same EntIndex is reachable via two parents.
	subKeys := make([]string, 0, len(inv.SubModules))
	for k := range inv.SubModules {
		subKeys = append(subKeys, k)
	}
	sort.Strings(subKeys)
	for _, parentIdx := range subKeys {
		for _, tr := range inv.SubModules[parentIdx] {
			if tr.Type != ModuleTypeTransceiver {
				continue
			}
			if tr.MemberID < 0 {
				continue
			}
			if _, dup := emittedModules[tr.EntIndex]; dup {
				continue
			}
			device := memberDevices[tr.MemberID]
			if device == nil {
				continue
			}
			// Sub-bay reconciler workaround (spec §Sub-bay emission
			// workaround): emit transceiver sub-bays DEVICE-ROOTED
			// (no Module=parent_linecard link). Linking the sub-bay to
			// its parent linecard makes the Diode reconciler re-plan
			// the parent inside the sub-bay's changeset and trip
			// dcim_module_module_bay_id_key on apply. Restore the link
			// when the upstream reconciler resolves nested parent-
			// module refs against committed sibling entities.
			subBay := emitModuleBay(device, tr)
			entities = append(entities, subBay)
			if c := metrics.GetModuleBaysEmitted(); c != nil {
				c.Add(context.Background(), 1, metric.WithAttributes(
					attribute.String("vendor", vendorFromDevice(device)),
				))
			}

			mod := emitModule(device, subBay, tr, defaults)
			entities = append(entities, mod)
			if c := metrics.GetModulesEmitted(); c != nil {
				c.Add(context.Background(), 1, metric.WithAttributes(
					attribute.String("vendor", vendorFromDevice(device)),
					attribute.String("type", string(tr.Type)),
				))
			}
			emittedModules[tr.EntIndex] = mod
		}
	}

	// Empty bays — class=5 rows with no class=9 child. Bare ModuleBay
	// only; no Module entity.
	for _, b := range inv.EmptyBays {
		if b.MemberID < 0 {
			continue
		}
		device := memberDevices[b.MemberID]
		if device == nil {
			continue
		}
		entities = append(entities, emitModuleBay(device, b))
		if c := metrics.GetModuleBaysEmitted(); c != nil {
			c.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("vendor", vendorFromDevice(device)),
			))
		}
	}

	ifaceMap := buildIfaceModuleMap(inv, aliasMap, ifIndexToName, emittedModules)
	return entities, ifaceMap
}

// emitModuleBay constructs a ModuleBay entity for a top-level
// (chassis-slot) module. Always carries Device — the chassis device is
// the matching scope for both ModuleBay and Module per Diode docs.
func emitModuleBay(device *diode.Device, m ModuleEntry) *diode.ModuleBay {
	name := m.BayName
	if name == "" {
		// Bay rows occasionally arrive without a name; fall back to
		// position so we never ship an empty-string required field
		// (Diode rejects "").
		name = m.BayPosition
	}
	if name == "" {
		name = "Unknown"
	}
	bay := &diode.ModuleBay{
		Device: device,
		Name:   &name,
	}
	if m.BayPosition != "" {
		pos := m.BayPosition
		bay.Position = &pos
	}
	return bay
}

// emitModule constructs a Module entity attached to its ModuleBay.
// Carries Device (NetBox matching scope) and a ModuleType built from
// the PID (Model) + the manufacturer resolved from the emitted Device.
// Manufacturer precedence: Device.DeviceType.Manufacturer.Name first
// (so the ModuleType label always matches what NetBox sees on the
// owning device), then the policy-level defaults, finally "Unknown".
// Sharing vendorFromDevice with the metrics path keeps the label and
// the emitted entity identical strings.
func emitModule(device *diode.Device, bay *diode.ModuleBay, m ModuleEntry, defaults *config.Defaults) *diode.Module {
	model := modelOrUnknown(m.Model)
	mfgName := resolveModuleManufacturer(device, defaults)
	moduleType := &diode.ModuleType{
		Model: &model,
		Manufacturer: &diode.Manufacturer{
			Name: &mfgName,
		},
	}
	mod := &diode.Module{
		Device:     device,
		ModuleBay:  bay,
		ModuleType: moduleType,
	}
	if m.Serial != "" {
		serial := m.Serial
		mod.Serial = &serial
	}
	if m.Description != "" {
		desc := m.Description
		mod.Description = &desc
	}
	return mod
}

// vendorFromDevice resolves the per-device manufacturer name for metric
// attribution. Reads the already-set DeviceType.Manufacturer.Name on the
// emitted Device — that's the same value emitModule uses for the
// ModuleType, so the counter labels stay consistent with what NetBox sees.
// Falls back to "Unknown" on any nil/blank in the chain so a missing
// pointer never produces an empty-string attribute.
func vendorFromDevice(d *diode.Device) string {
	if d == nil || d.DeviceType == nil || d.DeviceType.Manufacturer == nil || d.DeviceType.Manufacturer.Name == nil {
		return "Unknown"
	}
	if name := strings.TrimSpace(*d.DeviceType.Manufacturer.Name); name != "" {
		return name
	}
	return "Unknown"
}

// modelOrUnknown trims model and substitutes "Unknown" for the empty
// string. Diode rejects empty strings for required ModuleType.Model.
func modelOrUnknown(m string) string {
	if m == "" {
		return "Unknown"
	}
	return m
}

// vendorFromDefaults returns the policy-level device manufacturer or
// "Unknown" when unset. Diode rejects empty strings for required
// Manufacturer.Name.
func vendorFromDefaults(d *config.Defaults) string {
	if d == nil || d.Device.Manufacturer == "" {
		return "Unknown"
	}
	return d.Device.Manufacturer
}

// resolveModuleManufacturer picks the Manufacturer name to stamp on an
// emitted ModuleType. Precedence:
//  1. The emitted Device's DeviceType.Manufacturer.Name — keeps the
//     ModuleType label identical to the vendor attribute used by the
//     OTLP counters (vendorFromDevice).
//  2. The policy-level defaults.device.manufacturer — fallback for the
//     rare path where the device entity lacks a manufacturer.
//  3. "Unknown" — Diode rejects empty strings on this required field.
func resolveModuleManufacturer(device *diode.Device, defaults *config.Defaults) string {
	if v := vendorFromDevice(device); v != "Unknown" {
		return v
	}
	return vendorFromDefaults(defaults)
}
