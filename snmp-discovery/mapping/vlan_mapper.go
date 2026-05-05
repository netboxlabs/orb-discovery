package mapping

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/mapping/qbridge"
)

// OID prefixes for VlanMapper input.
const (
	oidDot1dBasePortIfIndex         = ".1.3.6.1.2.1.17.1.4.1.2."
	oidDot1qPvid                    = ".1.3.6.1.2.1.17.7.1.4.5.1.1."
	oidDot1qVlanStaticName          = ".1.3.6.1.2.1.17.7.1.4.3.1.1."
	oidDot1qVlanStaticEgressPorts   = ".1.3.6.1.2.1.17.7.1.4.3.1.2."
	oidDot1qVlanStaticUntaggedPorts = ".1.3.6.1.2.1.17.7.1.4.3.1.4."
	oidDot1qVlanStaticRowStatus     = ".1.3.6.1.2.1.17.7.1.4.3.1.5."
	oidIfAdminStatus                = ".1.3.6.1.2.1.2.2.1.7."
	oidIfType                       = ".1.3.6.1.2.1.2.2.1.3."
	// Cisco overlay
	oidCiscoVmVlan        = ".1.3.6.1.4.1.9.9.68.1.2.2.1.2."
	oidCiscoVmVoiceVlanID = ".1.3.6.1.4.1.9.9.68.1.5.1.1."
)

// ifTypeNumericToString maps a small subset of IANAifType numeric values
// to the strings qbridge.isL3Capable understands.
//
// IANAifType correspondences (RFC 2863 / IANA registry):
//
//	6   ethernetCsmacd  — standard Ethernet / Fast Ethernet
//	62  fastEther       — 100Base-TX (distinct from ethernetCsmacd on older gear)
//	117 gigabitEthernet — 1000Base-X / GbE interfaces
//	161 ieee8023adLag   — 802.3ad link aggregation group (LAG / LACP)
var ifTypeNumericToString = map[int]string{
	6:   "ethernetCsmacd",
	62:  "fastEther",
	117: "gigabitEthernet",
	161: "ieee8023adLag",
}

// VlanMapper bridges Q-BRIDGE / Cisco-overlay SNMP rows to diode.VLAN
// emission and *diode.Interface mutation. It is a postPassMapper:
// VlanMapper.Map is a no-op stub; the real work happens in PostMap.
type VlanMapper struct {
	logger  *slog.Logger
	options config.Options
}

// NewVlanMapper constructs a VlanMapper with the given policy options.
func NewVlanMapper(logger *slog.Logger, options config.Options) *VlanMapper {
	return &VlanMapper{logger: logger, options: options}
}

// Map is the row-scoped no-op required by the orbToEntityMapper interface.
func (m *VlanMapper) Map(
	_ map[ObjectIDIndex]*ObjectIDValue,
	_ *Entry,
	_ *EntityRegistry,
	_ *config.Defaults,
) diode.Entity {
	return nil
}

