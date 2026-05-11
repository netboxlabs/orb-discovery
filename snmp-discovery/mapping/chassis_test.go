package mapping

import (
	"log/slog"
	"testing"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/stretchr/testify/assert"
)

// TestChassisInventoryMapper_IsNoOp confirms the mapper accepts
// chassis_inventory PDUs without producing entities. Inventory data
// is consumed by TranslateAsStack later, not by this Map call.
func TestChassisInventoryMapper_IsNoOp(t *testing.T) {
	logger := slog.Default()
	registry := NewEntityRegistry(logger)
	mapper := &ChassisInventoryMapper{logger: logger}

	entry := &Entry{
		Entity: string(ChassisInventoryEntityType),
		Field:  "_id",
	}
	// Synthesize one entPhysicalSerialNum row.
	values := map[ObjectIDIndex]*ObjectIDValue{
		"11.1": {
			OID:    ".1.3.6.1.2.1.47.1.1.1.1.11.1",
			Index:  "11.1",
			Parent: ".1.3.6.1.2.1.47.1.1.1.1.11",
			Value:  "FCW123",
		},
	}

	result := mapper.Map(values, entry, registry, &config.Defaults{})
	assert.Nil(t, result, "ChassisInventoryMapper must not emit entities directly")
}

func TestExtractInventory_TwoMemberStack(t *testing.T) {
	logger := slog.Default()
	inv := extractInventory(fixtureCisco3850TwoMemberStack(), logger)

	assert.Len(t, inv.Members, 2)
	assert.Equal(t, 1, inv.Members[0].ID)
	assert.Equal(t, "FCW2147L0K3", inv.Members[0].Serial)
	assert.Equal(t, "WS-C3850-48P", inv.Members[0].Model)
	assert.Equal(t, "Switch 1", inv.Members[0].EntName)
	assert.Equal(t, "1", inv.Members[0].EntPhysicalIndex)

	assert.Equal(t, 2, inv.Members[1].ID)
	assert.Equal(t, "FCW2147L0K4", inv.Members[1].Serial)
	assert.Equal(t, "1000", inv.Members[1].EntPhysicalIndex)
}

func TestExtractInventory_StandaloneSingleChassis(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": {Value: "FOC1234ABCD"},
		".1.3.6.1.2.1.47.1.1.1.1.13.1": {Value: "ISR4321"},
	}
	inv := extractInventory(oids, logger)
	assert.Len(t, inv.Members, 1)
	assert.Equal(t, 1, inv.Members[0].ID)
	assert.Equal(t, "FOC1234ABCD", inv.Members[0].Serial)
}

func TestExtractInventory_NonChassisRowsIgnored(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		// Module (class=9) — must NOT show up as a member.
		".1.3.6.1.2.1.47.1.1.1.1.4.5":  {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.5.5":  {Value: "9"},
		".1.3.6.1.2.1.47.1.1.1.1.11.5": {Value: "MOD-SERIAL"},
		// True chassis (class=3, containedIn=0).
		".1.3.6.1.2.1.47.1.1.1.1.4.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": {Value: "REAL-CHASSIS"},
	}
	inv := extractInventory(oids, logger)
	assert.Len(t, inv.Members, 1)
	assert.Equal(t, "REAL-CHASSIS", inv.Members[0].Serial)
}

func TestExtractInventory_EmptySerialDropped(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":     {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":     {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":    {Value: ""},
		".1.3.6.1.2.1.47.1.1.1.1.4.1000":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1000":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1000":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1000": {Value: "VALID"},
	}
	inv := extractInventory(oids, logger)
	assert.Len(t, inv.Members, 1)
	assert.Equal(t, "VALID", inv.Members[0].Serial)
}

func TestDeriveMemberID_ParentRelPosWins(t *testing.T) {
	m := ChassisMember{ParentRelPos: 5, EntName: "Switch 9"}
	assert.Equal(t, 5, deriveMemberID(m, 0))
}

func TestDeriveMemberID_NameTrailingIntFallback(t *testing.T) {
	cases := []struct {
		entName string
		want    int
	}{
		{"Switch 1", 1},
		{"Switch 2", 2},
		{"FPC 0", 0},
		{"Member 7", 7},
		{"Virtual Chassis Member 3", 3},
		{"Chassis 12", 12},
	}
	for _, tc := range cases {
		t.Run(tc.entName, func(t *testing.T) {
			m := ChassisMember{ParentRelPos: 0, EntName: tc.entName}
			assert.Equal(t, tc.want, deriveMemberID(m, 99))
		})
	}
}

func TestDeriveMemberID_FinalIndexFallback(t *testing.T) {
	// parentRelPos=0, EntName has no trailing int → use ordinal fallback.
	m := ChassisMember{ParentRelPos: 0, EntName: "Chassis"}
	assert.Equal(t, 4, deriveMemberID(m, 4))
}

func TestExtractInventory_JunosFPC_TrailingIntFromName(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		// 3 FPC members with parentRelPos=0 (Junos doesn't populate it).
		// IDs must come from "FPC 0", "FPC 1", "FPC 2" trailing-int parse.
		".1.3.6.1.2.1.47.1.1.1.1.4.10":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.10":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.7.10":  {Value: "FPC 0"},
		".1.3.6.1.2.1.47.1.1.1.1.11.10": {Value: "BR0001"},
		".1.3.6.1.2.1.47.1.1.1.1.4.20":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.20":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.7.20":  {Value: "FPC 1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.20": {Value: "BR0002"},
		".1.3.6.1.2.1.47.1.1.1.1.4.30":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.30":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.7.30":  {Value: "FPC 2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.30": {Value: "BR0003"},
	}
	inv := extractInventory(oids, logger)
	assert.Len(t, inv.Members, 3)
	assert.Equal(t, 0, inv.Members[0].ID, "FPC 0 -> id 0")
	assert.Equal(t, 1, inv.Members[1].ID, "FPC 1 -> id 1")
	assert.Equal(t, 2, inv.Members[2].ID, "FPC 2 -> id 2")
}
