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

	entities := Translate(base, snap, defaults, "")

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
		"/system/state/hostname":                                   "spine1",
		"/interfaces/interface[name=Ethernet1]/state/admin-status": "UP",
	}
	defaults := &config.Defaults{
		Site: "lab", Role: "spine", Location: "rack-1",
		Tags:      []string{"managed"},
		Device:    config.DeviceDefaults{Comments: "auto-discovered", Tags: []string{"gnmi"}},
		Interface: config.InterfaceDefaults{Type: "other", Description: "discovered", Tags: []string{"if-tag"}},
	}
	entities := Translate(base, snap, defaults, "")
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

// TestToInt64PtrUintTypes verifies that uint variants (as produced by the gNMI
// UintVal decoder for OpenConfig mtu) are correctly converted rather than
// silently dropped to nil.
func TestToInt64PtrUintTypes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int64
	}{
		{"uint", uint(9000), 9000},
		{"uint8", uint8(255), 255},
		{"uint16", uint16(1500), 1500},
		{"uint32", uint32(65535), 65535},
		{"uint64", uint64(9000), 9000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toInt64Ptr(tc.in)
			require.NotNil(t, got, "toInt64Ptr returned nil for %T(%v)", tc.in, tc.in)
			require.Equal(t, tc.want, *got)
		})
	}
}

// TestTranslateUint64Mtu verifies that a uint64 mtu value from gNMI (the type
// returned by decodeTypedValue for UintVal) is propagated to the Interface.Mtu
// field instead of being silently dropped.
func TestTranslateUint64Mtu(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")

	snap := map[string]any{
		"/interfaces/interface[name=Ethernet1]/state/mtu": uint64(9000),
	}
	entities := Translate(base, snap, nil, "")
	var eth *diode.Interface
	for _, e := range entities {
		if i, ok := e.(*diode.Interface); ok && *i.Name == "Ethernet1" {
			eth = i
		}
	}
	require.NotNil(t, eth)
	require.NotNil(t, eth.Mtu, "mtu must not be nil for uint64 gNMI value")
	require.Equal(t, int64(9000), *eth.Mtu)
}

func TestTranslateDeviceSerialAndVersion(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")

	// Base snapshot: hostname + software version + a CHASSIS component carrying
	// the device's own serial.
	chassisSnap := func() map[string]any {
		return map[string]any{
			"/system/state/hostname":                               "spine1",
			"/system/state/software-version":                       "4.30.1F",
			"/components/component[name=Chassis1]/state/type":      "CHASSIS",
			"/components/component[name=Chassis1]/state/serial-no": "JPE-CHASSIS-1",
		}
	}

	t.Run("platform default + version", func(t *testing.T) {
		entities := Translate(base, chassisSnap(),
			&config.Defaults{Device: config.DeviceDefaults{Platform: "Arista EOS"}}, "")
		dev := entities[0].(*diode.Device)
		require.NotNil(t, dev.Serial)
		require.Equal(t, "JPE-CHASSIS-1", *dev.Serial)
		require.NotNil(t, dev.Platform)
		require.Equal(t, "Arista EOS 4.30.1F", *dev.Platform.Name)
		// No manufacturer default, no chassis mfg-name, no vendor — the resolved
		// device manufacturer is "Unknown" and Platform carries it.
		require.NotNil(t, dev.Platform.Manufacturer)
		require.Equal(t, "Unknown", *dev.Platform.Manufacturer.Name)

		// The CHASSIS component must NOT surface as a Module/ModuleBay.
		for _, e := range entities {
			switch v := e.(type) {
			case *diode.Module:
				require.NotEqual(t, "Chassis1", *v.ModuleBay.Name)
			case *diode.ModuleBay:
				require.NotEqual(t, "Chassis1", *v.Name)
			}
		}
	})

	t.Run("platform + manufacturer default attached to Platform", func(t *testing.T) {
		entities := Translate(base, chassisSnap(),
			&config.Defaults{Device: config.DeviceDefaults{Platform: "Arista EOS", Manufacturer: "Arista"}}, "")
		dev := entities[0].(*diode.Device)
		require.NotNil(t, dev.Platform)
		require.Equal(t, "Arista EOS 4.30.1F", *dev.Platform.Name)
		require.NotNil(t, dev.Platform.Manufacturer)
		require.Equal(t, "Arista", *dev.Platform.Manufacturer.Name)
		// DeviceType must reference the same manufacturer name.
		require.NotNil(t, dev.DeviceType)
		require.Equal(t, "Arista", *dev.DeviceType.Manufacturer.Name)
	})

	t.Run("version only (no platform default)", func(t *testing.T) {
		entities := Translate(base, chassisSnap(), &config.Defaults{}, "")
		dev := entities[0].(*diode.Device)
		require.NotNil(t, dev.Platform)
		require.Equal(t, "4.30.1F", *dev.Platform.Name)
		require.NotNil(t, dev.Serial)
		require.Equal(t, "JPE-CHASSIS-1", *dev.Serial)
	})

	t.Run("platform default only (no version leaf) preserves prior behavior", func(t *testing.T) {
		snap := map[string]any{"/system/state/hostname": "spine1"}
		entities := Translate(base, snap,
			&config.Defaults{Device: config.DeviceDefaults{Platform: "Arista EOS"}}, "")
		dev := entities[0].(*diode.Device)
		require.NotNil(t, dev.Platform)
		require.Equal(t, "Arista EOS", *dev.Platform.Name)
		require.Nil(t, dev.Serial)
	})

	t.Run("neither platform default nor version", func(t *testing.T) {
		snap := map[string]any{"/system/state/hostname": "spine1"}
		entities := Translate(base, snap, &config.Defaults{}, "")
		dev := entities[0].(*diode.Device)
		require.Nil(t, dev.Platform)
		require.Nil(t, dev.Serial)
	})
}

