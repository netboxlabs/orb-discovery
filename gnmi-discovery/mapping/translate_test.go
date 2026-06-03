package mapping

import (
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
	"github.com/stretchr/testify/require"
)

func TestListKeyAndLeaf(t *testing.T) {
	key, leaf, ok := listKeyAndLeaf(
		"/interfaces/interface[name=Ethernet1]/state/mtu",
		"/interfaces/interface")
	require.True(t, ok)
	require.Equal(t, "Ethernet1", key)
	require.Equal(t, "state/mtu", leaf)
}

func TestListKeyAndLeafNonMatch(t *testing.T) {
	_, _, ok := listKeyAndLeaf("/system/state/hostname", "/interfaces/interface")
	require.False(t, ok)
}

func TestListKeyAndLeafSlashInName(t *testing.T) {
	// Key value contains a slash (e.g. "Ethernet1/1"); split must happen on ']'
	// not on '/', so the full key and the correct leaf are recovered.
	key, leaf, ok := listKeyAndLeaf(
		"/interfaces/interface[name=Ethernet1/1]/state/mtu",
		"/interfaces/interface")
	require.True(t, ok)
	require.Equal(t, "Ethernet1/1", key)
	require.Equal(t, "state/mtu", leaf)
}

func TestTranslateDeviceAndInterfaces(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")

	snap := map[string]any{
		"/system/state/hostname":                                     "spine1",
		"/system/state/software-version":                             "4.30.1F",
		"/interfaces/interface[name=Ethernet1]/config/description":   "uplink",
		"/interfaces/interface[name=Ethernet1]/state/admin-status":   "UP",
		"/interfaces/interface[name=Ethernet1]/state/mtu":            9214,
		"/interfaces/interface[name=Management1]/state/admin-status": "UP",
	}
	defaults := &config.Defaults{Site: "lab", Role: "spine", Interface: config.InterfaceDefaults{Type: "other"}}

	entities := Translate(base, snap, defaults)

	var dev *diode.Device
	ifaceNames := map[string]bool{}
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Device:
			dev = v
		case *diode.Interface:
			ifaceNames[*v.Name] = true
		}
	}
	require.NotNil(t, dev)
	require.Equal(t, "spine1", *dev.Name)
	require.True(t, ifaceNames["Ethernet1"])
	require.True(t, ifaceNames["Management1"])
}

func TestTranslateAppliesRichDefaults(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	snap := map[string]any{
		"/system/state/hostname":                                     "spine1",
		"/interfaces/interface[name=Ethernet1]/state/admin-status": "UP",
	}
	defaults := &config.Defaults{
		Site: "lab", Role: "spine", Location: "rack-1",
		Tags:      []string{"managed"},
		Device:    config.DeviceDefaults{Comments: "auto-discovered", Tags: []string{"gnmi"}},
		Interface: config.InterfaceDefaults{Type: "other", Description: "discovered", Tags: []string{"if-tag"}},
	}
	entities := Translate(base, snap, defaults)
	dev := entities[0].(*diode.Device)
	require.Equal(t, "auto-discovered", *dev.Comments)
	require.NotNil(t, dev.Location)
	require.Equal(t, "rack-1", *dev.Location.Name)
	require.Same(t, dev.Site, dev.Location.Site) // location scoped to the device's site
	devTags := map[string]bool{}
	for _, tg := range dev.Tags {
		devTags[*tg.Name] = true
	}
	require.True(t, devTags["managed"] && devTags["gnmi"])

	var eth *diode.Interface
	for _, e := range entities {
		if i, ok := e.(*diode.Interface); ok && *i.Name == "Ethernet1" {
			eth = i
		}
	}
	require.NotNil(t, eth)
	require.Equal(t, "discovered", *eth.Description) // default applied (no leaf description present)
	require.Len(t, eth.Tags, 1)
	require.Equal(t, "if-tag", *eth.Tags[0].Name)
}

func TestTranslateComponents(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	snap := map[string]any{
		"/system/state/hostname":                               "spine1",
		"/components/component[name=Linecard1]/state/type":    "LINECARD",
		"/components/component[name=Linecard1]/state/serial-no": "JPE123",
		"/components/component[name=Linecard1]/state/part-no": "DCS-7500",
		"/components/component[name=PowerSupply1]/state/type": "POWER_SUPPLY",
	}
	entities := Translate(base, snap, &config.Defaults{Device: config.DeviceDefaults{Manufacturer: "Arista"}})
	var modules []*diode.Module
	var bays []*diode.ModuleBay
	bayIdx, modIdx := -1, -1
	for i, e := range entities {
		switch v := e.(type) {
		case *diode.Module:
			modules = append(modules, v)
			modIdx = i
		case *diode.ModuleBay:
			bays = append(bays, v)
			if bayIdx == -1 {
				bayIdx = i
			}
		}
	}
	// Only inventory-bearing components (LINECARD) are emitted; PSU is classified
	// but dropped in the MVP. Each emits a standalone ModuleBay AND a Module.
	require.Len(t, modules, 1)
	require.Len(t, bays, 1)
	require.Less(t, bayIdx, modIdx, "ModuleBay must be emitted before its Module")
	require.Equal(t, "JPE123", *modules[0].Serial)
	require.Equal(t, "DCS-7500", *modules[0].ModuleType.Model)
	require.Equal(t, "Arista", *modules[0].ModuleType.Manufacturer.Name)
	require.Equal(t, "Linecard1", *bays[0].Name)
}
