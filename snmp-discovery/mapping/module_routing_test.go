// Copyright 2026 NetBox Labs, Inc.

package mapping

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCapturingLogger builds an slog.Logger whose output lands in buf so
// individual tests can assert on warn-line presence.
func newCapturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// TestAssignMemberID_StandaloneAllZero — when chassisInv is nil the
// device is standalone; every module / sub-module / empty-bay entry must
// land on member id 0 so downstream translation emits them under master.
func TestAssignMemberID_StandaloneAllZero(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newCapturingLogger(buf)

	inv := newModuleInventory()
	inv.Modules = []ModuleEntry{
		{EntIndex: "101", Type: ModuleTypeSupervisor},
		{EntIndex: "201", Type: ModuleTypeLinecard},
	}
	inv.SubModules["201"] = []ModuleEntry{
		{EntIndex: "203", Type: ModuleTypeTransceiver, ParentEntIdx: "201"},
	}
	inv.EmptyBays = []ModuleEntry{
		{EntIndex: "300", Type: ModuleTypeUnknown},
	}

	assignMemberID(&inv, nil, ObjectIDValueMap{}, logger)

	for _, m := range inv.Modules {
		assert.Equalf(t, 0, m.MemberID, "module %s must be MemberID=0 in standalone", m.EntIndex)
	}
	for _, list := range inv.SubModules {
		for _, m := range list {
			assert.Equalf(t, 0, m.MemberID, "submodule %s must be MemberID=0 in standalone", m.EntIndex)
		}
	}
	for _, m := range inv.EmptyBays {
		assert.Equalf(t, 0, m.MemberID, "empty bay %s must be MemberID=0 in standalone", m.EntIndex)
	}
}

// TestAssignMemberID_VCMapsToMemberByChassisAncestor — module EntIndex
// "201" sits under class=5 "200" which sits under class=3 "1000". The
// chassis row "1000" belongs to member id 2 (per chassisInv.Members).
// assignMemberID must walk the containedIn chain to "1000" and stamp
// MemberID=2 on the module entry.
func TestAssignMemberID_VCMapsToMemberByChassisAncestor(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newCapturingLogger(buf)

	inv := newModuleInventory()
	inv.Modules = []ModuleEntry{
		{EntIndex: "201", Type: ModuleTypeLinecard},
	}

	chassisInv := &ChassisInventory{
		Members: []ChassisMember{
			{ID: 1, EntPhysicalIndex: "1"},
			{ID: 2, EntPhysicalIndex: "1000"},
		},
	}

	// containedIn chain: 201 -> 200 (class=5) -> 1000 (class=3).
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.201":  Value{Value: "200"},
		".1.3.6.1.2.1.47.1.1.1.1.5.201":  Value{Value: "9"},
		".1.3.6.1.2.1.47.1.1.1.1.4.200":  Value{Value: "1000"},
		".1.3.6.1.2.1.47.1.1.1.1.5.200":  Value{Value: "5"},
		".1.3.6.1.2.1.47.1.1.1.1.4.1000": Value{Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1000": Value{Value: "3"},
	}

	assignMemberID(&inv, chassisInv, oids, logger)

	require.Len(t, inv.Modules, 1)
	assert.Equal(t, 2, inv.Modules[0].MemberID,
		"module 201 chain terminates at chassis 1000 -> member 2")
}

// TestAssignMemberID_OrphanMember_DroppedOrLogged — a module whose
// chassis ancestor is NOT in chassisInv.Members must be left with the
// sentinel MemberID=-1 (translation step skips MemberID<0) and a warn
// log must fire so operators can spot the orphan.
func TestAssignMemberID_OrphanMember_DroppedOrLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newCapturingLogger(buf)

	inv := newModuleInventory()
	inv.Modules = []ModuleEntry{
		{EntIndex: "401", Type: ModuleTypeLinecard},
	}

	chassisInv := &ChassisInventory{
		Members: []ChassisMember{
			{ID: 1, EntPhysicalIndex: "1"},
			{ID: 2, EntPhysicalIndex: "1000"},
		},
	}

	// 401 -> 400 (class=5) -> 9999 (class=3). 9999 is not in chassisInv.
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.401":  Value{Value: "400"},
		".1.3.6.1.2.1.47.1.1.1.1.5.401":  Value{Value: "9"},
		".1.3.6.1.2.1.47.1.1.1.1.4.400":  Value{Value: "9999"},
		".1.3.6.1.2.1.47.1.1.1.1.5.400":  Value{Value: "5"},
		".1.3.6.1.2.1.47.1.1.1.1.4.9999": Value{Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.9999": Value{Value: "3"},
	}

	assignMemberID(&inv, chassisInv, oids, logger)

	require.Len(t, inv.Modules, 1)
	assert.Equal(t, -1, inv.Modules[0].MemberID,
		"orphan module must carry MemberID=-1 sentinel for the skip path")
	assert.Contains(t, buf.String(), "orphan",
		"orphan module must produce a warn log")
}

