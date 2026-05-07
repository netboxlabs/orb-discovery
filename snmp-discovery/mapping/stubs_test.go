package mapping

import (
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
)

func strPtr(s string) *string { return &s }

func TestNewIPMatchStub_Nil(t *testing.T) {
	assert.Nil(t, newIPMatchStub(nil))
}

func TestNewIPMatchStub_KeepsAddressAndVrfDropsRest(t *testing.T) {
	addr := "192.0.2.1/24"
	vrf := &diode.VRF{Name: strPtr("mgmt")}
	rich := &diode.IPAddress{
		Address:        &addr,
		Vrf:            vrf,
		AssignedObject: &diode.Interface{Name: strPtr("eth0")},
		Description:    strPtr("uplink"),
		Status:         strPtr("active"),
	}

	stub := newIPMatchStub(rich)

	assert.NotNil(t, stub)
	assert.NotSame(t, rich, stub, "must return a new pointer, not the input")
	assert.Equal(t, &addr, stub.Address)
	assert.Same(t, vrf, stub.Vrf, "Vrf is pointer-shared (already minimal)")
	assert.Nil(t, stub.AssignedObject, "AssignedObject must be cleared for cycle safety")
	assert.Nil(t, stub.Description)
	assert.Nil(t, stub.Status)
}

func TestNewMACMatchStub_Nil(t *testing.T) {
	assert.Nil(t, newMACMatchStub(nil))
}

func TestNewMACMatchStub_KeepsMacAddressOnly(t *testing.T) {
	mac := "aa:bb:cc:dd:ee:ff"
	rich := &diode.MACAddress{
		MacAddress:     &mac,
		AssignedObject: &diode.Interface{Name: strPtr("eth0")},
		Description:    strPtr("primary"),
	}

	stub := newMACMatchStub(rich)

	assert.NotNil(t, stub)
	assert.NotSame(t, rich, stub)
	assert.Equal(t, &mac, stub.MacAddress)
	assert.Nil(t, stub.AssignedObject)
	assert.Nil(t, stub.Description)
}

func TestNewDeviceStub_Nil(t *testing.T) {
	assert.Nil(t, newDeviceStub(nil))
}

func TestNewDeviceStub_KeepsMatcherFieldsDropsRest(t *testing.T) {
	site := &diode.Site{Name: strPtr("dc1")}
	tenant := &diode.Tenant{Name: strPtr("acme")}
	v4 := "192.0.2.10/24"
	v6 := "2001:db8::1/64"
	rich := &diode.Device{
		Name:        strPtr("sw1"),
		Site:        site,
		Tenant:      tenant,
		PrimaryIp4:  &diode.IPAddress{Address: &v4, AssignedObject: &diode.Interface{Name: strPtr("eth0")}},
		PrimaryIp6:  &diode.IPAddress{Address: &v6},
		Role:        &diode.DeviceRole{Name: strPtr("access-switch")},
		DeviceType:  &diode.DeviceType{Model: strPtr("Catalyst 9300")},
		Platform:    &diode.Platform{Name: strPtr("ios-xe")},
		Serial:      strPtr("FCW1234X5YZ"),
		AssetTag:    strPtr("ASSET-001"),
		Status:      strPtr("active"),
		Description: strPtr("ignore me"),
	}

	stub := newDeviceStub(rich)

	assert.NotNil(t, stub)
	assert.NotSame(t, rich, stub)
	assert.Equal(t, strPtr("sw1"), stub.Name)
	assert.Same(t, site, stub.Site)
	assert.Same(t, tenant, stub.Tenant)

	// PrimaryIp4 stubbed (no AssignedObject) — cycle break.
	assert.NotNil(t, stub.PrimaryIp4)
	assert.NotSame(t, rich.PrimaryIp4, stub.PrimaryIp4)
	assert.Equal(t, &v4, stub.PrimaryIp4.Address)
	assert.Nil(t, stub.PrimaryIp4.AssignedObject)

	assert.NotNil(t, stub.PrimaryIp6)
	assert.Equal(t, &v6, stub.PrimaryIp6.Address)

	// All non-matcher fields cleared.
	assert.Nil(t, stub.Role)
	assert.Nil(t, stub.DeviceType)
	assert.Nil(t, stub.Platform)
	assert.Nil(t, stub.Serial)
	assert.Nil(t, stub.AssetTag)
	assert.Nil(t, stub.Status)
	assert.Nil(t, stub.Description)
}

func TestNewDeviceStub_NilPrimaryIPs(t *testing.T) {
	rich := &diode.Device{Name: strPtr("sw1"), Site: &diode.Site{Name: strPtr("dc1")}}
	stub := newDeviceStub(rich)
	assert.NotNil(t, stub)
	assert.Nil(t, stub.PrimaryIp4)
	assert.Nil(t, stub.PrimaryIp6)
}