// PostMap performs the host-level VLAN classification pass.
func (m *VlanMapper) PostMap(
	allObjectIDs ObjectIDValueMap,
	registry *EntityRegistry,
	defaults *config.Defaults,
) []diode.Entity {
	gen := m.buildGenericRows(allObjectIDs)
	if len(gen.BasePortToIfIndex) == 0 {
		// No bridge port table — refuse Interface mutation. Still emit
		// VLAN entities below from the static table; they don't need
		// per-port translation.
		m.logger.Warn("vlan: missing dot1dBasePortIfIndex; skipping interface mutations",
			"reason", "bridge-port-translation-unavailable")
		return m.emitVLANs(allObjectIDs, defaults)
	}

	infos, err := qbridge.ExtractGeneric(gen)
	if err != nil {
		m.logger.Warn("vlan: ExtractGeneric failed", "error", err)
		return m.emitVLANs(allObjectIDs, defaults)
	}
	cisco := m.buildCiscoRows(allObjectIDs)
	// Defense in depth: ApplyCisco is a no-op when both Cisco-overlay maps
	// are empty (the runner only walks vendor-scoped OIDs on a Cisco-matched
	// host, so on generic-only hosts we never reach this branch's payload).
	// Skip the call entirely when there's nothing to apply, both for clarity
	// and to keep the no-op explicit.
	if len(cisco.MembershipAccessVlan) > 0 || len(cisco.VoiceVlanByIfIndex) > 0 {
		qbridge.ApplyCisco(infos, cisco)
	}

	// Build VLAN entities first — interface refs link to them by VID.
	vlanEntities := m.emitVLANs(allObjectIDs, defaults)
	vlanByVid := make(map[int]*diode.VLAN, len(vlanEntities))
	for _, e := range vlanEntities {
		v, ok := e.(*diode.VLAN)
		if !ok || v.Vid == nil {
			continue
		}
		vlanByVid[int(*v.Vid)] = v
	}

	// ensureVLAN returns the *diode.VLAN for vid, creating a stub when:
	//   - no static-name entry exists for vid, AND
	//   - options.CreateUnknownVlans is true.
	// This mirrors device-discovery PR #378's translate._ensure_vlan behavior:
	// classic Cisco IOS exposes vmVlan/dot1qPvid VIDs without advertising them
	// via dot1qVlanStaticName, so ports classify as access but vlanByVid is
	// empty — the stub ensures NetBox never receives mode=access with a nil
	// untagged_vlan reference.
	ensureVLAN := func(vid int) *diode.VLAN {
		if existing, ok := vlanByVid[vid]; ok {
			return existing
		}
		if !m.options.CreateUnknownVlans {
			return nil
		}
		stub := &diode.VLAN{
			Vid:  int64Ptr(int64(vid)),
			Name: StringPtr("VLAN" + strconv.Itoa(vid)),
		}
		applyVLANDefaults(stub, defaults)
		vlanByVid[vid] = stub
		vlanEntities = append(vlanEntities, stub)
		return stub
	}

	// Mutate interfaces in place. The registry holds *diode.Interface
	// instances InterfaceMapper produced; we look them up by ifIndex
	// using string-form ifIndex as ObjectIDIndex.
	for ifIndex, info := range infos {
		key := ObjectIDIndex(strconv.Itoa(ifIndex))
		raw := registry.GetEntity(InterfaceEntityType, key)
		iface, ok := raw.(*diode.Interface)
		if !ok || iface == nil {
			continue
		}
		c := qbridge.Classify(*info)
		applyClassification(iface, c, ensureVLAN)
	}
	return vlanEntities
}

// applyClassification mutates iface to carry the classified VLAN refs.
// Modes ModeRouted and ModeUnknown are no-ops (matches PR #378).
// ensureVLAN is called for every referenced VID; it may return nil when
// options.CreateUnknownVlans is false and the VID has no static name entry.
func applyClassification(iface *diode.Interface, c qbridge.Classification, ensureVLAN func(int) *diode.VLAN) {
	mode := classificationToNetboxMode(c.Mode)
	if mode == "" {
		return
	}
	iface.Mode = StringPtr(mode)
	if c.Untagged != nil {
		if v := ensureVLAN(*c.Untagged); v != nil {
			iface.UntaggedVlan = v
		}
	}
	if len(c.Tagged) > 0 {
		tagged := make([]*diode.VLAN, 0, len(c.Tagged))
		for _, vid := range c.Tagged {
			if v := ensureVLAN(vid); v != nil {
				tagged = append(tagged, v)
			}
		}
		if len(tagged) > 0 {
			iface.TaggedVlans = tagged
		}
	}
}

func classificationToNetboxMode(m qbridge.Mode) string {
	switch m {
	case qbridge.ModeAccess:
		return "access"
	case qbridge.ModeTrunk:
		return "tagged"
	case qbridge.ModeTrunkAll:
		return "tagged-all"
	}
	return ""
}

