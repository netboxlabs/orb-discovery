package mapping

import (
	"log/slog"
	"os"
	"strconv"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

func TestVlanMapper_MapIsNoop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm := NewVlanMapper(logger)
	got := vm.Map(nil, nil, nil, nil)
	if got != nil {
		t.Errorf("Map returned non-nil entity: %v", got)
	}
}

func TestVlanMapper_PostMap_NoVLANRows(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm := NewVlanMapper(logger)
	registry := NewEntityRegistry(logger)
	defaults := &config.Defaults{}
	got := vm.PostMap(ObjectIDValueMap{}, registry, defaults)
	if len(got) != 0 {
		t.Errorf("PostMap with empty input: got %d entities, want 0", len(got))
	}
}

func TestVlanMapper_PostMap_AccessPort_MutatesInterface(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	registry := NewEntityRegistry(logger)
	// Pre-populate an Interface as if InterfaceMapper had run.
	iface := &diode.Interface{Name: StringPtr("Ethernet1")}
	if registry.entities[InterfaceEntityType] == nil {
		registry.entities[InterfaceEntityType] = map[ObjectIDIndex]diode.Entity{}
	}
	registry.entities[InterfaceEntityType]["101"] = iface
	registry.MarkInterfaceVerified(iface)

	rows := buildAccessPortFixture(101, 10)

	vm := NewVlanMapper(logger)
	defaults := &config.Defaults{VLAN: config.VLANDefaults{Status: "active"}}
	emitted := vm.PostMap(rows, registry, defaults)

	if iface.Mode == nil || *iface.Mode != "access" {
		t.Errorf("Interface.Mode: got %v, want access", iface.Mode)
	}
	if iface.UntaggedVlan == nil || iface.UntaggedVlan.Vid == nil || *iface.UntaggedVlan.Vid != 10 {
		t.Errorf("Interface.UntaggedVlan: got %+v, want VID 10 (int64)", iface.UntaggedVlan)
	}

	vlanCount := 0
	for _, e := range emitted {
		if _, ok := e.(*diode.VLAN); ok {
			vlanCount++
		}
	}
	if vlanCount == 0 {
		t.Error("expected at least one diode.VLAN entity emitted")
	}
}

// buildAccessPortFixture constructs a minimal ObjectIDValueMap covering an
// access port with one VLAN. Mirrors the OID layout the runtime walker
// produces.
func buildAccessPortFixture(ifIndex, vid int) ObjectIDValueMap {
	out := ObjectIDValueMap{}
	put := func(oid string, val string, t Asn1BER) {
		out[oid] = Value{Value: val, Type: t}
	}
	// dot1dBasePortIfIndex.1 = ifIndex
	put(".1.3.6.1.2.1.17.1.4.1.2.1", strconv.Itoa(ifIndex), Integer)
	// dot1qPvid.<ifIndex>
	put(".1.3.6.1.2.1.17.7.1.4.5.1.1."+strconv.Itoa(ifIndex), strconv.Itoa(vid), Integer)
	// dot1qVlanStaticName.<vid>
	put(".1.3.6.1.2.1.17.7.1.4.3.1.1."+strconv.Itoa(vid), "Eng", OctetString)
	// dot1qVlanStaticEgressPorts.<vid> = 0x80 (port 1)
	put(".1.3.6.1.2.1.17.7.1.4.3.1.2."+strconv.Itoa(vid), "\x80", OctetString)
	// dot1qVlanStaticUntaggedPorts.<vid> = 0x80
	put(".1.3.6.1.2.1.17.7.1.4.3.1.4."+strconv.Itoa(vid), "\x80", OctetString)
	// dot1qVlanStaticRowStatus.<vid> = active(1)
	put(".1.3.6.1.2.1.17.7.1.4.3.1.5."+strconv.Itoa(vid), "1", Integer)
	// ifAdminStatus.<ifIndex> = 1 (up)
	put(".1.3.6.1.2.1.2.2.1.7."+strconv.Itoa(ifIndex), "1", Integer)
	// ifType.<ifIndex> = 6 (ethernetCsmacd)
	put(".1.3.6.1.2.1.2.2.1.3."+strconv.Itoa(ifIndex), "6", Integer)
	return out
}
