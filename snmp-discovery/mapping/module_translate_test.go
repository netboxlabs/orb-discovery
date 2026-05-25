// Copyright 2026 NetBox Labs, Inc.

package mapping

import (
	"log/slog"
	"os"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// modeOff / modeLinecards / modeFull return *config.Options with the
// matching discover_modules string set. Tiny helpers so each test reads
// like prose.
func modeOff() *config.Options {
	v := "off"
	return &config.Options{DiscoverModules: &v}
}

func modeLinecards() *config.Options {
	v := "linecards"
	return &config.Options{DiscoverModules: &v}
}

func modeFull() *config.Options {
	v := "full"
	return &config.Options{DiscoverModules: &v}
}

// TestTranslateModules_OffMode_ReturnsNil — default (and explicit "off")
// must short-circuit before any extraction or emission so existing
// pipelines see zero behaviour change.
func TestTranslateModules_OffMode_ReturnsNil(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	oids := buildOIDs(chassis9404RWithTransceiversFixture())
	dev := &diode.Device{Name: strPtr("test")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, ifaceMap := TranslateModules(oids, nil, memberDevices, modeOff(), nil, logger)

	assert.Nil(t, entities, "off mode emits no entities")
	assert.Nil(t, ifaceMap, "off mode produces no iface attachment map")
}

// TestTranslateModules_LinecardsMode_StandaloneEmitsLinecardsAndSupervisorsNoTransceivers
// — linecards mode emits the chassis-slot modules (supervisor + linecard
// in the 9404R fixture) and their bays, but NOT the transceiver. The
// iface attachment map is nil/empty in linecards mode.
func TestTranslateModules_LinecardsMode_StandaloneEmitsLinecardsAndSupervisorsNoTransceivers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	oids := buildOIDs(chassis9404RWithTransceiversFixture())
	dev := &diode.Device{Name: strPtr("test-router")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, ifaceMap := TranslateModules(oids, nil, memberDevices, modeLinecards(), nil, logger)

	var bays []*diode.ModuleBay
	var modules []*diode.Module
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.ModuleBay:
			bays = append(bays, v)
		case *diode.Module:
			modules = append(modules, v)
		}
	}
	require.Len(t, bays, 2, "supervisor bay + linecard bay")
	require.Len(t, modules, 2, "supervisor + linecard; no transceiver in linecards mode")

	// iface map stays empty — transceiver routing is full-mode only.
	assert.Empty(t, ifaceMap)

	// No transceiver model leaks into the module list.
	for _, m := range modules {
		require.NotNil(t, m.ModuleType, "every emitted Module needs a ModuleType")
		require.NotNil(t, m.ModuleType.Model)
		assert.NotEqual(t, "SFP-10G-LR", *m.ModuleType.Model,
			"transceivers must not be emitted as modules in linecards mode")
	}

	// Every ModuleBay carries Device; every Module carries Device + ModuleBay.
	for _, b := range bays {
		require.NotNil(t, b.Device, "ModuleBay must have Device set")
	}
	for _, m := range modules {
		require.NotNil(t, m.Device, "Module must have Device set")
		require.NotNil(t, m.ModuleBay, "Module must have ModuleBay set")
	}
}

// TestTranslateModules_PSUAndFan_NotEmitted — PSUs and fans are
// classified for label-only purposes; they must NOT surface as Module
// entities even in full mode.
func TestTranslateModules_PSUAndFan_NotEmitted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rows := []fixtureRow{
		{"1", "0", "3", "1", "Chassis", "FOO", "C9404R", "Chassis", ""},
		{"100", "1", "5", "1", "PowerSupply 1", "", "", "", ""},
		{"101", "100", "9", "1", "PSU 1", "PSUSER01", "PWR-C5-715WAC", "", ""},
		{"200", "1", "5", "1", "Fan Tray", "", "", "", ""},
		// Fan PID — uses "FAN-" prefix so the current classifier (which
		// matches HasPrefix(upper, "FAN")) recognises it. PR #419's
		// classifier additionally treats "C9400-FAN" via the -FAN suffix,
		// but that branch is unrelated to this filter test.
		{"201", "200", "9", "1", "Fan 1", "FANSER01", "FAN-T1-R", "", ""},
		{"300", "1", "5", "1", "Slot 3", "", "", "", ""},
		{"301", "300", "9", "1", "Linecard 3", "LCSER03", "C9400-LC-48U", "", ""},
	}
	dev := &diode.Device{Name: strPtr("test")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, _ := TranslateModules(buildOIDs(rows), nil, memberDevices, modeFull(), nil, logger)

	var modules []*diode.Module
	for _, e := range entities {
		if m, ok := e.(*diode.Module); ok {
			modules = append(modules, m)
		}
	}
	require.Len(t, modules, 1, "only the linecard — PSU + fan filtered out")
	require.NotNil(t, modules[0].ModuleType)
	require.NotNil(t, modules[0].ModuleType.Model)
	assert.Equal(t, "C9400-LC-48U", *modules[0].ModuleType.Model)
}