// applyVLANDefaults applies the defaults.VLAN fields (Description, Tags,
// Tenant, Group) to v. It does NOT touch Status — callers that derive status
// from RowStatus handle that themselves; stubs produced by ensureVLAN only
// inherit defaults.VLAN.Status when it is explicitly set.
func applyVLANDefaults(v *diode.VLAN, defaults *config.Defaults) {
	if defaults == nil {
		return
	}
	vd := defaults.VLAN
	if vd.Description != "" {
		v.Description = StringPtr(vd.Description)
	}
	if len(vd.Tags) > 0 {
		tags := make([]*diode.Tag, 0, len(vd.Tags))
		for _, t := range vd.Tags {
			t := t // capture loop variable
			tags = append(tags, &diode.Tag{Name: &t})
		}
		v.Tags = tags
	}
	if vd.Tenant != "" {
		v.Tenant = &diode.Tenant{Name: StringPtr(vd.Tenant)}
	}
	if vd.Group != "" {
		v.Group = &diode.VLANGroup{Name: StringPtr(vd.Group)}
	}
	if vd.Status != "" {
		v.Status = StringPtr(vd.Status)
	}
}

// buildGenericRows extracts Q-BRIDGE + BRIDGE-MIB rows from the host's
// flat ObjectIDValueMap.
func (m *VlanMapper) buildGenericRows(all ObjectIDValueMap) qbridge.GenericRows {
	rows := qbridge.GenericRows{
		BasePortToIfIndex: map[int]int{},
		PortPvid:          map[int]int{},
		VlanEgressPorts:   map[int][]byte{},
		VlanUntaggedPorts: map[int][]byte{},
		IfAdminStatus:     map[int]int{},
		IfTypes:           map[int]string{},
	}
	// dot1qPortVlanTable is INDEX { dot1dBasePort } per RFC 4363, so the OID
	// suffix is a bridge port number, NOT an ifIndex. Collect raw bridge-port-
	// keyed PVIDs first; translate to ifIndex after the loop once
	// BasePortToIfIndex is fully populated.
	bridgePortPvid := map[int]int{}
	for oid, v := range all {
		switch {
		case strings.HasPrefix(oid, oidDot1dBasePortIfIndex):
			bp, ok1 := atoi(strings.TrimPrefix(oid, oidDot1dBasePortIfIndex))
			ifx, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				rows.BasePortToIfIndex[bp] = ifx
			}
		case strings.HasPrefix(oid, oidDot1qPvid):
			bp, ok1 := atoi(strings.TrimPrefix(oid, oidDot1qPvid))
			vid, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				bridgePortPvid[bp] = vid
			}
		case strings.HasPrefix(oid, oidDot1qVlanStaticEgressPorts):
			vid, ok := atoi(strings.TrimPrefix(oid, oidDot1qVlanStaticEgressPorts))
			if ok {
				rows.VlanEgressPorts[vid] = []byte(v.Value)
			}
		case strings.HasPrefix(oid, oidDot1qVlanStaticUntaggedPorts):
			vid, ok := atoi(strings.TrimPrefix(oid, oidDot1qVlanStaticUntaggedPorts))
			if ok {
				rows.VlanUntaggedPorts[vid] = []byte(v.Value)
			}
		case strings.HasPrefix(oid, oidIfAdminStatus):
			ifx, ok1 := atoi(strings.TrimPrefix(oid, oidIfAdminStatus))
			s, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				rows.IfAdminStatus[ifx] = s
			}
		case strings.HasPrefix(oid, oidIfType):
			ifx, ok1 := atoi(strings.TrimPrefix(oid, oidIfType))
			n, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				if name, found := ifTypeNumericToString[n]; found {
					rows.IfTypes[ifx] = name
				}
			}
		}
	}
	// Translate bridge-port-keyed PVIDs to ifIndex using the now-complete map.
	for bp, vid := range bridgePortPvid {
		if ifx, ok := rows.BasePortToIfIndex[bp]; ok {
			rows.PortPvid[ifx] = vid
		}
	}
	return rows
}

