// Copyright 2026 NetBox Labs, Inc.

// Package mapping — module_routing.go: per-VC-member dispatch for the
// module / module bay inventory built by extractModuleInventory. Walks
// each module's containedIn chain until it hits a class=3 chassis, then
// maps that chassis EntPhysicalIndex to the owning ChassisInventory
// member id. Sits between extractModuleInventory and the translation
// step so the extractor stays chassis-topology-agnostic.
package mapping

import (
	"log/slog"
	"strings"
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
