package qbridge

import (
	"testing"
)

func TestExtractGeneric_AccessPort(t *testing.T) {
	rows := GenericRows{
		BasePortToIfIndex: map[int]int{1: 101, 2: 102},
		PortPvid:          map[int]int{101: 10, 102: 20},
		VlanEgressPorts: map[int][]byte{
			10: {0x80}, // port 1
			20: {0x40}, // port 2
		},
		VlanUntaggedPorts: map[int][]byte{
			10: {0x80},
			20: {0x40},
		},
		IfTypes: map[int]string{
			101: "ethernetCsmacd",
			102: "ethernetCsmacd",
		},
		IfAdminStatus: map[int]int{101: 1, 102: 1},
	}
	got, err := ExtractGeneric(rows)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	for _, ifIndex := range []int{101, 102} {
		info, ok := got[ifIndex]
		if !ok {
			t.Fatalf("ifIndex %d missing", ifIndex)
		}
		if !info.Enabled || !info.BridgePortPresent {
			t.Errorf("ifIndex %d: Enabled=%v BridgePortPresent=%v",
				ifIndex, info.Enabled, info.BridgePortPresent)
		}
	}
}

func TestExtractGeneric_TrunkAllWildcard(t *testing.T) {
	bp := map[int]int{1: 1001}
	rows := GenericRows{
		BasePortToIfIndex: bp,
		PortPvid:          map[int]int{1001: 1},
		VlanEgressPorts:   map[int][]byte{},
		VlanUntaggedPorts: map[int][]byte{},
		IfAdminStatus:     map[int]int{1001: 1},
		IfTypes:           map[int]string{1001: "ethernetCsmacd"},
	}
	for vid := 1; vid <= 4094; vid++ {
		// Each VID has port 1 in its egress set (so port 1 is in all VLANs).
		rows.VlanEgressPorts[vid] = []byte{0x80}
		if vid != 1 {
			// All VLANs except native have it tagged on the port.
			rows.VlanUntaggedPorts[vid] = []byte{0x00}
		} else {
			rows.VlanUntaggedPorts[vid] = []byte{0x80}
		}
	}
	got, err := ExtractGeneric(rows)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	info := got[1001]
	if !info.AllowedVlans.IsWildcard {
		t.Errorf("expected wildcard, got %+v", info.AllowedVlans)
	}
}

func TestExtractGeneric_RoutedPort(t *testing.T) {
	rows := GenericRows{
		BasePortToIfIndex: map[int]int{1: 201}, // 201 IS in bridge table
		PortPvid:          map[int]int{},        // but has no PVID -> not bridged
		VlanEgressPorts:   map[int][]byte{},
		VlanUntaggedPorts: map[int][]byte{},
		IfAdminStatus:     map[int]int{201: 1},
		IfTypes:           map[int]string{201: "ethernetCsmacd"},
	}
	got, err := ExtractGeneric(rows)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	info := got[201]
	if info.OperMode != OperRouted {
		t.Errorf("expected OperRouted, got %v", info.OperMode)
	}
}

func TestExtractGeneric_MissingTranslationTable(t *testing.T) {
	rows := GenericRows{
		BasePortToIfIndex: map[int]int{}, // empty
		VlanEgressPorts:   map[int][]byte{10: {0xFF}},
	}
	if _, err := ExtractGeneric(rows); err == nil {
		t.Fatal("want error, got nil")
	}
}
