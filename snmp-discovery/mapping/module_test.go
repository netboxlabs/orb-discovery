package mapping

import (
	"log/slog"
	"testing"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/stretchr/testify/assert"
)

// TestChassisModuleMapper_MapReturnsNil confirms the mapper accepts
// chassis_module PDUs without producing entities. Module data is
// consumed by TranslateModulesWithAlias later, not by this Map call.
func TestChassisModuleMapper_MapReturnsNil(t *testing.T) {
	logger := slog.Default()
	registry := NewEntityRegistry(logger)
	mapper := &ChassisModuleMapper{logger: logger}

	entry := &Entry{
		Entity: string(ChassisModuleEntityType),
		Field:  "_id",
	}
	// Synthesize one entPhysicalDescr row.
	values := map[ObjectIDIndex]*ObjectIDValue{
		"2.1": {
			OID:    ".1.3.6.1.2.1.47.1.1.1.1.2.1",
			Index:  "2.1",
			Parent: ".1.3.6.1.2.1.47.1.1.1.1.2",
			Value:  "Linecard-1",
		},
	}

	result := mapper.Map(values, entry, registry, &config.Defaults{})
	assert.Nil(t, result, "ChassisModuleMapper must not emit entities directly")
}

// TestNewModuleInventory_HasEmptyMaps asserts the constructor produces
// a usable zero-value: SubModules ready for keyed writes, Modules and
// EmptyBays untouched (nil slices append fine).
func TestNewModuleInventory_HasEmptyMaps(t *testing.T) {
	inv := newModuleInventory()
	assert.NotNil(t, inv.SubModules, "newModuleInventory must initialise the SubModules map")
	assert.Empty(t, inv.SubModules)
	assert.Empty(t, inv.Modules, "Modules starts as an empty/nil slice")
	assert.Nil(t, inv.EmptyBays, "EmptyBays is lazily initialised; nil is fine on construct")
}

// TestClassifyModule walks the decision matrix: parent-depth gates
// transceiver/supervisor, PID prefixes pick PSU/Fan, everything else
// falls through to linecard. Both empty inputs → unknown.
func TestClassifyModule(t *testing.T) {
	cases := []struct {
		name            string
		model           string
		vendorType      string
		hasModuleParent bool
		want            ModuleType
	}{
		// Transceiver: under a class=9 module parent AND optic PID.
		{"qsfp-100g-sr4 under linecard", "QSFP-100G-SR4", "", true, ModuleTypeTransceiver},
		{"sfp-10g-lr under linecard", "SFP-10G-LR", "", true, ModuleTypeTransceiver},
		{"x2-10g under linecard", "X2-10GB-SR", "", true, ModuleTypeTransceiver},

		// Supervisor: at chassis level AND SUP pattern.
		{"sup at chassis depth", "C9400-SUP-1", "", false, ModuleTypeSupervisor},
		{"sup2 at chassis depth", "VS-SUP2T-10G", "", false, ModuleTypeSupervisor},

		// Linecard defaults at chassis depth.
		{"linecard PID at chassis depth", "C9400-LC-48U", "", false, ModuleTypeLinecard},
		{"unknown PID at chassis depth", "FOO-BAR-9000", "", false, ModuleTypeLinecard},

		// PSU and Fan classified anywhere.
		{"psu pwr prefix", "PWR-C5-715WAC", "", false, ModuleTypePSU},
		{"psu psu prefix", "PSU-2KW-AC", "", false, ModuleTypePSU},
		{"fan prefix", "FAN-T2", "", false, ModuleTypeFan},

		// Edge: under module parent but non-optic → linecard (depth alone insufficient).
		{"non-optic under module parent", "WS-X45-FOO", "", true, ModuleTypeLinecard},

		// Edge: optic-shaped PID at chassis level → linecard (PID alone insufficient).
		{"optic PID at chassis depth", "QSFP-100G-SR4", "", false, ModuleTypeLinecard},

		// VendorType fallback when Model is blank.
		{"vendortype fallback optic", "", "QSFP-100G-LR4", true, ModuleTypeTransceiver},

		// Both blank → unknown.
		{"both blank", "", "", false, ModuleTypeUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyModule(c.model, c.vendorType, c.hasModuleParent)
			assert.Equal(t, c.want, got)
		})
	}
}
