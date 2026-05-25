// Copyright 2026 NetBox Labs, Inc.

// Package mapping — module_routing.go: per-VC-member dispatch for the
// module / module bay inventory built by extractModuleInventory. Walks
// each module's containedIn chain until it hits a class=3 chassis, then
// maps that chassis EntPhysicalIndex to the owning ChassisInventory
// member id. Sits between extractModuleInventory and the translation
// step so the extractor stays chassis-topology-agnostic.
package mapping

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// assignMemberID stamps each ModuleEntry in inv with the logical member
// id that owns it.
//
//   - chassisInv == nil OR no members -> standalone target; every entry
//     lands on MemberID=0 so the translation step emits modules under
//     the master device.
//   - VC / stack target -> walk the entPhysicalContainedIn chain from
//     each module's EntIndex until reaching a chassis EntPhysicalIndex
//     present in chassisInv.Members. Stamp the corresponding member id.
//     Modules whose chain terminates at a chassis NOT in chassisInv get
//     the sentinel MemberID=-1 plus a warn log; the translation step
//     skips MemberID<0 entries.
//
// The cycle guard mirrors extractModuleInventory.walkParents — a `seen`
// set bounded by the chain length suffices since the containedIn
// relation is meant to be a DAG (malformed MIBs notwithstanding).
func assignMemberID(inv *ModuleInventory, chassisInv *ChassisInventory, oids ObjectIDValueMap, logger *slog.Logger) {
	if inv == nil {
		return
	}

	// Standalone fast path — nothing to walk.
	if chassisInv == nil || len(chassisInv.Members) == 0 {
		for i := range inv.Modules {
			inv.Modules[i].MemberID = 0
		}
		for parent, list := range inv.SubModules {
			for i := range list {
				list[i].MemberID = 0
			}
			inv.SubModules[parent] = list
		}
		for i := range inv.EmptyBays {
			inv.EmptyBays[i].MemberID = 0
		}
		return
	}

	entIdxToMemberID := make(map[string]int, len(chassisInv.Members))
	for _, m := range chassisInv.Members {
		entIdxToMemberID[m.EntPhysicalIndex] = m.ID
	}

	resolve := func(startEnt string) (int, bool) {
		// Walk containedIn until we hit a class=3 ent that lives in the
		// chassisInv member map. seen guards a malformed self-/mutual-
		// referential containedIn pair from looping forever.
		cur := strings.TrimSpace(oids[oidEntPhysicalContainedIn+startEnt].Value)
		seen := make(map[string]struct{})
		for cur != "" && cur != "0" {
			if _, dup := seen[cur]; dup {
				return 0, false
			}
			seen[cur] = struct{}{}
			class := strings.TrimSpace(oids[oidEntPhysicalClass+cur].Value)
			if class == entPhysicalClassChassis {
				id, ok := entIdxToMemberID[cur]
				return id, ok
			}
			cur = strings.TrimSpace(oids[oidEntPhysicalContainedIn+cur].Value)
		}
		return 0, false
	}

	stamp := func(e *ModuleEntry) {
		id, ok := resolve(e.EntIndex)
		if !ok {
			logger.Warn("module discovery: orphan module — chassis ancestor not in chassis inventory",
				"ent", e.EntIndex, "model", e.Model)
			e.MemberID = -1
			if c := metrics.GetModulesDropped(); c != nil {
				c.Add(context.Background(), 1, metric.WithAttributes(
					attribute.String("reason", "orphan_member"),
				))
			}
			return
		}
		e.MemberID = id
	}

	for i := range inv.Modules {
		stamp(&inv.Modules[i])
	}
	for parent, list := range inv.SubModules {
		for i := range list {
			stamp(&list[i])
		}
		inv.SubModules[parent] = list
	}
	for i := range inv.EmptyBays {
		stamp(&inv.EmptyBays[i])
	}
}

