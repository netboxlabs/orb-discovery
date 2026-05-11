package mapping

import (
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
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

func TestExtractInventory_DuplicateID_DifferentSerials_RefusesEmission(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":     {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":     {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":     {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":    {Value: "SERIAL-A"},
		".1.3.6.1.2.1.47.1.1.1.1.4.1000":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1000":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1000":  {Value: "1"}, // same id, different serial
		".1.3.6.1.2.1.47.1.1.1.1.11.1000": {Value: "SERIAL-B"},
	}
	inv := extractInventory(oids, logger)
	// Ambiguous: BOTH members dropped, IDs tracked for routing warns.
	assert.Empty(t, inv.Members)
	assert.Contains(t, inv.DroppedIDs, 1)
}

func TestExtractInventory_DuplicateSerial_HigherIDDropped(t *testing.T) {
	logger := slog.Default()
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":     {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":     {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":     {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":    {Value: "DUP-SERIAL"},
		".1.3.6.1.2.1.47.1.1.1.1.4.1000":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1000":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1000":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1000": {Value: "DUP-SERIAL"},
	}
	inv := extractInventory(oids, logger)
	assert.Len(t, inv.Members, 1)
	assert.Equal(t, 1, inv.Members[0].ID)
	assert.Contains(t, inv.DroppedIDs, 2)
}

func TestExtractInventory_IsStack(t *testing.T) {
	logger := slog.Default()
	assert.False(t, ChassisInventory{}.IsStack(), "empty -> standalone")
	assert.False(t, extractInventory(ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": {Value: "S"},
	}, logger).IsStack(), "1 member -> standalone")
	assert.True(t, extractInventory(fixtureCisco3850TwoMemberStack(), logger).IsStack())
}

func TestBuildMasterRef_CarriesAllMatcherFields(t *testing.T) {
	master := &diode.Device{
		Name:     strPtr("3850-stack"),
		Serial:   strPtr("FCW2147L0K3"),
		AssetTag: strPtr("ASSET-1"),
		Site:     &diode.Site{Name: strPtr("dc1")},
		Tenant:   &diode.Tenant{Name: strPtr("acme")},
		Role:     &diode.DeviceRole{Name: strPtr("access")},
		DeviceType: &diode.DeviceType{
			Model:        strPtr("WS-C3850-48P"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
		},
		PrimaryIp4: &diode.IPAddress{
			Address: strPtr("10.0.0.1/24"),
			// AssignedObject populated on rich master — MUST be stripped on ref.
			AssignedObject: &diode.Interface{Name: strPtr("Vlan1")},
		},
		// source_match value shape matches policy.setDeviceSourceMatch:
		// the value is a nested diode.Metadata with "netbox_id".
		Metadata: diode.Metadata{"source_match": diode.Metadata{"netbox_id": 42}},
	}

	ref := buildMasterRef(master)

	assert.Equal(t, "3850-stack", *ref.Name)
	assert.Equal(t, "FCW2147L0K3", *ref.Serial)
	assert.Equal(t, "ASSET-1", *ref.AssetTag)
	assert.Equal(t, "dc1", *ref.Site.Name)
	assert.Equal(t, "acme", *ref.Tenant.Name)
	assert.Equal(t, "access", *ref.Role.Name)
	assert.Equal(t, "WS-C3850-48P", *ref.DeviceType.Model)
	assert.NotNil(t, ref.PrimaryIp4)
	assert.Equal(t, "10.0.0.1/24", *ref.PrimaryIp4.Address)
	assert.Nil(t, ref.PrimaryIp4.AssignedObject,
		"primary_ip4.AssignedObject must be nil — breaks IP->Iface->Device cycle")
	assert.Nil(t, ref.VirtualChassis, "non-recursion")
	assert.Nil(t, ref.VcPosition, "VcPosition would only feed unreachable matcher #8")
	assert.Equal(t, diode.Metadata{"netbox_id": 42}, ref.Metadata["source_match"])
}

func TestBuildMasterRef_NilMasterReturnsNil(t *testing.T) {
	assert.Nil(t, buildMasterRef(nil))
}

func TestBuildMasterRef_OmitsUnsetFields(t *testing.T) {
	master := &diode.Device{Name: strPtr("x"), Serial: strPtr("y")}
	ref := buildMasterRef(master)
	assert.Nil(t, ref.AssetTag)
	assert.Nil(t, ref.PrimaryIp4)
	assert.Nil(t, ref.PrimaryIp6)
	assert.Nil(t, ref.Site)
}
