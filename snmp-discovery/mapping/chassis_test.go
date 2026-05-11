package mapping

import (
	"log/slog"
	"os"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/snmp-discovery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
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

func TestBuildMemberDevice_CarriesVcPositionAndMatcherBlock(t *testing.T) {
	master := &diode.Device{
		Name:     strPtr("3850-stack"),
		Site:     &diode.Site{Name: strPtr("dc1")},
		Tenant:   &diode.Tenant{Name: strPtr("acme")},
		Role:     &diode.DeviceRole{Name: strPtr("access")},
		Platform: &diode.Platform{Name: strPtr("ios-xe")},
		AssetTag: strPtr("MASTER-ASSET"),
		DeviceType: &diode.DeviceType{
			Model:        strPtr("WS-C3850-48P"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
		},
	}
	masterRef := buildMasterRef(master)
	member := ChassisMember{ID: 2, Serial: "FCW2147L0K4", Model: "WS-C3850-12X"}

	dev := buildMemberDevice(master, member, masterRef, "3850-stack")

	assert.Equal(t, "3850-stack-stack-2", *dev.Name)
	assert.Equal(t, "FCW2147L0K4", *dev.Serial)
	assert.Nil(t, dev.AssetTag, "AssetTag must be CLEARED on members")
	assert.Equal(t, int64(2), *dev.VcPosition)
	assert.NotNil(t, dev.VirtualChassis)
	assert.Equal(t, "3850-stack", *dev.VirtualChassis.Name)
	assert.NotNil(t, dev.VirtualChassis.Master)
	assert.Equal(t, "3850-stack", *dev.VirtualChassis.Master.Name)
	assert.Nil(t, dev.VirtualChassis.Master.VirtualChassis, "non-recursion")

	assert.Equal(t, "dc1", *dev.Site.Name)
	assert.Equal(t, "acme", *dev.Tenant.Name)
	assert.Equal(t, "access", *dev.Role.Name)
	assert.Equal(t, "ios-xe", *dev.Platform.Name)

	// Per-member DeviceType from entPhysicalModelName, not master's.
	assert.Equal(t, "WS-C3850-12X", *dev.DeviceType.Model)
}

func TestBuildMemberDevice_FallsBackToMasterDeviceTypeWhenModelEmpty(t *testing.T) {
	master := &diode.Device{
		Name: strPtr("stack"),
		DeviceType: &diode.DeviceType{
			Model:        strPtr("ModelA"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("VendorA")},
		},
	}
	masterRef := buildMasterRef(master)
	member := ChassisMember{ID: 2, Serial: "X", Model: ""}

	dev := buildMemberDevice(master, member, masterRef, "stack")
	assert.Equal(t, "ModelA", *dev.DeviceType.Model,
		"member device_type falls back to master when entPhysicalModelName is empty")
}

func TestTranslateAsStack_StandaloneSetsSerialAndReturnsUnchangedShape(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{Name: strPtr("standalone")}
	iface := &diode.Interface{Name: strPtr("Gi0/0/0"), Device: master}
	entities := []diode.Entity{master, iface}
	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1": {Value: "FOC0001"},
	}

	out := TranslateAsStack(entities, oids, nil, &config.Defaults{}, logger)

	assert.Len(t, out, 2, "shape unchanged on standalone")
	assert.Equal(t, "FOC0001", *master.Serial)
}

func TestTranslateAsStack_TwoMemberStackEmitsVCAndMember(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{
		Name: strPtr("3850-stack.example"),
		Site: &diode.Site{Name: strPtr("dc1")},
		DeviceType: &diode.DeviceType{
			Model:        strPtr("WS-C3850-48P"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
		},
	}
	ifaceM1 := &diode.Interface{Name: strPtr("GigabitEthernet1/0/1"), Device: master}
	ifaceM2 := &diode.Interface{Name: strPtr("GigabitEthernet2/0/1"), Device: master}
	entities := []diode.Entity{master, ifaceM1, ifaceM2}
	// No alias-table coverage in this fixture — ifName parsing drives routing.
	ifIndexByIface := map[*diode.Interface]int{}

	out := TranslateAsStack(entities, fixtureCisco3850TwoMemberStack(), ifIndexByIface, &config.Defaults{}, logger)

	// master + VC + 1 member + 2 interfaces = 5
	var vc *diode.VirtualChassis
	var members []*diode.Device
	for _, e := range out {
		switch v := e.(type) {
		case *diode.VirtualChassis:
			vc = v
		case *diode.Device:
			if v != master {
				members = append(members, v)
			}
		}
	}
	assert.NotNil(t, vc)
	assert.Equal(t, "3850-stack.example", *vc.Name)
	assert.Len(t, members, 1)
	assert.Equal(t, "FCW2147L0K4", *members[0].Serial)
	assert.Equal(t, int64(2), *members[0].VcPosition)

	// Master remains plain (no VcPosition, no VirtualChassis).
	assert.Nil(t, master.VcPosition)
	assert.Nil(t, master.VirtualChassis)
	assert.Equal(t, "FCW2147L0K3", *master.Serial,
		"master Serial set from chassis row 1")

	// Interface routing: Gi1/0/1 -> master; Gi2/0/1 -> member.
	assert.Equal(t, master, ifaceM1.Device, "Gi1/0/1 stays on master")
	assert.Equal(t, "3850-stack.example-stack-2", *ifaceM2.Device.Name,
		"Gi2/0/1 routes to member 2")
}

func TestTranslateAsStack_DroppedMemberIfaceSkippedWithWarn(t *testing.T) {
	// 3-row inventory with member 2 dropped via duplicate id; an
	// interface named Gi2/0/1 must be EXCLUDED from the output
	// (not silently routed to master).
	logger := slog.Default()
	master := &diode.Device{Name: strPtr("stack"), DeviceType: &diode.DeviceType{Model: strPtr("X")}}
	orphan := &diode.Interface{Name: strPtr("Gi2/0/1"), Device: master}
	memberIface := &diode.Interface{Name: strPtr("Gi3/0/1"), Device: master}
	entities := []diode.Entity{master, orphan, memberIface}

	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":   {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":   {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":   {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":  {Value: "S1"},
		// Two rows both claiming id=2 -> dropped as ambiguous.
		".1.3.6.1.2.1.47.1.1.1.1.4.20":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.20":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.20":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.20": {Value: "S2-A"},
		".1.3.6.1.2.1.47.1.1.1.1.4.30":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.30":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.30":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.30": {Value: "S2-B"},
		// Surviving member 3.
		".1.3.6.1.2.1.47.1.1.1.1.4.40":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.40":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.40":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.40": {Value: "S3"},
	}

	out := TranslateAsStack(entities, oids, nil, &config.Defaults{}, logger)

	// Orphan (Gi2/0/1) is excluded.
	for _, e := range out {
		if iface, ok := e.(*diode.Interface); ok {
			assert.NotEqual(t, "Gi2/0/1", *iface.Name,
				"orphaned member-2 port must be skipped, not routed to master")
		}
	}
	// Member-3 port survives.
	found := false
	for _, e := range out {
		if iface, ok := e.(*diode.Interface); ok && *iface.Name == "Gi3/0/1" {
			found = true
		}
	}
	assert.True(t, found, "member-3 port must remain in the output")
}

// TestTranslateAsStack_IPRoutedToMemberViaAssignedObject guards
// finding #12: MapObjectIDsToEntity drops IP-assigned interfaces from
// top-level emission, so member-owned interfaces visible only through
// IP.AssignedObject must still be rerouted from master to member.
func TestTranslateAsStack_IPRoutedToMemberViaAssignedObject(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{
		Name: strPtr("3850-stack.example"),
		DeviceType: &diode.DeviceType{
			Model:        strPtr("WS-C3850-48P"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
		},
	}
	// Member-2 iface present only via IP.AssignedObject — NOT as a
	// top-level Interface entity. Today's pipeline strips it from
	// top-level when an IP references it.
	memberIface := &diode.Interface{Name: strPtr("GigabitEthernet2/0/24"), Device: master}
	memberIP := &diode.IPAddress{
		Address:        strPtr("10.0.2.24/24"),
		AssignedObject: memberIface,
	}
	entities := []diode.Entity{master, memberIP}

	out := TranslateAsStack(entities, fixtureCisco3850TwoMemberStack(), nil, &config.Defaults{}, logger)

	// The IP survived and its nested Interface.Device now points at member-2.
	var seenIP *diode.IPAddress
	for _, e := range out {
		if ip, ok := e.(*diode.IPAddress); ok {
			seenIP = ip
		}
	}
	assert.NotNil(t, seenIP)
	iface, _ := seenIP.AssignedObject.(*diode.Interface)
	assert.Equal(t, "3850-stack.example-stack-2", *iface.Device.Name,
		"IP.AssignedObject.Interface.Device must be re-pointed to member-2")
}

// TestTranslateAsStack_OrphanIPFiltered guards finding #12: an IP
// assigned to an interface that was skipped (parsed to a dropped
// member id) must NOT be ingested — otherwise NetBox sees an IP
// with a dangling AssignedObject.
func TestTranslateAsStack_OrphanIPFiltered(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{Name: strPtr("stack"), DeviceType: &diode.DeviceType{Model: strPtr("X")}}
	orphanIface := &diode.Interface{Name: strPtr("Gi2/0/1"), Device: master}
	orphanIP := &diode.IPAddress{
		Address:        strPtr("10.0.0.99/24"),
		AssignedObject: orphanIface,
	}
	entities := []diode.Entity{master, orphanIface, orphanIP}

	oids := ObjectIDValueMap{
		".1.3.6.1.2.1.47.1.1.1.1.4.1":   {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.1":   {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.1":   {Value: "1"},
		".1.3.6.1.2.1.47.1.1.1.1.11.1":  {Value: "S1"},
		// Member 2 duplicated -> dropped as ambiguous.
		".1.3.6.1.2.1.47.1.1.1.1.4.20":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.20":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.20":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.20": {Value: "S2-A"},
		".1.3.6.1.2.1.47.1.1.1.1.4.30":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.30":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.30":  {Value: "2"},
		".1.3.6.1.2.1.47.1.1.1.1.11.30": {Value: "S2-B"},
		// Real member 3.
		".1.3.6.1.2.1.47.1.1.1.1.4.40":  {Value: "0"},
		".1.3.6.1.2.1.47.1.1.1.1.5.40":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.6.40":  {Value: "3"},
		".1.3.6.1.2.1.47.1.1.1.1.11.40": {Value: "S3"},
	}

	out := TranslateAsStack(entities, oids, nil, &config.Defaults{}, logger)

	for _, e := range out {
		_, isIP := e.(*diode.IPAddress)
		assert.False(t, isIP, "IP assigned to a skipped (orphan) interface must be filtered")
	}
}

func TestTranslateAsStack_ArubaCX_2MemberVSF(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{
		Name: strPtr("aruba-cx-stack"),
		Site: &diode.Site{Name: strPtr("dc1")},
		DeviceType: &diode.DeviceType{
			Model:        strPtr("Aruba-6300M-48G"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("HPE Aruba")},
		},
	}
	memberIface := &diode.Interface{Name: strPtr("2/1/24"), Device: master}
	entities := []diode.Entity{master, memberIface}

	out := TranslateAsStack(entities, fixtureArubaCX2MemberVSF(), nil, &config.Defaults{}, logger)

	var members []*diode.Device
	for _, e := range out {
		if d, ok := e.(*diode.Device); ok && d != master {
			members = append(members, d)
		}
	}
	assert.Len(t, members, 1)
	assert.Equal(t, "SG12346", *members[0].Serial)
	assert.Equal(t, "aruba-cx-stack-stack-2", *memberIface.Device.Name)
}

func TestTranslateAsStack_JunosQFX_4MemberVC(t *testing.T) {
	logger := slog.Default()
	master := &diode.Device{
		Name: strPtr("vc-edge-01"),
		Site: &diode.Site{Name: strPtr("dc1")},
		DeviceType: &diode.DeviceType{
			Model:        strPtr("EX4300-48T"),
			Manufacturer: &diode.Manufacturer{Name: strPtr("Juniper")},
		},
	}
	fpc2Iface := &diode.Interface{Name: strPtr("xe-2/0/0"), Device: master}
	entities := []diode.Entity{master, fpc2Iface}

	out := TranslateAsStack(entities, fixtureJunosQFX4MemberVC(), nil, &config.Defaults{}, logger)

	var members []*diode.Device
	for _, e := range out {
		if d, ok := e.(*diode.Device); ok && d != master {
			members = append(members, d)
		}
	}
	assert.Len(t, members, 3, "4-member VC -> master + 3 member Devices")

	// Master pinned to lowest id (FPC 0).
	assert.Equal(t, "BR0000000001", *master.Serial)

	// xe-2/0/0 routes to FPC 2 member.
	assert.Equal(t, "vc-edge-01-stack-2", *fpc2Iface.Device.Name)
}

// stubManufacturers and stubDeviceLookup satisfy the data.ManufacturerRetriever
// and data.DeviceRetriever interfaces with harmless no-op implementations so
// that chassis tests can run through the full DeviceMapper code path without
// loading the real data files.
type stubManufacturers struct{}

func (stubManufacturers) GetManufacturer(_ string) (string, error) { return "Unknown", nil }

type stubDeviceLookup struct{}

func (stubDeviceLookup) GetDevice(_ string) (string, error)                     { return "", nil }
func (stubDeviceLookup) GetDeviceModel(_ string, _ map[string]string) (string, error) {
	return "", nil
}

// newTestMappingConfig reads the production mapping.yaml and builds a
// *Config using the same call path as the runner.
func newTestMappingConfig(t *testing.T, logger *slog.Logger) *Config {
	t.Helper()
	data, err := os.ReadFile("../policy/mapping.yaml")
	require.NoError(t, err, "read ../policy/mapping.yaml")
	var mc config.Mapping
	require.NoError(t, yaml.Unmarshal(data, &mc), "unmarshal mapping.yaml")
	cfg, err := NewConfig(mc.Entries, logger, stubManufacturers{}, stubDeviceLookup{}, &config.Defaults{}, config.Options{})
	require.NoError(t, err, "NewConfig from production mapping.yaml")
	return cfg
}

// TestTranslateAsStack_Idempotent_ThroughFullMapperPipeline runs the full
// MapObjectIDsToEntity → TranslateAsStack pipeline twice on identical OID
// input and asserts that both runs produce byte-equivalent proto output.
// This catches nondeterminism in either stage (Go-map iteration,
// pointer reuse, etc.).
func TestTranslateAsStack_Idempotent_ThroughFullMapperPipeline(t *testing.T) {
	logger := slog.Default()

	// Build a complete OID map: chassis inventory + ifTable rows so
	// MapObjectIDsToEntity emits Devices and Interfaces, then
	// TranslateAsStack rewrites them.
	//
	// IdentifierSize must match what the real walker sets: the runner
	// calls mappingConfig.GenericObjectIDs() which returns identifierSize=1
	// for every OID (child entries with IdentifierSize==0 default to 1).
	// Without this the scalar device OIDs and the ifTable OIDs all group
	// into the same index bucket (index=""), causing nondeterministic entity
	// count depending on which mapping entry wins the bucket.
	build := func() ObjectIDValueMap {
		oids := fixtureCisco3850TwoMemberStack()
		// Patch sysName and sysObjectID (from the base fixture) to use
		// IdentifierSize=1 so they group under index "0" (device bucket).
		for k, v := range oids {
			switch k {
			case ".1.3.6.1.2.1.1.5.0", ".1.3.6.1.2.1.1.2.0":
				v.IdentifierSize = 1
				oids[k] = v
			}
		}
		// Minimal ifTable: ifIndex 10101 = Gi1/0/1, 10201 = Gi2/0/1.
		// IdentifierSize=1 groups each by trailing ifIndex.
		oids[".1.3.6.1.2.1.2.2.1.2.10101"] = Value{Value: "GigabitEthernet1/0/1", IdentifierSize: 1}
		oids[".1.3.6.1.2.1.2.2.1.3.10101"] = Value{Value: "6", IdentifierSize: 1}
		oids[".1.3.6.1.2.1.2.2.1.2.10201"] = Value{Value: "GigabitEthernet2/0/1", IdentifierSize: 1}
		oids[".1.3.6.1.2.1.2.2.1.3.10201"] = Value{Value: "6", IdentifierSize: 1}
		return oids
	}

	run := func() []diode.Entity {
		cfg := newTestMappingConfig(t, logger)
		mapper := NewObjectIDMapper(cfg, logger, &config.Defaults{}, "10.0.0.1")
		oids := build()
		ents := mapper.MapObjectIDsToEntity(oids)
		ifIdx := mapper.InterfacesByIfIndex()
		return TranslateAsStack(ents, oids, ifIdx, &config.Defaults{}, logger)
	}

	a := run()
	b := run()

	assert.Equal(t, len(a), len(b), "entity count must be deterministic")
	for i := range a {
		if i >= len(b) {
			break
		}
		assert.IsType(t, a[i], b[i], "entity %d type mismatch", i)
	}
	marshalOpts := proto.MarshalOptions{Deterministic: true}
	for i := range a {
		if i >= len(b) {
			break
		}
		ab, err := marshalOpts.Marshal(a[i].ConvertToProtoMessage())
		require.NoError(t, err, "marshal a[%d]", i)
		bb, err := marshalOpts.Marshal(b[i].ConvertToProtoMessage())
		require.NoError(t, err, "marshal b[%d]", i)
		assert.Equal(t, ab, bb,
			"entity %d proto bytes differ — nondeterminism somewhere in MapObjectIDsToEntity or TranslateAsStack", i)
	}
}
