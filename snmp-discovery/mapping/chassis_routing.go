package mapping

import (
	"log/slog"
	"strconv"
	"strings"
)

const (
	oidEntAliasMappingIdent = ".1.3.6.1.2.1.47.1.3.2.1.2."
	// ifEntry.ifIndex column — values in entAliasMappingIdent
	// reference this column; the trailing component is the ifIndex
	// they map to. Stored WITHOUT a leading dot so the comparison
	// works against the normalized OID value (gosnmp ObjectIdentifier
	// values may or may not include the leading dot depending on
	// vendor / version).
	oidIfEntryIfIndexNoDot = "1.3.6.1.2.1.2.2.1.1."
)

// chassisRouter answers "which member owns ifIndex N?" by combining
// the parsed inventory with entAliasMappingTable + an entPhysical
// containedIn chain walk. Falls back to ifName parsing when alias
// table data is missing for an ifIndex (see ParseMemberID).
type chassisRouter struct {
	inventory ChassisInventory
	// containedIn[entPhysicalIndex] = parent entPhysicalIndex (per RFC 6933).
	containedIn map[string]string
	// memberByEntIdx[entPhysicalIndex] = logical member id, only
	// populated for chassis rows that survived validation.
	memberByEntIdx map[string]int
	// ifIndexToEnt[ifIndex] = entPhysicalIndex carrying that ifIndex.
	ifIndexToEnt map[int]string
	logger       *slog.Logger
}

func newChassisRouter(inv ChassisInventory, oids ObjectIDValueMap, logger *slog.Logger) *chassisRouter {
	r := &chassisRouter{
		inventory:      inv,
		containedIn:    map[string]string{},
		memberByEntIdx: map[string]int{},
		ifIndexToEnt:   map[int]string{},
		logger:         logger,
	}
	for _, m := range inv.Members {
		r.memberByEntIdx[m.EntPhysicalIndex] = m.ID
	}
	for oid, v := range oids {
		if strings.HasPrefix(oid, oidEntPhysicalContainedIn) {
			ent := strings.TrimPrefix(oid, oidEntPhysicalContainedIn)
			r.containedIn[ent] = strings.TrimSpace(v.Value)
			continue
		}
		if strings.HasPrefix(oid, oidEntAliasMappingIdent) {
			suffix := strings.TrimPrefix(oid, oidEntAliasMappingIdent)
			parts := strings.SplitN(suffix, ".", 2)
			entIdx := parts[0]
			// Normalize the value by stripping any leading dot —
			// gosnmp's ObjectIdentifier rendering varies. Also skip
			// non-ifIndex VariablePointer values (e.g. ifAlias, ifDescr).
			val := strings.TrimPrefix(strings.TrimSpace(v.Value), ".")
			if !strings.HasPrefix(val, oidIfEntryIfIndexNoDot) {
				continue
			}
			ifIdxStr := strings.TrimPrefix(val, oidIfEntryIfIndexNoDot)
			ifIdx, err := strconv.Atoi(ifIdxStr)
			if err != nil {
				continue
			}
			r.ifIndexToEnt[ifIdx] = entIdx
		}
	}
	return r
}

// routeIfIndex returns the owning member id for ifIndex via
// entAliasMappingTable + containedIn chain walk. Returns ok=false
// when the alias table doesn't cover ifIndex OR the chain doesn't
// terminate at an inventoried chassis row.
func (r *chassisRouter) routeIfIndex(ifIndex int) (int, bool) {
	ent, ok := r.ifIndexToEnt[ifIndex]
	if !ok {
		return 0, false
	}
	// Bounded walk: at most 32 hops up the containedIn chain.
	for hop := 0; hop < 32; hop++ {
		if id, isMember := r.memberByEntIdx[ent]; isMember {
			return id, true
		}
		parent, has := r.containedIn[ent]
		if !has || parent == "" || parent == "0" || parent == ent {
			return 0, false
		}
		ent = parent
	}
	return 0, false
}
