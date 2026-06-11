package mapping

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// VRF discovery table columns, tried in tiers until one yields rows.
// Walk-result keys carry no leading dot.
const (
	// Tier 1 — MPLS-L3VPN-STD-MIB (RFC 4382): the standards path.
	// mplsL3VpnVrfRD is indexed by the VRF name (length-prefixed octets)
	// and its value is the route distinguisher; mplsL3VpnIfConfRowStatus
	// is indexed by vrfName + ifIndex and carries the membership.
	oidMplsL3VpnVrfRD  = "1.3.6.1.2.1.10.166.11.1.2.2.1.4"
	oidMplsL3VpnIfConf = "1.3.6.1.2.1.10.166.11.1.1.1.1.2"
	// Tier 2 — the pre-standard MPLS-VPN-MIB (experimental arc), same
	// table shapes; still common on older Cisco IOS.
	oidMplsVpnVrfRDLegacy  = "1.3.6.1.3.118.1.2.2.1.3"
	oidMplsVpnIfConfLegacy = "1.3.6.1.3.118.1.2.1.1.2"
	// Tier 3 — CISCO-VRF-MIB for VRF-lite platforms without the MPLS
	// MIBs. cvVrfName is indexed by an integer VRF id;
	// cvVrfInterfaceType is indexed by vrfId + ifIndex. No RD here.
	oidCvVrfName      = "1.3.6.1.4.1.9.9.711.1.1.1.1.2"
	oidCvVrfInterface = "1.3.6.1.4.1.9.9.711.1.2.1.1.2"
)

// Display-form route distinguishers some agents return instead of the
// RFC 4382 8-byte encoding: "65000:100", "10.1.1.1:55".
var vrfDisplayRdRe = regexp.MustCompile(`^(?:\d+|\d+\.\d+\.\d+\.\d+):\d+$`)

type vrfRecord struct {
	name      string
	rd        string
	ifIndexes map[int]struct{}
}

// VrfMapper satisfies the orbToEntityMapper interface for the "vrf"
// pseudo-entity. Map is a no-op: VRF rows are consumed wholesale by the
// runner-level TranslateVrfs pass via the raw oids map, mirroring the
// chassis_inventory / chassis_module pseudo-mappers.
type VrfMapper struct {
	logger *slog.Logger
}

// Map is the row-scoped no-op required by the orbToEntityMapper interface.
func (m *VrfMapper) Map(
	_ map[ObjectIDIndex]*ObjectIDValue,
	_ *Entry,
	_ *EntityRegistry,
	_ *config.Defaults,
) diode.Entity {
	return nil
}

// TranslateVrfs derives VRF entities and an ifIndex→VRF map from the raw
// walk results, trying the three MIB tiers in order until one yields rows.
// VRF entities are returned sorted by name for deterministic emission; the
// same *diode.VRF pointers appear in the map so attached references and
// standalone entities reconcile identically.
func TranslateVrfs(
	oids ObjectIDValueMap,
	defaults *config.Defaults,
	logger *slog.Logger,
) ([]diode.Entity, map[int]*diode.VRF) {
	records := collectNameIndexedVrfs(oids, oidMplsL3VpnVrfRD, oidMplsL3VpnIfConf, logger)
	if len(records) == 0 {
		records = collectNameIndexedVrfs(oids, oidMplsVpnVrfRDLegacy, oidMplsVpnIfConfLegacy, logger)
	}
	if len(records) == 0 {
		records = collectCiscoVrfs(oids, logger)
	}
	if len(records) == 0 {
		return nil, nil
	}

	var tags []*diode.Tag
	if defaults != nil {
		for _, t := range defaults.Tags {
			tagName := t
			tags = append(tags, &diode.Tag{Name: &tagName})
		}
	}

	names := make([]string, 0, len(records))
	for name := range records {
		names = append(names, name)
	}
	sort.Strings(names)

	entities := make([]diode.Entity, 0, len(records))
	byIfIndex := make(map[int]*diode.VRF)
	for _, name := range names {
		rec := records[name]
		vrfName := rec.name
		vrf := &diode.VRF{Name: &vrfName}
		if rec.rd != "" {
			rd := rec.rd
			vrf.Rd = &rd
		}
		if len(tags) > 0 {
			vrf.Tags = tags
		}
		entities = append(entities, vrf)
		for ifIndex := range rec.ifIndexes {
			byIfIndex[ifIndex] = vrf
		}
	}
	return entities, byIfIndex
}

