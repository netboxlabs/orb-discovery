package mapping

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBundledBase(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, ok := store.Get("_base")
	require.True(t, ok)
	require.Equal(t, "/system/state/hostname", base.Device.Hostname)
	require.Equal(t, "/interfaces/interface", base.Interfaces.ListPath)
}

func TestOverlayInheritsBase(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "acme.yaml"), []byte(`
extends: _base
match:
  vendor: ACME
components:
  keys:
    serial: state/serial-number
`), 0o644))

	store, err := LoadProfiles(dir)
	require.NoError(t, err)
	p, ok := store.Get("acme")
	require.True(t, ok)
	// inherited from _base:
	require.Equal(t, "/system/state/hostname", p.Device.Hostname)
	require.Equal(t, "/interfaces/interface", p.Interfaces.ListPath)
	// overridden:
	require.Equal(t, "state/serial-number", p.Components.Keys["serial"])
	// untouched base key still present:
	require.Equal(t, "name", p.Components.Keys["name"])
}

func TestMatchFallsBackToBase(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	p := store.Match(MatchInput{Vendor: "TotallyUnknown"})
	require.Equal(t, "_base", p.Name)
}

func TestMatchByVendor(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "acme.yaml"), []byte(`
extends: _base
match:
  vendor: ACME
`), 0o644))
	store, err := LoadProfiles(dir)
	require.NoError(t, err)
	p := store.Match(MatchInput{Vendor: "ACME Networks"})
	require.Equal(t, "acme", p.Name)
}

func TestSubscribePathsAreCuratedLeaves(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	paths := base.SubscribePaths()
	// device leaves + per-interface/-component wildcard leaves; never the bare list root
	require.Contains(t, paths, "/system/state/hostname")
	require.Contains(t, paths, "/interfaces/interface[name=*]/state/admin-status")
	require.NotContains(t, paths, "/interfaces/interface")
	for _, p := range paths {
		require.NotContains(t, p, "oper-status") // §6: volatile telemetry excluded
	}
}

func TestAllowsPath(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	require.True(t, base.AllowsPath("/system/state/hostname"))
	require.True(t, base.AllowsPath("/interfaces/interface[name=Ethernet1]/state/admin-status"))
	require.True(t, base.AllowsPath("/components/component[name=Linecard1]/state/serial-no"))
	require.False(t, base.AllowsPath("/interfaces/interface[name=Ethernet1]/state/oper-status"))
	require.False(t, base.AllowsPath("/interfaces/interface[name=Ethernet1]/state/counters/in-octets"))
}

func TestAllowsDelete(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	require.True(t, base.AllowsDelete("/interfaces/interface[name=Ethernet1]")) // list-entry delete
	require.True(t, base.AllowsDelete("/interfaces/interface"))                 // whole list (ancestor)
	require.True(t, base.AllowsDelete("/components/component[name=Linecard1]"))
	require.False(t, base.AllowsDelete("/network-instances/network-instance[name=default]")) // out of scope
	require.False(t, base.AllowsDelete("/acl/acl-sets"))
	// HIGH: a similarly-named sibling must NOT be treated as within scope
	require.False(t, base.AllowsDelete("/interfaces/interface-state"))
	require.False(t, base.AllowsDelete("/interfaces/interfaces"))
}
