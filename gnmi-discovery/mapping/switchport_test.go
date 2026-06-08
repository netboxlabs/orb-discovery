package mapping

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSwitchedVlanPath(t *testing.T) {
	lp := "/interfaces/interface"
	for _, c := range []struct{ path, iface, leaf string }{
		{"/interfaces/interface[name=Ethernet1]/ethernet/switched-vlan/state/interface-mode", "Ethernet1", "interface-mode"},
		{"/interfaces/interface[name=Ethernet1]/ethernet/switched-vlan/state/access-vlan", "Ethernet1", "access-vlan"},
		{"/interfaces/interface[name=Po1]/aggregation/switched-vlan/state/trunk-vlans", "Po1", "trunk-vlans"},
		{"/interfaces/interface[name=Eth1/1]/ethernet/switched-vlan/state/native-vlan", "Eth1/1", "native-vlan"},
	} {
		iface, leaf, ok := parseSwitchedVlanPath(c.path, lp)
		require.True(t, ok, c.path)
		require.Equal(t, c.iface, iface)
		require.Equal(t, c.leaf, leaf)
	}
	for _, p := range []string{
		"/interfaces/interface[name=Ethernet1]/state/mtu",
		"/interfaces/interface[name=Ethernet1]/ethernet/switched-vlan/state/counters/in",
		"/interfaces/interface[name=Ethernet1]/ethernet/state/port-speed",
	} {
		_, _, ok := parseSwitchedVlanPath(p, lp)
		require.False(t, ok, p)
	}
}

func TestSafeVid(t *testing.T) {
	for _, in := range []any{1, 4094, "10", int64(100), float64(200), uint64(300), uint16(40)} {
		_, ok := safeVid(in)
		require.True(t, ok)
	}
	for _, in := range []any{0, 4095, -1, true, false, "x", "", nil} {
		_, ok := safeVid(in)
		require.False(t, ok)
	}
	v, _ := safeVid("4094")
	require.Equal(t, int64(4094), v)
}

func TestExpandTrunkVlans(t *testing.T) {
	require.Equal(t, []int64{20, 30, 31, 32}, expandTrunkVlans([]any{float64(20), "30..32"}))
	require.Equal(t, []int64{100}, expandTrunkVlans("100"))                         // lone scalar
	require.Equal(t, []int64{1, 2, 3}, expandTrunkVlans([]any{"3", "1", "2", "2"})) // sort + dedup
	// clamp a malformed huge range to <=4094 entries (bound, no OOM)
	require.Len(t, expandTrunkVlans([]any{"1..999999"}), 4094)
	// out-of-range / reversed skipped
	require.Empty(t, expandTrunkVlans([]any{"5000..6000", "10..5", "x"}))
}

func TestOCVlanMode(t *testing.T) {
	require.Equal(t, "access", ocVlanMode["ACCESS"])
	require.Equal(t, "tagged", ocVlanMode["TRUNK"])
	_, ok := ocVlanMode["BOGUS"]
	require.False(t, ok)
}