func TestTranslateComponents(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")
	snap := map[string]any{
		"/system/state/hostname":                                "spine1",
		"/components/component[name=Linecard1]/state/type":      "LINECARD",
		"/components/component[name=Linecard1]/state/serial-no": "JPE123",
		"/components/component[name=Linecard1]/state/part-no":   "DCS-7500",
		"/components/component[name=PowerSupply1]/state/type":   "POWER_SUPPLY",
	}
	entities := Translate(base, snap, &config.Defaults{Device: config.DeviceDefaults{Manufacturer: "Arista"}}, "")
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

// TestComponentTypeIdentityref verifies that the JSON_IETF-serialized identityref
// form of /components/component/state/type (module-prefixed, e.g.
// "openconfig-platform-types:CHASSIS") is normalized identically to the bare form
// ("CHASSIS"). Without prefix-stripping, exact upper-case equality fails and the
// chassis serial/manufacturer/model are silently lost AND every Module/ModuleBay
// is silently dropped. This test FAILS against the pre-fix exact-equality code.
func TestComponentTypeIdentityref(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")

	// Same topology as the bare-form tests, but with prefixed identityref types.
	snap := map[string]any{
		"/system/state/hostname":                                "spine1",
		"/components/component[name=Chassis1]/state/type":       "openconfig-platform-types:CHASSIS",
		"/components/component[name=Chassis1]/state/serial-no":  "JPE-CHASSIS-1",
		"/components/component[name=Chassis1]/state/part-no":    "DCS-7050",
		"/components/component[name=Chassis1]/state/mfg-name":   "Arista",
		"/components/component[name=Linecard1]/state/type":      "openconfig-platform-types:LINECARD",
		"/components/component[name=Linecard1]/state/serial-no": "JPE123",
		"/components/component[name=Linecard1]/state/part-no":   "DCS-LC",
		"/components/component[name=Xcvr1]/state/type":          "openconfig-platform-types:TRANSCEIVER",
		"/components/component[name=Xcvr1]/state/mfg-name":      "Finisar",
		"/components/component[name=Xcvr1]/state/part-no":       "FTLX",
	}
	entities := Translate(base, snap, &config.Defaults{}, "")

	dev := entities[0].(*diode.Device)
	require.NotNil(t, dev.Serial)
	require.Equal(t, "JPE-CHASSIS-1", *dev.Serial, "chassis serial must resolve from prefixed type")
	require.NotNil(t, dev.DeviceType)
	require.Equal(t, "Arista", *dev.DeviceType.Manufacturer.Name, "chassis mfg-name must resolve from prefixed type")
	require.Equal(t, "DCS-7050", *dev.DeviceType.Model, "chassis part-no must resolve from prefixed type")

	// Modules/ModuleBays must still be emitted for the prefixed LINECARD/TRANSCEIVER,
	// and the CHASSIS must NOT surface as a Module/ModuleBay.
	mods := map[string]*diode.Module{}
	bays := map[string]bool{}
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.Module:
			mods[*v.ModuleBay.Name] = v
		case *diode.ModuleBay:
			bays[*v.Name] = true
		}
	}
	require.Contains(t, mods, "Linecard1", "LINECARD must emit a Module from prefixed type")
	require.Contains(t, mods, "Xcvr1", "TRANSCEIVER must emit a Module from prefixed type")
	require.True(t, bays["Linecard1"] && bays["Xcvr1"])
	require.NotContains(t, mods, "Chassis1", "CHASSIS must not surface as a Module")
	require.False(t, bays["Chassis1"], "CHASSIS must not surface as a ModuleBay")
	// The transceiver carries its own mfg-name; the chassis manufacturer is Arista.
	require.Equal(t, "Finisar", *mods["Xcvr1"].ModuleType.Manufacturer.Name)
	require.Equal(t, "Arista", *mods["Linecard1"].ModuleType.Manufacturer.Name)
}

