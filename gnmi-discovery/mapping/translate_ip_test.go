package mapping

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseIPAddressPath(t *testing.T) {
	lp := "/interfaces/interface"
	iface, idx, fam, ip, leaf, ok := parseIPAddressPath(
		"/interfaces/interface[name=Ethernet1]/subinterfaces/subinterface[index=0]/ipv4/addresses/address[ip=10.0.0.1]/state/prefix-length", lp)
	require.True(t, ok)
	require.Equal(t, "Ethernet1", iface)
	require.Equal(t, "0", idx)
	require.Equal(t, "ipv4", fam)
	require.Equal(t, "10.0.0.1", ip)
	require.Equal(t, "state/prefix-length", leaf)
}

func TestParseIPAddressPathV6(t *testing.T) {
	lp := "/interfaces/interface"
	iface, idx, fam, ip, leaf, ok := parseIPAddressPath(
		"/interfaces/interface[name=Eth1/1]/subinterfaces/subinterface[index=100]/ipv6/addresses/address[ip=2001:db8::1]/state/prefix-length", lp)
	require.True(t, ok)
	require.Equal(t, "Eth1/1", iface)   // slash in the interface key survives (split on ']')
	require.Equal(t, "100", idx)
	require.Equal(t, "ipv6", fam)
	require.Equal(t, "2001:db8::1", ip) // colons in the v6 key survive
	require.Equal(t, "state/prefix-length", leaf)
}

func TestParseIPAddressPathNonMatch(t *testing.T) {
	lp := "/interfaces/interface"
	for _, p := range []string{
		"/interfaces/interface[name=Ethernet1]/state/mtu",                 // not an IP path
		"/system/state/hostname",                                          // unrelated
		"/components/component[name=Chassis1]/state/serial-no",            // unrelated
	} {
		_, _, _, _, _, ok := parseIPAddressPath(p, lp)
		require.False(t, ok, p)
	}
}
