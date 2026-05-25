// Package mapping — module.go: scaffold for chassis module / module
// bay discovery on modular Cisco IOS-XE chassis. ChassisModuleMapper
// is intentionally a no-op; it exists only to register the
// entPhysicalDescr + entPhysicalVendorType walk columns. The actual
// translation lives in TranslateModulesWithAlias in module_translate.go.
package mapping

import (
	"log/slog"
	"strings"

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

// ModuleType is the vendor-neutral classification assigned by
// classifyModule. Drives both the linecards-mode filter (skip
// transceivers) and downstream Diode emission as Module.Type.
type ModuleType string

const (
	ModuleTypeLinecard    ModuleType = "linecard"
	ModuleTypeSupervisor  ModuleType = "supervisor"
	ModuleTypeTransceiver ModuleType = "transceiver"
	ModuleTypePSU         ModuleType = "psu"     // classified for labelling only; never emitted as a module entity
	ModuleTypeFan         ModuleType = "fan"     // classified for labelling only; never emitted as a module entity
	ModuleTypeUnknown     ModuleType = "unknown"
)

// ModuleEntry is one class=9 row from entPhysicalTable after
// classification. BayEntIndex points at the class=5 container that
// owns this module — the bay is emitted as a separate entity
// alongside the module installed in it.
type ModuleEntry struct {
	EntIndex     string     // own entPhysicalIndex
	BayEntIndex  string     // parent class=5 container entPhysicalIndex
	BayName      string     // entPhysicalName of the bay
	BayPosition  string     // entPhysicalParentRelPos of the module
	Name         string     // entPhysicalName
	Serial       string     // entPhysicalSerialNum
	Model        string     // entPhysicalModelName
	Description  string     // entPhysicalDescr
	VendorType   string     // entPhysicalVendorType
	Type         ModuleType // classifyModule output
	MemberID     int        // ChassisInventory.Members[].ID; 0 for standalone
	ParentEntIdx string     // for transceivers, class=9 module they sit under; "" for top-level
}

// ModuleInventory is the deduped, classified set for one target.
// Modules carries top-level (chassis-rooted) modules; SubModules maps
// each parent EntIndex → its transceiver children. EmptyBays carries
// class=5 rows with no class=9 child but whose parent resolves to a
// chassis or container — Aruba CX-style empty slots; emitted only in
// `full` mode.
type ModuleInventory struct {
	Modules    []ModuleEntry
	SubModules map[string][]ModuleEntry
	EmptyBays  []ModuleEntry // bare bays — BayEntIndex == EntIndex; Serial/Model empty
}

func newModuleInventory() ModuleInventory {
	return ModuleInventory{
		SubModules: make(map[string][]ModuleEntry),
	}
}

// Optic PID prefixes — pluggable transceivers across Cisco / generic
// vendors. Matched only when the row sits under a class=9 module
// parent; PID alone is insufficient (a chassis-level optic-shaped PID
// is treated as a linecard).
var opticPIDPrefixes = []string{"QSFP-", "SFP-", "X2-", "GLC-", "CFP-", "XENPAK-", "XFP-"}

// classifyModule picks a ModuleType from a row's PID and its location
// in the containment tree. hasModuleParent is true when an ancestor in
// the entPhysicalTable chain is itself class=9. Effective PID prefers
// trimmed Model and falls back to trimmed VendorType when Model is
// blank — Aruba CX populates VendorType where Cisco populates Model.
func classifyModule(model, vendorType string, hasModuleParent bool) ModuleType {
	pid := strings.TrimSpace(model)
	if pid == "" {
		pid = strings.TrimSpace(vendorType)
	}

	// PSU / Fan are vendor-neutral by PID prefix and parent-agnostic —
	// they appear at chassis level and under shelves alike.
	upper := strings.ToUpper(pid)
	if strings.HasPrefix(upper, "PSU-") || strings.HasPrefix(upper, "PWR-") {
		return ModuleTypePSU
	}
	if strings.HasPrefix(upper, "FAN") {
		return ModuleTypeFan
	}

	// Transceiver requires BOTH a module-class ancestor AND an optic
	// PID — depth alone catches non-optic sub-modules; PID alone
	// catches spare optics inventoried at chassis level.
	if hasModuleParent {
		for _, p := range opticPIDPrefixes {
			if strings.HasPrefix(upper, p) {
				return ModuleTypeTransceiver
			}
		}
	} else if isSupervisorPID(upper) {
		// Supervisor lives at chassis depth on dual-sup platforms.
		return ModuleTypeSupervisor
	}

	if pid == "" {
		return ModuleTypeUnknown
	}

	// Safe default — non-optic under a module parent OR no special
	// pattern at chassis level both land here.
	return ModuleTypeLinecard
}

// isSupervisorPID recognises Cisco-style supervisor product IDs on an
// already upper-cased PID. Covers C9400-SUP-1 (dash-delimited), VS-SUP2T
// (digit-suffixed, no trailing dash) and SUPV variants.
func isSupervisorPID(upper string) bool {
	if strings.Contains(upper, "SUP-") || strings.Contains(upper, "-SUP-") || strings.Contains(upper, "SUPV") {
		return true
	}
	// SUP followed by a digit — VS-SUP2T-10G, SUP6T, SUP7, etc.
	for i := 0; i+3 < len(upper); i++ {
		if upper[i] == 'S' && upper[i+1] == 'U' && upper[i+2] == 'P' {
			c := upper[i+3]
			if c >= '0' && c <= '9' {
				return true
			}
		}
	}
	return false
}
