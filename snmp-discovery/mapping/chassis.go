// Package mapping — chassis.go: detection + translation of SNMP
// virtual-chassis / switch-stack topology into Diode VirtualChassis +
// member Device entities. Reads ENTITY-MIB entPhysicalTable + RFC 6933
// entAliasMappingTable. Vendor-neutral; relies on lowest-member-id
// master pinning for stability across stack-role failovers.
//
// Public entry point: TranslateAsStack (to be added in a later task).
package mapping

import (
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// ChassisInventoryMapper is a no-op orbToEntityMapper. The
// chassis_inventory entity type exists solely so that ENTITY-MIB
// columns and entAliasMappingTable get walked into the runner's
// ObjectIDValueMap. The actual translation happens later in
// TranslateAsStack, which reads the raw oids map directly.
type ChassisInventoryMapper struct {
	logger *slog.Logger
}

// Map is intentionally a no-op — see type doc.
func (m *ChassisInventoryMapper) Map(
	_ map[ObjectIDIndex]*ObjectIDValue,
	_ *Entry,
	_ *EntityRegistry,
	_ *config.Defaults,
) diode.Entity {
	return nil
}

// ChassisMember is one row of ENTITY-MIB entPhysicalTable identified
// as a top-level chassis (class=3, containedIn=0). The ID is the
// derived logical member id (see deriveMemberID); EntPhysicalIndex is
// the raw row index used for entAliasMappingTable chain walks.
type ChassisMember struct {
	ID               int
	EntPhysicalIndex string
	Serial           string
	Model            string
	EntName          string
	ParentRelPos     int
}

// ChassisInventory is the deduped, validated, member-id-sorted set of
// stack members for one target. Len(Members) >= 2 means stack.
type ChassisInventory struct {
	Members    []ChassisMember
	DroppedIDs map[int]struct{}
}

// IsStack reports whether the inventory should trigger VC emission.
func (c ChassisInventory) IsStack() bool { return len(c.Members) >= 2 }

// entPhysical column prefixes — kept as constants so the extractor and
// future enrichers reference the same OIDs.
const (
	oidEntPhysicalContainedIn = ".1.3.6.1.2.1.47.1.1.1.1.4."
	oidEntPhysicalClass       = ".1.3.6.1.2.1.47.1.1.1.1.5."
	oidEntPhysicalParentRel   = ".1.3.6.1.2.1.47.1.1.1.1.6."
	oidEntPhysicalName        = ".1.3.6.1.2.1.47.1.1.1.1.7."
	oidEntPhysicalSerialNum   = ".1.3.6.1.2.1.47.1.1.1.1.11."
	oidEntPhysicalModelName   = ".1.3.6.1.2.1.47.1.1.1.1.13."

	entPhysicalClassChassis = "3"
)

// extractInventory scans oids for class=3 + containedIn=0 entPhysical
// rows with non-empty serial. Returns members sorted ascending by ID.
// Member id derivation uses the full 3-tier precedence in deriveMemberID
// (ParentRelPos > 0 → trailing-int parse of EntName → ordinal fallback).
func extractInventory(oids ObjectIDValueMap, logger *slog.Logger) ChassisInventory {
	candidates := []string{}
	for oid, v := range oids {
		if !strings.HasPrefix(oid, oidEntPhysicalClass) {
			continue
		}
		if strings.TrimSpace(v.Value) != entPhysicalClassChassis {
			continue
		}
		idx := strings.TrimPrefix(oid, oidEntPhysicalClass)
		candidates = append(candidates, idx)
	}

	members := make([]ChassisMember, 0, len(candidates))
	for _, idx := range candidates {
		contained := strings.TrimSpace(oids[oidEntPhysicalContainedIn+idx].Value)
		if contained != "0" {
			continue
		}
		serial := strings.TrimSpace(oids[oidEntPhysicalSerialNum+idx].Value)
		if serial == "" {
			logger.Warn("chassis row dropped: empty serial",
				"entPhysicalIndex", idx)
			continue
		}
		parentRel, _ := strconv.Atoi(strings.TrimSpace(oids[oidEntPhysicalParentRel+idx].Value))
		members = append(members, ChassisMember{
			EntPhysicalIndex: idx,
			Serial:           serial,
			Model:            strings.TrimSpace(oids[oidEntPhysicalModelName+idx].Value),
			EntName:          strings.TrimSpace(oids[oidEntPhysicalName+idx].Value),
			ParentRelPos:     parentRel,
		})
	}

	// Sort by entPhysicalIndex first so the ordinal fallback is
	// deterministic when neither parentRelPos nor entPhysicalName
	// provides an id signal.
	slices.SortFunc(members, func(a, b ChassisMember) int {
		ai, _ := strconv.Atoi(a.EntPhysicalIndex)
		bi, _ := strconv.Atoi(b.EntPhysicalIndex)
		return ai - bi
	})
	for i := range members {
		members[i].ID = deriveMemberID(members[i], i+1)
	}
	// Dedup pass 1: drop later-occurring duplicates of the same serial,
	// keep the lowest-id occurrence. Track dropped ids for the routing
	// warn-and-skip rule.
	dropped := map[int]struct{}{}
	bySerial := map[string]int{}
	survivors := members[:0]
	for _, m := range members {
		if existing, ok := bySerial[m.Serial]; ok {
			// Keep the lower id, drop the higher id.
			keep, drop := existing, m.ID
			if m.ID < existing {
				keep, drop = m.ID, existing
				// rewrite the survivor we already appended
				for i := range survivors {
					if survivors[i].Serial == m.Serial {
						survivors[i] = m
						break
					}
				}
			}
			bySerial[m.Serial] = keep
			dropped[drop] = struct{}{}
			logger.Warn("chassis row dropped: duplicate serial",
				"serial", m.Serial, "kept_id", keep, "dropped_id", drop)
			continue
		}
		bySerial[m.Serial] = m.ID
		survivors = append(survivors, m)
	}
	members = survivors

	// Dedup pass 2: same id with different serials -> drop all
	// occurrences of that id (ambiguous -> refuse to emit).
	byID := map[int][]ChassisMember{}
	for _, m := range members {
		byID[m.ID] = append(byID[m.ID], m)
	}
	survivors = members[:0]
	for _, group := range byID {
		if len(group) > 1 {
			id := group[0].ID
			dropped[id] = struct{}{}
			logger.Warn("chassis row dropped: ambiguous duplicate member id",
				"id", id, "count", len(group))
			continue
		}
		survivors = append(survivors, group[0])
	}
	members = survivors

	return ChassisInventory{
		Members:    sortByID(members),
		DroppedIDs: dropped,
	}
}

func sortByID(members []ChassisMember) []ChassisMember {
	slices.SortFunc(members, func(a, b ChassisMember) int { return a.ID - b.ID })
	return members
}

var trailingIntRe = regexp.MustCompile(`(\d+)\s*$`)

// deriveMemberID picks the logical member id with precedence:
//  1. ParentRelPos when > 0 (ENTITY-MIB standard signal)
//  2. Trailing integer in EntName ("Switch 1", "FPC 0", "Member 7")
//  3. ordinalFallback (the caller-supplied 1-based position after
//     sorting by entPhysicalIndex)
//
// ParentRelPos == 0 deliberately defers to (2)/(3): the column is
// often unpopulated and 0 is the "unknown / not in a relative
// position" sentinel per RFC 6933.
func deriveMemberID(m ChassisMember, ordinalFallback int) int {
	if m.ParentRelPos > 0 {
		return m.ParentRelPos
	}
	if match := trailingIntRe.FindString(m.EntName); match != "" {
		if id, err := strconv.Atoi(strings.TrimSpace(match)); err == nil {
			return id
		}
	}
	return ordinalFallback
}