// AttachVrfs overwrites IPAddress VRF references for addresses whose
// assigned interface belongs to a discovered VRF. Discovered membership is
// device state and wins over the configured defaults (which remain the
// fallback for every other address) — mirroring device-discovery's
// precedence. Interfaces outside the map are left untouched.
func AttachVrfs(
	entities []diode.Entity,
	vrfByIfIndex map[int]*diode.VRF,
	ifIndexByIface map[*diode.Interface]int,
) {
	if len(vrfByIfIndex) == 0 || len(ifIndexByIface) == 0 {
		return
	}
	for _, e := range entities {
		ip, ok := e.(*diode.IPAddress)
		if !ok {
			continue
		}
		iface, ok := ip.AssignedObject.(*diode.Interface)
		if !ok {
			continue
		}
		idx, ok := ifIndexByIface[iface]
		if !ok {
			continue
		}
		if vrf, hit := vrfByIfIndex[idx]; hit {
			ip.Vrf = vrf
		}
	}
}

// collectNameIndexedVrfs reads one RD column (indexed by the VRF name as
// length-prefixed octets) and one IfConf column (indexed by vrfName +
// ifIndex). VRFs appearing only in the membership table are still emitted
// (RD absent) so a partial agent implementation can't hide a VRF.
func collectNameIndexedVrfs(
	oids ObjectIDValueMap,
	rdColumn, ifConfColumn string,
	logger *slog.Logger,
) map[string]*vrfRecord {
	records := make(map[string]*vrfRecord)
	get := func(name string) *vrfRecord {
		rec, ok := records[name]
		if !ok {
			rec = &vrfRecord{name: name, ifIndexes: make(map[int]struct{})}
			records[name] = rec
		}
		return rec
	}
	for oid, value := range oids {
		if suffix, ok := oidSuffix(oid, rdColumn); ok {
			name, decoded := decodeOctetStringIndex(suffix, 0)
			if !decoded {
				logger.Debug("vrf: undecodable VRF-name index, skipping row",
					"oid", oid)
				continue
			}
			rec := get(name)
			rec.rd = decodeRouteDistinguisher(value.Value, logger)
			continue
		}
		if suffix, ok := oidSuffix(oid, ifConfColumn); ok {
			name, tail, decoded := decodeOctetStringIndexWithTail(suffix, 1)
			if !decoded {
				logger.Debug("vrf: undecodable IfConf index, skipping row",
					"oid", oid)
				continue
			}
			get(name).ifIndexes[tail[0]] = struct{}{}
		}
	}
	return records
}

// collectCiscoVrfs reads CISCO-VRF-MIB: cvVrfName (integer VRF id index →
// name) and cvVrfInterfaceType (vrfId + ifIndex index). Membership rows
// whose vrfId has no name row are dropped — there is nothing to attach.
func collectCiscoVrfs(oids ObjectIDValueMap, logger *slog.Logger) map[string]*vrfRecord {
	namesByID := make(map[int]string)
	membersByID := make(map[int]map[int]struct{})
	for oid, value := range oids {
		if suffix, ok := oidSuffix(oid, oidCvVrfName); ok {
			id, err := strconv.Atoi(suffix)
			if err != nil {
				logger.Debug("vrf: non-integer cvVrfId index, skipping row", "oid", oid)
				continue
			}
			if name := value.Value; name != "" {
				namesByID[id] = name
			}
			continue
		}
		if suffix, ok := oidSuffix(oid, oidCvVrfInterface); ok {
			parts := strings.SplitN(suffix, ".", 2)
			if len(parts) != 2 {
				continue
			}
			id, err1 := strconv.Atoi(parts[0])
			ifIndex, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				logger.Debug("vrf: undecodable cvVrfInterface index, skipping row", "oid", oid)
				continue
			}
			if membersByID[id] == nil {
				membersByID[id] = make(map[int]struct{})
			}
			membersByID[id][ifIndex] = struct{}{}
		}
	}
	records := make(map[string]*vrfRecord, len(namesByID))
	for id, name := range namesByID {
		rec := &vrfRecord{name: name, ifIndexes: make(map[int]struct{})}
		for ifIndex := range membersByID[id] {
			rec.ifIndexes[ifIndex] = struct{}{}
		}
		records[name] = rec
	}
	return records
}

