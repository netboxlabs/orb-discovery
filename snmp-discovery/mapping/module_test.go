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