// TestTranslateModules_FullMode_EmitsTransceiversAsSubBayedModules — in
// full mode the 9404R fixture must emit the supervisor + linecard pair
// plus a transceiver nested in its own sub-bay, AND the iface attachment
// map must route Gi1/0/1 → that transceiver Module.
func TestTranslateModules_FullMode_EmitsTransceiversAsSubBayedModules(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	oids := buildOIDs(chassis9404RWithTransceiversFixture())
	dev := &diode.Device{Name: strPtr("test-router")}
	memberDevices := map[int]*diode.Device{0: dev}

	// Transceiver EntIndex "203" sits behind ifIndex "10101" /
	// ifName "Gi1/0/1" per the fixture's alias wiring intent.
	aliasMap := map[string]string{"203": "10101"}
	ifIndexToName := map[string]string{"10101": "Gi1/0/1"}

	entities, ifaceMap := TranslateModulesWithAlias(
		oids, nil, memberDevices, modeFull(), nil, logger,
		aliasMap, ifIndexToName,
	)

	var bays []*diode.ModuleBay
	var modules []*diode.Module
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.ModuleBay:
			bays = append(bays, v)
		case *diode.Module:
			modules = append(modules, v)
		}
	}
	// Supervisor bay + linecard bay + transceiver sub-bay = 3 bays;
	// supervisor + linecard + transceiver = 3 modules.
	require.Len(t, bays, 3, "supervisor bay + linecard bay + transceiver sub-bay")
	require.Len(t, modules, 3, "supervisor + linecard + transceiver")

	// Iface map points the physical port at the transceiver module.
	require.Len(t, ifaceMap, 1)
	require.Contains(t, ifaceMap, "Gi1/0/1")
	require.NotNil(t, ifaceMap["Gi1/0/1"].Serial)
	assert.Equal(t, "FNS24010TR1", *ifaceMap["Gi1/0/1"].Serial)
}