// TestAssignMemberID_VCMasterKeyedByLowestMemberID — modules under the
// master chassis (entPhysicalIndex == Members[0].EntPhysicalIndex) must
// carry the lowest member id (Members[0].ID == 1), NOT 0. The "0 means
// standalone" rule applies only when chassisInv is nil/empty.
func TestAssignMemberID_VCMasterKeyedByLowestMemberID(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := newCapturingLogger(buf)

	inv := newModuleInventory()
	inv.Modules = []ModuleEntry{
		{EntIndex: "101", Type: ModuleTypeLinecard},
	}

	chassisInv := &ChassisInventory{
		Members: []ChassisMember{
			{ID: 1, EntPhysicalIndex: "1"},
			{ID: 2, EntPhysicalIndex: "1000"},
		},
	}

	// 101 -> 100 (class=5) -> 1 (class=3, master).
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.101": Value{Value: "100"},
		".1.3.6.1.2.1.47.1.1.1.1.5.101": Value{Value: "9"},
		".1.3.6.1.2.1.47.1.1.1.1.4.100": Value{Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.5.100": Value{Value: "5"},
		".1.3.6.1.2.1.47.1.1.1.1.4.1":   Value{Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":   Value{Value: "3"},
	}

	assignMemberID(&inv, chassisInv, oids, logger)

	require.Len(t, inv.Modules, 1)
	assert.Equal(t, 1, inv.Modules[0].MemberID,
		"module under master chassis must carry the lowest member id (1), not 0")
}

// --- buildIfaceModuleMap tests ---

// TestBuildIfaceModuleMap_HappyPath — transceiver EntIndex "203" routes
// through aliasMap -> ifIndex "10101" -> ifIndexToName -> "Gi1/0/1".
// The emitted Module at emittedModules["203"] must appear keyed by
// "Gi1/0/1" in the result so the runner can later set Interface.Module.
func TestBuildIfaceModuleMap_HappyPath(t *testing.T) {
	inv := newModuleInventory()
	inv.SubModules["201"] = []ModuleEntry{
		{EntIndex: "203", Type: ModuleTypeTransceiver, ParentEntIdx: "201"},
	}
	aliasMap := map[string]string{"203": "10101"}
	ifIndexToName := map[string]string{"10101": "Gi1/0/1"}
	mod := &diode.Module{}
	emitted := map[string]*diode.Module{"203": mod}

	got := buildIfaceModuleMap(inv, aliasMap, ifIndexToName, emitted)

	require.Contains(t, got, "Gi1/0/1")
	assert.Same(t, mod, got["Gi1/0/1"], "result must point at the emittedModules entry")
	assert.Len(t, got, 1, "only the transceiver routes; no extra keys")
}

// TestBuildIfaceModuleMap_TransceiverWithoutAliasSkipped — a transceiver
// missing from aliasMap is silently skipped (no ifName to bind to).
func TestBuildIfaceModuleMap_TransceiverWithoutAliasSkipped(t *testing.T) {
	inv := newModuleInventory()
	inv.SubModules["201"] = []ModuleEntry{
		{EntIndex: "203", Type: ModuleTypeTransceiver, ParentEntIdx: "201"},
	}
	emitted := map[string]*diode.Module{"203": {}}

	got := buildIfaceModuleMap(inv, map[string]string{}, map[string]string{"10101": "Gi1/0/1"}, emitted)

	assert.Empty(t, got, "no aliasMap entry -> transceiver not in result")
}

// TestBuildIfaceModuleMap_TransceiverWithoutIfNameSkipped — aliasMap
// resolves but the ifIndexToName lookup misses. Skip with no panic.
func TestBuildIfaceModuleMap_TransceiverWithoutIfNameSkipped(t *testing.T) {
	inv := newModuleInventory()
	inv.SubModules["201"] = []ModuleEntry{
		{EntIndex: "203", Type: ModuleTypeTransceiver, ParentEntIdx: "201"},
	}
	emitted := map[string]*diode.Module{"203": {}}

	got := buildIfaceModuleMap(inv, map[string]string{"203": "10101"}, map[string]string{}, emitted)

	assert.Empty(t, got, "ifIndex without an ifName entry -> transceiver not in result")
}

