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
