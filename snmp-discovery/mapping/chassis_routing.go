package mapping

import (
	"log/slog"
	"regexp"
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

// Cisco IOS / IOS-XE / NX-OS stack-style names that begin with a known
// physical-port type prefix followed by digits/0/digits (or digits/digits).
// Captures the leading member id.
var cisco3TupleRe = regexp.MustCompile(
	`^(?:Gi(?:gabitEthernet)?|Te(?:nGigE|nGigabitEthernet)?|Twe(?:ntyFiveGigE|ntyFiveGigabitEthernet)?|Fo(?:rtyGigE|rtyGigabitEthernet)?|Hu(?:ndredGigE|ndredGigabitEthernet)?|Tw(?:oGigabitEthernet)?|Fi(?:veGigabitEthernet)?|mGig|Fa(?:stEthernet)?|Et(?:hernet)?)(\d+)/\d+/\d+$`)

// Junos FPC pattern (xe-X/Y/Z, ge-X/Y/Z, et-X/Y/Z, mge-X/Y/Z, ...).
var junosFpcRe = regexp.MustCompile(`^[a-z]{2,4}-(\d+)/\d+/\d+$`)

// Aruba CX / HP/H3C Comware "X/Y/Z" numeric form (no alpha prefix).
var numeric3TupleRe = regexp.MustCompile(`^(\d+)/\d+/\d+$`)

// HP/H3C dashed long form.
var h3cDashRe = regexp.MustCompile(`^(?:Ten-GigabitEthernet|Forty-GigabitEthernet|Hundred-GigabitEthernet)(\d+)/\d+/\d+$`)

// Names that must route to master regardless of trailing digits.
var masterOnlyPrefixes = []string{
	"Vlan", "Loopback", "Lo", "Port-channel", "Po",
	"Tunnel", "Tu", "BVI", "Bundle-Ether", "Null",
	"mgmt", "Management", "ManagementEthernet",
	"FastEthernet0/0", // non-stack mgmt-only port on some routers
}

// ParseMemberID extracts the leading stack-member id from ifName per
// the vendor conventions listed in chassis_routing.go (Cisco IOS/IOS-XE
// stack 3-tuple incl. mGig families; Junos FPC; Aruba CX / Comware
// numeric 3-tuple; HP/H3C dashed long form). Returns ok=false for
// LAGs, SVIs, loopbacks, tunnels, BVIs, Bundle-Ether, Null, and mgmt
// interfaces — callers should route those to master.
func ParseMemberID(ifName string) (int, bool) {
	if ifName == "" {
		return 0, false
	}
	for _, p := range masterOnlyPrefixes {
		if strings.HasPrefix(ifName, p) {
			return 0, false
		}
	}
	// Strip subinterface suffix (e.g., "Gi2/0/1.100" -> "Gi2/0/1",
	// "xe-2/0/0.0" -> "xe-2/0/0") so the leading member id is reachable
	// by the parent-interface regexes below.
	name := ifName
	if idx := strings.LastIndexByte(name, '.'); idx > 0 {
		unit := name[idx+1:]
		allDigits := unit != "" && strings.IndexFunc(unit, func(r rune) bool {
			return r < '0' || r > '9'
		}) == -1
		if allDigits {
			name = name[:idx]
		}
	}
	for _, re := range []*regexp.Regexp{cisco3TupleRe, h3cDashRe, junosFpcRe, numeric3TupleRe} {
		if m := re.FindStringSubmatch(name); m != nil {
			if id, err := strconv.Atoi(m[1]); err == nil {
				return id, true
			}
		}
	}
	return 0, false
}

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
	// droppedByEntIdx[entPhysicalIndex] = logical member id for chassis
	// rows that were dropped during validation. Used so that an alias-table
	// hit on a dropped chassis row returns the dropped id (triggering the
	// caller's skip-with-warn path) rather than falling through to
	// ParseMemberID which might mis-route the interface to master.
	droppedByEntIdx map[string]int
	// ifIndexToEnt[ifIndex] = entPhysicalIndex carrying that ifIndex.
	ifIndexToEnt map[int]string
	logger       *slog.Logger
}

func newChassisRouter(inv ChassisInventory, oids ObjectIDValueMap, logger *slog.Logger) *chassisRouter {
	r := &chassisRouter{
		inventory:       inv,
		containedIn:     map[string]string{},
		memberByEntIdx:  map[string]int{},
		droppedByEntIdx: map[string]int{},
		ifIndexToEnt:    map[int]string{},
		logger:          logger,
	}
	for _, m := range inv.Members {
		r.memberByEntIdx[m.EntPhysicalIndex] = m.ID
	}
	for ent, id := range inv.DroppedEntIndexes {
		r.droppedByEntIdx[ent] = id
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
		// Also surface dropped chassis rows so the caller's skip-with-warn
		// path fires instead of falling through to ParseMemberID.
		if id, isDropped := r.droppedByEntIdx[ent]; isDropped {
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