// TestTranslateModules_FullMode_SubBayDeviceRooted — pins the sub-bay
// reconciler workaround documented in the design spec under
// "Sub-bay emission workaround". The transceiver's sub-bay MUST carry
// Device (so Diode has a matching scope) and MUST NOT carry Module
// (linking the sub-bay to its parent linecard makes the Diode
// reconciler re-plan the parent inside the sub-bay's changeset and
// trip dcim_module_module_bay_id_key on apply).
func TestTranslateModules_FullMode_SubBayDeviceRooted(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	oids := buildOIDs(chassis9404RWithTransceiversFixture())
	dev := &diode.Device{Name: strPtr("test-router")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, _ := TranslateModulesWithAlias(
		oids, nil, memberDevices, modeFull(), nil, logger, nil, nil,
	)

	// The transceiver's bay carries the port-shaped name from the
	// fixture ("TenGigabitEthernet2/0/1"). Find it and assert the
	// workaround invariants.
	var subBay *diode.ModuleBay
	for _, e := range entities {
		b, ok := e.(*diode.ModuleBay)
		if !ok || b.Name == nil {
			continue
		}
		if *b.Name == "TenGigabitEthernet2/0/1" {
			subBay = b
			break
		}
	}
	require.NotNil(t, subBay, "transceiver sub-bay must be emitted")
	assert.NotNil(t, subBay.Device,
		"sub-bay must be device-rooted (workaround for Diode reconciler)")
	assert.Nil(t, subBay.Module,
		"sub-bay must NOT carry Module=parent_linecard — see spec §Sub-bay emission workaround")
}

// TestTranslateModules_FullMode_EmptyBayEmittedAsBareModuleBay —
// Aruba CX-style empty bays surface as bare ModuleBay entities (no
// Module installed) in full mode only.
func TestTranslateModules_FullMode_EmptyBayEmittedAsBareModuleBay(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rows := []fixtureRow{
		{"1", "0", "3", "1", "Chassis", "FOO", "8400", "Chassis", ""},
		// Empty bay — class=5 under chassis, no class=9 child.
		{"700", "1", "5", "5", "Slot 5 (empty)", "", "", "Slot 5", ""},
	}
	dev := &diode.Device{Name: strPtr("test")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, _ := TranslateModulesWithAlias(
		buildOIDs(rows), nil, memberDevices, modeFull(), nil, logger, nil, nil,
	)

	var bays []*diode.ModuleBay
	var modules []*diode.Module
	for _, e := range entities {
		switch v := e.(type) {
		case *diode.ModuleBay:
			bays = append(bays, v)
		case *diode.Module:
			modules = append(modules, v)
		}
	}
	require.Len(t, bays, 1, "one bare ModuleBay for the empty slot")
	require.Empty(t, modules, "no Module entity for an empty bay")
	require.NotNil(t, bays[0].Name)
	assert.Equal(t, "Slot 5 (empty)", *bays[0].Name)
	assert.NotNil(t, bays[0].Device, "even bare bays carry Device")
}

// TestTranslateModules_SubBayWorkaround_NotLinkedToParentLinecard pins
// the Diode reconciler workaround (spec §Sub-bay emission workaround).
// Background: dcim_module_module_bay_id_key is a unique constraint; if
// we emit a transceiver sub-bay with Module=parent_linecard, the
// reconciler re-plans the parent linecard inside the sub-bay's
// changeset and the apply step trips the unique constraint. Until the
// upstream fix lands, every transceiver-shaped ModuleBay must be
// device-rooted (Device set, Module nil) and the transceiver's own
// Module.ModuleBay must in turn carry Device (so it has a matching
// scope).
func TestTranslateModules_SubBayWorkaround_NotLinkedToParentLinecard(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	oids := buildOIDs(chassis9404RWithTransceiversFixture())
	dev := &diode.Device{Name: strPtr("test-router")}
	memberDevices := map[int]*diode.Device{0: dev}

	entities, _ := TranslateModulesWithAlias(
		oids, nil, memberDevices, modeFull(), nil, logger, nil, nil,
	)

	// Identify which bays are sub-bays (transceiver-shaped). The 9404R
	// fixture names its port container "TenGigabitEthernet2/0/1", so a
	// HasPrefix("TenGigabit") match is sufficient here.
	subBaysSeen := 0
	for _, e := range entities {
		b, ok := e.(*diode.ModuleBay)
		if !ok || b.Name == nil {
			continue
		}
		name := *b.Name
		if !startsWith(name, "TenGigabit") {
			continue
		}
		subBaysSeen++
		// Workaround invariants:
		assert.Nil(t, b.Module,
			"sub-bay %q must not link to parent linecard — see spec §Sub-bay emission workaround", name)
		assert.NotNil(t, b.Device,
			"sub-bay %q must be device-rooted (Device set)", name)
	}
	require.Equal(t, 1, subBaysSeen, "expected exactly one transceiver sub-bay")

	// The transceiver Module must reach Device through its own bay too
	// (the device-rooted bay carries Device).
	for _, e := range entities {
		m, ok := e.(*diode.Module)
		if !ok || m.ModuleType == nil || m.ModuleType.Model == nil {
			continue
		}
		if *m.ModuleType.Model != "SFP-10G-LR" {
			continue
		}
		require.NotNil(t, m.ModuleBay, "transceiver Module must carry ModuleBay")
		assert.NotNil(t, m.ModuleBay.Device,
			"transceiver Module.ModuleBay must carry Device (device-rooted workaround)")
	}
}

// startsWith is a tiny local helper to keep the regression test free of
// strings.HasPrefix imports leakage in case future test refactors drop
// strings entirely.
func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