// buildCiscoRows extracts Cisco overlay rows.
func (m *VlanMapper) buildCiscoRows(all ObjectIDValueMap) qbridge.CiscoRows {
	rows := qbridge.CiscoRows{
		MembershipAccessVlan: map[int]int{},
		VoiceVlanByIfIndex:   map[int]int{},
	}
	for oid, v := range all {
		switch {
		case strings.HasPrefix(oid, oidCiscoVmVlan):
			ifx, ok1 := atoi(strings.TrimPrefix(oid, oidCiscoVmVlan))
			vid, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				rows.MembershipAccessVlan[ifx] = vid
			}
		case strings.HasPrefix(oid, oidCiscoVmVoiceVlanID):
			ifx, ok1 := atoi(strings.TrimPrefix(oid, oidCiscoVmVoiceVlanID))
			vid, ok2 := atoi(v.Value)
			if ok1 && ok2 {
				rows.VoiceVlanByIfIndex[ifx] = vid
			}
		}
	}
	return rows
}

// emitVLANs scans dot1qVlanStaticName / RowStatus and constructs one
// *diode.VLAN per discovered VID. Names default to "VLAN<vid>" when the
// SNMP value is empty (matches device-discovery behavior). Status comes
// from defaults.VLAN.Status; if empty, derived from RowStatus
// (active(1)->active, notInService(2)->reserved, else unset).
//
// When m.options.CreateUnknownVlans is false, VIDs whose
// dot1qVlanStaticName row is absent (or empty) are skipped — only VLANs
// with a real name from the device are emitted. The default v1 behavior
// (CreateUnknownVlans zero-value = false in Go) preserves the original
// emit-all path because the YAML default is true (see config.Options).
// Note: the zero-value in Go for bool is false, but the YAML field uses
// yaml:"create_unknown_vlans" and callers that do not set the option
// explicitly get false — matching the original behavior of always
// emitting. When a policy explicitly sets create_unknown_vlans: false,
// unknown VLANs are suppressed.
func (m *VlanMapper) emitVLANs(all ObjectIDValueMap, defaults *config.Defaults) []diode.Entity {
	type pending struct {
		name      string
		rowStatus int
	}
	byVid := map[int]*pending{}
	for oid, v := range all {
		switch {
		case strings.HasPrefix(oid, oidDot1qVlanStaticName):
			vid, ok := atoi(strings.TrimPrefix(oid, oidDot1qVlanStaticName))
			if !ok {
				continue
			}
			p, exists := byVid[vid]
			if !exists {
				p = &pending{}
				byVid[vid] = p
			}
			p.name = v.Value
		case strings.HasPrefix(oid, oidDot1qVlanStaticRowStatus):
			vid, ok := atoi(strings.TrimPrefix(oid, oidDot1qVlanStaticRowStatus))
			if !ok {
				continue
			}
			st, ok2 := atoi(v.Value)
			if !ok2 {
				continue
			}
			p, exists := byVid[vid]
			if !exists {
				p = &pending{}
				byVid[vid] = p
			}
			p.rowStatus = st
		}
	}
	out := make([]diode.Entity, 0, len(byVid))
	for vid, p := range byVid {
		if vid < 1 || vid > 4094 {
			continue
		}
		// When create_unknown_vlans is false, skip VIDs that have no
		// dot1qVlanStaticName row (name == ""). A status-only row with
		// no name is treated as "unknown" and suppressed.
		if !m.options.CreateUnknownVlans && p.name == "" {
			continue
		}
		name := p.name
		if name == "" {
			name = "VLAN" + strconv.Itoa(vid)
		}
		v := &diode.VLAN{
			Vid:  int64Ptr(int64(vid)),
			Name: StringPtr(name),
		}
		if status := resolveVLANStatus(p.rowStatus, defaults); status != "" {
			v.Status = StringPtr(status)
		}
		applyVLANDefaults(v, defaults)
		out = append(out, v)
	}
	return out
}

func resolveVLANStatus(rowStatus int, defaults *config.Defaults) string {
	if defaults != nil && defaults.VLAN.Status != "" {
		return defaults.VLAN.Status
	}
	switch rowStatus {
	case 1: // active
		return "active"
	case 2: // notInService
		return "reserved"
	}
	return ""
}

// atoi is strconv.Atoi with a single-return ok flag.
func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// int64Ptr is a local helper for *int64 values (diode.VLAN.Vid is *int64).
func int64Ptr(v int64) *int64 {
	return &v
}
