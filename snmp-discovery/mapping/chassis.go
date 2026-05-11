// Package mapping — chassis.go: detection + translation of SNMP
// virtual-chassis / switch-stack topology into Diode VirtualChassis +
// member Device entities. Reads ENTITY-MIB entPhysicalTable + RFC 6933
// entAliasMappingTable. Vendor-neutral; relies on lowest-member-id
// master pinning for stability across stack-role failovers.
//
// Public entry point: TranslateAsStack (to be added in a later task).
package mapping

import (
	"log/slog"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// ChassisInventoryMapper is a no-op orbToEntityMapper. The
// chassis_inventory entity type exists solely so that ENTITY-MIB
// columns and entAliasMappingTable get walked into the runner's
// ObjectIDValueMap. The actual translation happens later in
// TranslateAsStack, which reads the raw oids map directly.
type ChassisInventoryMapper struct {
	logger *slog.Logger
}

// Map is intentionally a no-op — see type doc.
func (m *ChassisInventoryMapper) Map(
	_ map[ObjectIDIndex]*ObjectIDValue,
	_ *Entry,
	_ *EntityRegistry,
	_ *config.Defaults,
) diode.Entity {
	return nil
}
