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
