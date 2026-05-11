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
	Members []ChassisMember
}

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
// Member id derivation lives in deriveMemberID (Task 3); for now we
// honor parentRelPos when set and otherwise fall back to ordinal
// position so the standalone case is covered.
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

	for i := range members {
		if members[i].ParentRelPos > 0 {
			members[i].ID = members[i].ParentRelPos
		}
	}
	if len(members) > 0 {
		fillSequentialIDs(members)
	}
	slices.SortFunc(members, func(a, b ChassisMember) int { return a.ID - b.ID })

	return ChassisInventory{Members: members}
}

// fillSequentialIDs assigns 1..N to members whose ID is still zero,
// preserving entPhysicalIndex ascending order.
// STUB: deleted in Task 3 when deriveMemberID takes over full ID precedence.
func fillSequentialIDs(members []ChassisMember) {
	slices.SortFunc(members, func(a, b ChassisMember) int {
		ai, _ := strconv.Atoi(a.EntPhysicalIndex)
		bi, _ := strconv.Atoi(b.EntPhysicalIndex)
		return ai - bi
	})
	next := 1
	for i := range members {
		if members[i].ID == 0 {
			members[i].ID = next
		}
		next = members[i].ID + 1
	}
}
