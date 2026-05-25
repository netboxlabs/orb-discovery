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
	"log/slog"

	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
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

	manufacturer := vendorFromDefaults(defaults)

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
		mod := emitModule(device, bay, m, manufacturer)
		entities = append(entities, mod)
		emittedModules[m.EntIndex] = mod
	}

	// Full-mode-only: transceiver sub-bays + empty bays + iface routing
	// land in Task 10. Linecards mode stops here.
	return entities, nil
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
// the PID (Model) + the policy-level manufacturer.
func emitModule(device *diode.Device, bay *diode.ModuleBay, m ModuleEntry, manufacturer string) *diode.Module {
	model := modelOrUnknown(m.Model)
	mfgName := manufacturer
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
