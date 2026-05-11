package mapping

import (
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/assert"
)

func TestPruneNestedRefs_MultiDevice_InterfaceRoutesToOwningMember(t *testing.T) {
	dtype := &diode.DeviceType{
		Model:        strPtr("WS-C3850-48P"),
		Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
	}
	master := &diode.Device{
		Name:       strPtr("master-1"),
		Serial:     strPtr("M1"),
		DeviceType: dtype,
	}
	member := &diode.Device{
		Name:       strPtr("master-1-stack-2"),
		Serial:     strPtr("M2"),
		DeviceType: dtype,
	}

	ifaceOnMaster := &diode.Interface{
		Name:   strPtr("Gi1/0/1"),
		Device: master,
	}
	ifaceOnMember := &diode.Interface{
		Name:   strPtr("Gi2/0/1"),
		Device: member,
	}

	entities := []diode.Entity{master, member, ifaceOnMaster, ifaceOnMember}
	PruneNestedRefs(entities, master)

	// Top-level Devices stay rich — pruning must not touch their own DeviceType.
	assert.NotNil(t, master.DeviceType, "rich master must remain rich (no top-level pruning)")
	assert.NotNil(t, member.DeviceType, "rich member must remain rich")
	// Interface.Device must point to a stub of its OWN owner, not master.
	assert.Equal(t, "master-1-stack-2", *ifaceOnMember.Device.Name,
		"member iface's Device ref must resolve to the member, not master")
}

// TestPruneNestedRefs_MultiDevice_ParentBridgeLagRefsResolveToOwningMember
// guards finding #13: when TranslateAsStack reroutes a member's
// Interface.Device, any nested Parent/Bridge/Lag on that interface
// must also resolve to the SAME member, not be left pointing at
// master (which would happen if ResolveSubinterfaceParents ran before
// TranslateAsStack — it does today).
func TestPruneNestedRefs_MultiDevice_ParentBridgeLagRefsResolveToOwningMember(t *testing.T) {
	dtype := &diode.DeviceType{
		Model:        strPtr("WS-C3850-48P"),
		Manufacturer: &diode.Manufacturer{Name: strPtr("Cisco")},
	}
	master := &diode.Device{Name: strPtr("master-1"), DeviceType: dtype}
	member := &diode.Device{Name: strPtr("master-1-stack-2"), DeviceType: dtype}

	parent := &diode.Interface{Name: strPtr("Gi2/0/1"), Device: member}
	sub := &diode.Interface{
		Name:   strPtr("Gi2/0/1.10"),
		Device: member,
		// Parent.Device was set by ResolveSubinterfaceParents BEFORE
		// TranslateAsStack reassigned ownership; it currently points
		// at the old (master) ref. The multi-device pruning step must
		// re-resolve by name -> member.
		Parent: &diode.Interface{Name: strPtr("Gi2/0/1"), Device: master},
	}

	entities := []diode.Entity{master, member, parent, sub}
	PruneNestedRefs(entities, master)

	assert.Equal(t, "master-1-stack-2", *sub.Parent.Device.Name,
		"subinterface's Parent.Device must resolve to the member, not stale master")
}

func TestPruneNestedRefs_MultiDevice_IPAddressRoutesViaInterface(t *testing.T) {
	master := &diode.Device{Name: strPtr("master-1")}
	member := &diode.Device{Name: strPtr("master-1-stack-2")}
	memberIface := &diode.Interface{Name: strPtr("Gi2/0/1"), Device: member}
	ip := &diode.IPAddress{
		Address:        strPtr("10.0.0.2/24"),
		AssignedObject: memberIface,
	}
	entities := []diode.Entity{master, member, memberIface, ip}
	PruneNestedRefs(entities, master)

	stub, ok := ip.AssignedObject.(*diode.Interface)
	assert.True(t, ok)
	assert.Equal(t, "master-1-stack-2", *stub.Device.Name)
}

func TestPruneNestedRefs_SingleDevice_BehaviorUnchanged(t *testing.T) {
	dev := &diode.Device{Name: strPtr("only"), Serial: strPtr("X")}
	iface := &diode.Interface{Name: strPtr("Gi0/0/0"), Device: dev}
	ip := &diode.IPAddress{Address: strPtr("10.0.0.1/24"), AssignedObject: iface}
	entities := []diode.Entity{dev, iface, ip}
	PruneNestedRefs(entities, dev)

	stub, _ := ip.AssignedObject.(*diode.Interface)
	assert.Equal(t, "only", *stub.Device.Name)
}