// TestTranslateDiscoversManufacturer exercises the manufacturer/model discovery
// precedence: policy default > chassis mfg-name (or part-no) > Capabilities
// vendor > "Unknown"; and per-component module manufacturer.
func TestTranslateDiscoversManufacturer(t *testing.T) {
	store, err := LoadProfiles("")
	require.NoError(t, err)
	base, _ := store.Get("_base")

	devOf := func(entities []diode.Entity) *diode.Device { return entities[0].(*diode.Device) }

	t.Run("chassis mfg-name discovered (no default)", func(t *testing.T) {
		snap := map[string]any{
			"/system/state/hostname":                              "spine1",
			"/system/state/software-version":                      "4.30.1F",
			"/components/component[name=Chassis1]/state/type":     "CHASSIS",
			"/components/component[name=Chassis1]/state/mfg-name": "Arista",
		}
		dev := devOf(Translate(base, snap,
			&config.Defaults{Device: config.DeviceDefaults{Platform: "Arista EOS"}}, ""))
		require.NotNil(t, dev.DeviceType)
		require.Equal(t, "Arista", *dev.DeviceType.Manufacturer.Name)
		require.NotNil(t, dev.Platform)
		require.NotNil(t, dev.Platform.Manufacturer)
		require.Equal(t, "Arista", *dev.Platform.Manufacturer.Name)
	})

	t.Run("capabilities vendor fallback (no mfg-name, no default)", func(t *testing.T) {
		snap := map[string]any{"/system/state/hostname": "r1"}
		dev := devOf(Translate(base, snap, &config.Defaults{}, "Nokia"))
		require.Equal(t, "Nokia", *dev.DeviceType.Manufacturer.Name)
	})

	t.Run("policy default overrides discovered", func(t *testing.T) {
		snap := map[string]any{
			"/system/state/hostname":                              "spine1",
			"/components/component[name=Chassis1]/state/type":     "CHASSIS",
			"/components/component[name=Chassis1]/state/mfg-name": "Arista",
		}
		dev := devOf(Translate(base, snap,
			&config.Defaults{Device: config.DeviceDefaults{Manufacturer: "OverrideCorp"}}, "Nokia"))
		require.Equal(t, "OverrideCorp", *dev.DeviceType.Manufacturer.Name)
	})

	t.Run("nothing discovered -> Unknown", func(t *testing.T) {
		snap := map[string]any{"/system/state/hostname": "r1"}
		dev := devOf(Translate(base, snap, &config.Defaults{}, ""))
		require.Equal(t, "Unknown", *dev.DeviceType.Manufacturer.Name)
		require.Equal(t, "Unknown", *dev.DeviceType.Model)
	})

	t.Run("chassis part-no -> DeviceType.Model (no model default)", func(t *testing.T) {
		snap := map[string]any{
			"/system/state/hostname":                             "spine1",
			"/components/component[name=Chassis1]/state/type":    "CHASSIS",
			"/components/component[name=Chassis1]/state/part-no": "DCS-7050",
		}
		dev := devOf(Translate(base, snap, &config.Defaults{}, ""))
		require.Equal(t, "DCS-7050", *dev.DeviceType.Model)
	})

	t.Run("model default overrides chassis part-no", func(t *testing.T) {
		snap := map[string]any{
			"/system/state/hostname":                             "spine1",
			"/components/component[name=Chassis1]/state/type":    "CHASSIS",
			"/components/component[name=Chassis1]/state/part-no": "DCS-7050",
		}
		dev := devOf(Translate(base, snap,
			&config.Defaults{Device: config.DeviceDefaults{Model: "ModelDefault"}}, ""))
		require.Equal(t, "ModelDefault", *dev.DeviceType.Model)
	})

	t.Run("per-component module manufacturer from its own mfg-name", func(t *testing.T) {
		// Chassis is Arista; a transceiver reports its own mfg-name "Finisar".
		snap := map[string]any{
			"/system/state/hostname":                              "spine1",
			"/components/component[name=Chassis1]/state/type":     "CHASSIS",
			"/components/component[name=Chassis1]/state/mfg-name": "Arista",
			"/components/component[name=Xcvr1]/state/type":        "TRANSCEIVER",
			"/components/component[name=Xcvr1]/state/mfg-name":    "Finisar",
			"/components/component[name=Xcvr1]/state/part-no":     "FTLX",
			"/components/component[name=Linecard1]/state/type":    "LINECARD",
		}
		entities := Translate(base, snap, &config.Defaults{}, "")
		mods := map[string]*diode.Module{}
		for _, e := range entities {
			if m, ok := e.(*diode.Module); ok {
				mods[*m.ModuleBay.Name] = m
			}
		}
		require.Contains(t, mods, "Xcvr1")
		require.Equal(t, "Finisar", *mods["Xcvr1"].ModuleType.Manufacturer.Name)
		// The linecard has no own mfg-name -> falls back to the device manufacturer.
		require.Contains(t, mods, "Linecard1")
		require.Equal(t, "Arista", *mods["Linecard1"].ModuleType.Manufacturer.Name)
	})
}
