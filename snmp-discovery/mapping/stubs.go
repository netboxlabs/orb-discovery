// Package mapping helpers that produce matcher-only stubs of diode
// entities. These are used to shrink nested references in the wire
// payload: only fields the diode-netbox-plugin needs to *match* the
// existing object are kept; full data still rides on the top-level
// entity. See docs/superpowers/specs/2026-05-07-snmp-payload-stub-nested-refs-design.md.
package mapping

import "github.com/netboxlabs/diode-sdk-go/diode"

// newIPMatchStub returns an IPAddress carrying only the matcher fields
// (Address, Vrf). AssignedObject is intentionally nil — that is what
// breaks the IP→Interface→Device cycle when this stub is embedded in a
// Device stub's PrimaryIp4/PrimaryIp6.
func newIPMatchStub(ip *diode.IPAddress) *diode.IPAddress {
	if ip == nil {
		return nil
	}
	return &diode.IPAddress{
		Address: ip.Address,
		Vrf:     ip.Vrf,
	}
}

// newMACMatchStub returns a MACAddress carrying only MacAddress. Used
// inside Interface stubs to preserve the unique_primary_mac_address
// matcher precedence on dcim.interface.
func newMACMatchStub(mac *diode.MACAddress) *diode.MACAddress {
	if mac == nil {
		return nil
	}
	return &diode.MACAddress{
		MacAddress: mac.MacAddress,
	}
}

// newDeviceStub returns a Device populated with matcher-only fields.
// Site and Tenant are pointer-shared from the source — already minimal
// in snmp-discovery, no transitive bloat. PrimaryIp4 and PrimaryIp6 go
// through newIPMatchStub so AssignedObject is cleared, breaking any
// cycle into the rich top-level Device.
//
// INVARIANT: the fields here must be a superset of every dcim.device
// matcher field that snmp-discovery currently populates on the rich
// Device. As of the spec date, snmp-discovery does NOT populate
// AssetTag, OobIp, Rack, Position, Face, VirtualChassis, or
// VcPosition. If a new mapper starts setting any of those, this stub
// must grow to include them — otherwise the rich entity and the stub
// will resolve via different matcher precedence paths and may match
// different NetBox devices. See
// docs/superpowers/specs/2026-05-07-snmp-payload-stub-nested-refs-design.md.
func newDeviceStub(d *diode.Device) *diode.Device {
	if d == nil {
		return nil
	}
	return &diode.Device{
		Name:       d.Name,
		Site:       d.Site,
		Tenant:     d.Tenant,
		PrimaryIp4: newIPMatchStub(d.PrimaryIp4),
		PrimaryIp6: newIPMatchStub(d.PrimaryIp6),
	}
}

// newInterfaceStub returns an Interface populated with matcher-only
// fields: Name, Device (caller-supplied stub), and PrimaryMacAddress
// (run through newMACMatchStub). Used wherever an Interface appears as
// a nested reference: Parent, Bridge, Lag, IPAddress.AssignedObject,
// MACAddress.AssignedObject. Including PrimaryMacAddress preserves the
// dcim.interface unique_primary_mac_address matcher precedence so the
// stub resolves to the same interface as the rich top-level entity.
func newInterfaceStub(iface *diode.Interface, deviceStub *diode.Device) *diode.Interface {
	if iface == nil {
		return nil
	}
	return &diode.Interface{
		Name:              iface.Name,
		Device:            deviceStub,
		PrimaryMacAddress: newMACMatchStub(iface.PrimaryMacAddress),
	}
}

// CurrentDeviceFrom returns the first *diode.Device in the slice, or
// nil. In normal operation the snmp-discovery mapper emits exactly one
// top-level Device per walk (the registry's CurrentDeviceIndex), so
// this is a cheap O(N) lookup and avoids threading the pointer through
// the runner separately.
func CurrentDeviceFrom(entities []diode.Entity) *diode.Device {
	for _, e := range entities {
		if d, ok := e.(*diode.Device); ok {
			return d
		}
	}
	return nil
}

// PruneNestedRefs walks entities once and replaces nested Device and
// Interface references with matcher-only stubs. The top-level rich
// Device entity (the same pointer as currentDevice) is left unchanged
// — only nested references *to* it on other entities are rewritten.
//
// Call from the runner AFTER annotateDeviceWithSourceMatch and
// annotateEntitiesWithRunID, BEFORE Ingest. Running before annotation
// would either (a) cause annotators to skip the rich Device because
// they would only see stubs, or (b) bloat every stub with run_id
// metadata, defeating the savings.
//
// No-op if currentDevice is nil or entities is empty.
func PruneNestedRefs(entities []diode.Entity, currentDevice *diode.Device) {
	if currentDevice == nil || len(entities) == 0 {
		return
	}
	deviceStub := newDeviceStub(currentDevice)

	for _, entity := range entities {
		switch e := entity.(type) {
		case *diode.Device:
			// Top-level Device is the rich one; leave it alone.
			if e == currentDevice {
				continue
			}
		case *diode.Interface:
			e.Device = deviceStub
			if e.Parent != nil {
				e.Parent = newInterfaceStub(e.Parent, deviceStub)
			}
			if e.Bridge != nil {
				e.Bridge = newInterfaceStub(e.Bridge, deviceStub)
			}
			if e.Lag != nil {
				e.Lag = newInterfaceStub(e.Lag, deviceStub)
			}
		case *diode.IPAddress:
			if iface, ok := e.AssignedObject.(*diode.Interface); ok && iface != nil {
				e.AssignedObject = newInterfaceStub(iface, deviceStub)
			}
		case *diode.MACAddress:
			if iface, ok := e.AssignedObject.(*diode.Interface); ok && iface != nil {
				e.AssignedObject = newInterfaceStub(iface, deviceStub)
			}
		case *diode.Module:
			e.Device = deviceStub
		}
	}
}