// buildIfaceModuleMap builds the {ifName -> *diode.Module} lookup the
// runner needs to set Interface.Module on each transceiver-owning port.
//
// Only transceivers under inv.SubModules participate — top-level modules
// (linecards, supervisors) aren't single-port entities and don't route
// to an ifName. For each transceiver:
//
//  1. aliasMap[EntIndex] gives the ifIndex (as set up upstream by the
//     entAliasMappingTable walk).
//  2. ifIndexToName[ifIndex] gives the canonical ifName.
//  3. emittedModules[EntIndex] is the *diode.Module the translator
//     already produced; this map points the ifName at it.
//
// Any step that misses simply skips the transceiver — partial coverage
// is normal (optic with no alias-table row, port-channel placeholder, etc.).
func buildIfaceModuleMap(
	inv ModuleInventory,
	aliasMap map[string]string,
	ifIndexToName map[string]string,
	emittedModules map[string]*diode.Module,
) map[string]*diode.Module {
	out := make(map[string]*diode.Module)
	for _, list := range inv.SubModules {
		for _, e := range list {
			if e.Type != ModuleTypeTransceiver {
				continue
			}
			ifIdx, ok := aliasMap[e.EntIndex]
			if !ok {
				continue
			}
			ifName, ok := ifIndexToName[ifIdx]
			if !ok {
				continue
			}
			mod, ok := emittedModules[e.EntIndex]
			if !ok {
				continue
			}
			out[ifName] = mod
		}
	}
	return out
}

// AliasMapFromOIDs parses entAliasMappingTable rows into a flat
// entPhysicalIndex -> ifIndex map (decimal strings). Mirrors the parse
// rules in chassis_routing.go:152-179 (drop malformed suffixes; drop
// non-ifEntry.ifIndex values) but emits the simpler shape the module
// path consumes — the chassis router does its own per-ifIndex
// candidate ranking, so first-occurrence-wins here is harmless.
func AliasMapFromOIDs(oids ObjectIDValueMap) map[string]string {
	out := make(map[string]string)
	for oid, v := range oids {
		if !strings.HasPrefix(oid, oidEntAliasMappingIdent) {
			continue
		}
		suffix := strings.TrimPrefix(oid, oidEntAliasMappingIdent)
		parts := strings.SplitN(suffix, ".", 2)
		if len(parts) != 2 {
			continue
		}
		entIdx := parts[0]
		// Normalize: strip leading dot (gosnmp's ObjectIdentifier
		// rendering varies) then require the value to point at
		// ifEntry.ifIndex — skip ifAlias / ifDescr targets.
		val := strings.TrimPrefix(strings.TrimSpace(v.Value), ".")
		if !strings.HasPrefix(val, oidIfEntryIfIndexNoDot) {
			continue
		}
		ifIdx := strings.TrimPrefix(val, oidIfEntryIfIndexNoDot)
		if _, err := strconv.Atoi(ifIdx); err != nil {
			continue
		}
		if _, exists := out[entIdx]; !exists {
			out[entIdx] = ifIdx
		}
	}
	return out
}

// IfNameByIfIndex inverts the runner's *Interface -> ifIndex map into
// ifIndex (decimal string) -> ifName. Interfaces with nil Name are
// skipped — a transceiver cannot route to a nameless port.
func IfNameByIfIndex(ifIndexByIface map[*diode.Interface]int) map[string]string {
	out := make(map[string]string, len(ifIndexByIface))
	for iface, idx := range ifIndexByIface {
		if iface == nil || iface.Name == nil {
			continue
		}
		out[strconv.Itoa(idx)] = *iface.Name
	}
	return out
}

// MemberDevicesFromEntities groups the Devices in entities by member id
// for module dispatch. Master (VcPosition == nil) is keyed by the lowest
// member id in chassisInv.Members[0].ID — mirroring TranslateAsStack's
// memberByID convention (chassis.go:432). For standalone targets
// (chassisInv nil/empty) the master falls back to key 0. Non-master
// members are keyed by *VcPosition.
func MemberDevicesFromEntities(
	entities []diode.Entity,
	chassisInv *ChassisInventory,
) map[int]*diode.Device {
	out := make(map[int]*diode.Device)
	masterID := 0
	if chassisInv != nil && len(chassisInv.Members) > 0 {
		// Members are sorted ascending by ID upstream;
		// Members[0].ID is the master's logical member id.
		masterID = chassisInv.Members[0].ID
	}
	for _, e := range entities {
		dev, ok := e.(*diode.Device)
		if !ok || dev == nil {
			continue
		}
		if dev.VcPosition != nil {
			out[int(*dev.VcPosition)] = dev
			continue
		}
		// First master wins — defensive against duplicate emission.
		if _, exists := out[masterID]; !exists {
			out[masterID] = dev
		}
	}
	return out
}
