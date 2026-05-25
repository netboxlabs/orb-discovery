// Package mapping — module.go: scaffold for chassis module / module
// bay discovery on modular Cisco IOS-XE chassis. ChassisModuleMapper
// is intentionally a no-op; it exists only to register the
// entPhysicalDescr + entPhysicalVendorType walk columns. The actual
// translation lives in TranslateModulesWithAlias in module_translate.go.
package mapping

import (
	"log/slog"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
)

// ChassisModuleMapper is a no-op orbToEntityMapper. See package file
// doc — data flows through the raw oids map into
// TranslateModulesWithAlias, not via this Map call.
type ChassisModuleMapper struct {
	logger *slog.Logger
}

// Map is intentionally a no-op — see type doc.
func (m *ChassisModuleMapper) Map(
	_ map[ObjectIDIndex]*ObjectIDValue,
	_ *Entry,
	_ *EntityRegistry,
	_ *config.Defaults,
) diode.Entity {
	return nil
}
