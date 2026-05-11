package mapping

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRouteIfIndex_AliasTable_DirectChassis(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	// Port directly on chassis row 1 (entPhysicalIndex=1).
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.10101"}
	// Port on a card under chassis row 1000 (entPhysicalIndex=1050 -> contained=1000).
	oids[".1.3.6.1.2.1.47.1.1.1.1.4.1050"] = Value{Value: "1000"}
	oids[".1.3.6.1.2.1.47.1.1.1.1.5.1050"] = Value{Value: "9"} // module
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1050.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.10201"}

	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)

	id, ok := r.routeIfIndex(10101)
	assert.True(t, ok)
	assert.Equal(t, 1, id)

	id, ok = r.routeIfIndex(10201)
	assert.True(t, ok)
	assert.Equal(t, 2, id)
}

func TestRouteIfIndex_AliasMissing_ReturnsFalse(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack() // no alias entries
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(10101)
	assert.False(t, ok)
}

func TestRouteIfIndex_AliasValueWithoutLeadingDot(t *testing.T) {
	// Some gosnmp ObjectIdentifier renderings omit the leading dot.
	// Routing must still succeed.
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: "1.3.6.1.2.1.2.2.1.1.10101"} // no leading dot
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	id, ok := r.routeIfIndex(10101)
	assert.True(t, ok)
	assert.Equal(t, 1, id)
}

func TestRouteIfIndex_NonIfIndexAliasValueIgnored(t *testing.T) {
	// Non-ifIndex VariablePointer values (e.g. pointing at ifAlias)
	// must be skipped without crashing.
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.1.0"] = Value{Value: ".1.3.6.1.2.1.31.1.1.1.18.10101"} // ifAlias
	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(10101)
	assert.False(t, ok, "non-ifIndex alias values are not usable for routing")
}

func TestRouteIfIndex_ChainTerminatesAtRoot_ReturnsFalse(t *testing.T) {
	logger := slog.Default()
	oids := fixtureCisco3850TwoMemberStack()
	// entPhysicalIndex 999 contained directly in root (0) — not a chassis.
	oids[".1.3.6.1.2.1.47.1.1.1.1.4.999"] = Value{Value: "0"}
	oids[".1.3.6.1.2.1.47.1.3.2.1.2.999.0"] = Value{Value: ".1.3.6.1.2.1.2.2.1.1.42"}

	inv := extractInventory(oids, logger)
	r := newChassisRouter(inv, oids, logger)
	_, ok := r.routeIfIndex(42)
	assert.False(t, ok)
}

func TestParseMemberID(t *testing.T) {
	cases := []struct {
		ifName string
		wantID int
		wantOK bool
	}{
		// Cisco IOS/IOS-XE stack 3-tuple.
		{"GigabitEthernet1/0/1", 1, true},
		{"Gi1/0/1", 1, true},
		{"Gi2/0/24", 2, true},
		{"TenGigE2/0/24", 2, true},
		{"TenGigabitEthernet3/0/1", 3, true},
		{"FortyGigabitEthernet1/1/1", 1, true},
		{"HundredGigE1/0/1", 1, true},
		{"mGig3/0/1", 3, true}, // multi-gig prefix
		{"TwoGigabitEthernet1/0/2", 1, true},
		{"FiveGigabitEthernet1/0/3", 1, true},

		// Junos FPC.
		{"xe-0/0/0", 0, true},
		{"ge-2/0/1", 2, true},
		{"et-3/0/0", 3, true},

		// Aruba CX 1/1/1.
		{"1/1/1", 1, true},
		{"2/1/24", 2, true},

		// HP/H3C Comware.
		{"GigabitEthernet1/0/1", 1, true},
		{"Ten-GigabitEthernet2/0/1", 2, true},

		// No member id (route to master).
		{"Vlan10", 0, false},
		{"Loopback0", 0, false},
		{"Port-channel1", 0, false},
		{"Po1", 0, false},
		{"mgmt0", 0, false},
		{"Tunnel1", 0, false},
		{"BVI100", 0, false},
		{"Bundle-Ether1", 0, false},
		{"Null0", 0, false},
		{"", 0, false},

		// Negative cases for non-stack 2-tuple naming conventions —
		// must NOT false-positive into a stack member id.
		{"Gi1/1", 0, false},              // Cisco 2-tuple (non-stack chassis)
		{"GigabitEthernet1/1", 0, false}, // Cisco 2-tuple long form
		{"Ethernet1/1", 0, false},        // NX-OS 2-tuple (VPC is not VC)
		{"ether1", 0, false},             // MikroTik (no stack convention)
		{"ether10", 0, false},
		{"1:1", 0, false},          // Extreme EXOS (unsupported in batch 1)
		{"sfp-sfpplus1", 0, false}, // MikroTik SFP+ port

		// Subinterfaces — must parse the parent-port member id, not fall through.
		{"GigabitEthernet2/0/1.100", 2, true},
		{"Gi2/0/1.100", 2, true},
		{"xe-2/0/0.0", 2, true},
		{"ge-3/1/0.500", 3, true},
		{"2/1/24.100", 2, true},
		// Non-digit suffix → leave name alone, then no match (Vlan10.foo, etc.).
		{"Vlan10.foo", 0, false},
		// Edge: trailing dot with no digits, should not strip.
		{"foo.", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.ifName, func(t *testing.T) {
			id, ok := ParseMemberID(tc.ifName)
			assert.Equal(t, tc.wantOK, ok, "ok mismatch")
			if tc.wantOK {
				assert.Equal(t, tc.wantID, id, "id mismatch")
			}
		})
	}
}