// oidSuffix returns the index portion of oid under the given table column,
// tolerating a leading dot on either side.
func oidSuffix(oid, column string) (string, bool) {
	oid = strings.TrimPrefix(oid, ".")
	column = strings.TrimPrefix(column, ".")
	if !strings.HasPrefix(oid, column+".") {
		return "", false
	}
	return oid[len(column)+1:], true
}

// decodeOctetStringIndex decodes an SNMP octet-string table index from its
// OID suffix form. See decodeOctetStringIndexWithTail.
func decodeOctetStringIndex(suffix string, _ int) (string, bool) {
	name, _, ok := decodeOctetStringIndexWithTail(suffix, 0)
	return name, ok
}

// decodeOctetStringIndexWithTail decodes a length-prefixed octet-string
// index followed by tailLen plain integer sub-identifiers (e.g. the
// mplsL3VpnIfConf index: vrfName + ifIndex). When the leading
// sub-identifier doesn't look like a valid length prefix, the IMPLIED
// (unprefixed) encoding is tried as a fallback — agents disagree here.
// All name octets must be printable ASCII; anything else fails the row.
func decodeOctetStringIndexWithTail(suffix string, tailLen int) (string, []int, bool) {
	parts := strings.Split(suffix, ".")
	ints := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", nil, false
		}
		ints[i] = n
	}
	if len(ints) < 1+tailLen {
		return "", nil, false
	}
	// Length-prefixed form: first sub-identifier is the octet count.
	if n := ints[0]; n > 0 && len(ints) == 1+n+tailLen {
		if name, ok := octetsToString(ints[1 : 1+n]); ok {
			return name, ints[1+n:], true
		}
	}
	// IMPLIED form: every leading sub-identifier is a name octet.
	if name, ok := octetsToString(ints[:len(ints)-tailLen]); ok {
		return name, ints[len(ints)-tailLen:], true
	}
	return "", nil, false
}

func octetsToString(octets []int) (string, bool) {
	if len(octets) == 0 {
		return "", false
	}
	b := make([]byte, len(octets))
	for i, o := range octets {
		if o < 0x20 || o > 0x7e {
			return "", false
		}
		b[i] = byte(o)
	}
	return string(b), true
}

// decodeRouteDistinguisher normalizes an RD value to its display form.
// Agents return either the display string directly or the RFC 4382 8-byte
// encoding (2-byte type, then type-specific fields). Unset / undecodable
// RDs return "" so the rd field stays off the wire and the VRF matches
// NetBox records whose rd column is null.
func decodeRouteDistinguisher(raw string, logger *slog.Logger) string {
	if raw == "" {
		return ""
	}
	if vrfDisplayRdRe.MatchString(raw) {
		return raw
	}
	if len(raw) != 8 {
		logger.Debug("vrf: unrecognized RD form, emitting VRF without rd",
			"length", len(raw))
		return ""
	}
	b := []byte(raw)
	allZero := true
	for _, x := range b {
		if x != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return ""
	}
	rdType := int(b[0])<<8 | int(b[1])
	switch rdType {
	case 0: // 2-byte ASN : 4-byte assigned number
		asn := int(b[2])<<8 | int(b[3])
		num := uint32(b[4])<<24 | uint32(b[5])<<16 | uint32(b[6])<<8 | uint32(b[7])
		return fmt.Sprintf("%d:%d", asn, num)
	case 1: // IPv4 address : 2-byte assigned number
		num := int(b[6])<<8 | int(b[7])
		return fmt.Sprintf("%d.%d.%d.%d:%d", b[2], b[3], b[4], b[5], num)
	case 2: // 4-byte ASN : 2-byte assigned number
		asn := uint32(b[2])<<24 | uint32(b[3])<<16 | uint32(b[4])<<8 | uint32(b[5])
		num := int(b[6])<<8 | int(b[7])
		return fmt.Sprintf("%d:%d", asn, num)
	default:
		logger.Debug("vrf: unknown RD type, emitting VRF without rd", "type", rdType)
		return ""
	}
}
