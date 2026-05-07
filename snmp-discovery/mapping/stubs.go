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
