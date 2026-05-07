#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Matcher-only stubs of diode entities used to shrink nested
references in the wire payload sent to the Diode SDK ingester.

These helpers run at the client boundary, after run_id annotation
and before message-size estimation, so the rich entity graph is
preserved through translation/annotation and only the wire payload
is trimmed.
"""

from netboxlabs.diode.sdk.diode.v1 import ingester_pb2 as pb
from netboxlabs.diode.sdk.ingester import Entity


def _ip_match_stub(ip: pb.IPAddress) -> pb.IPAddress:
    """Return an IPAddress carrying only matcher fields.

    `assigned_object_interface` is intentionally unset — that breaks
    the IP→Interface→Device cycle when this stub is embedded in a
    Device stub's primary_ip4/primary_ip6 fields.
    """
    stub = pb.IPAddress(address=ip.address)
    if ip.HasField("vrf"):
        stub.vrf.CopyFrom(ip.vrf)
    return stub


def _current_device_from(entities: list[Entity]) -> pb.Device | None:
    """Return the rich top-level Device proto, or None.

    device-discovery emits exactly one top-level Device per call to
    translate_data. This O(N) lookup avoids threading the device
    pointer through to the client separately.
    """
    for e in entities:
        if e.HasField("device"):
            return e.device
    return None


def _device_match_stub(d: pb.Device) -> pb.Device:
    """Return a Device carrying matcher-only fields plus the
    validation-required fields NetBox checks during create.

    INVARIANT: this set must be a superset of (a) every dcim.device
    matcher field device-discovery currently populates, and (b) every
    field NetBox treats as required for create. As of the spec date,
    device-discovery does NOT populate oob_ip, position, face,
    virtual_chassis, or vc_position. If a new translator path starts
    setting any of those, this stub must grow to include them —
    otherwise the rich entity and the stub will resolve via different
    matcher precedence paths or fail validation on the first cycle.

    `asset_tag` is the highest-precedence matcher and is populated
    when the policy sets defaults.device.asset_tag — kept on the stub
    so the rich entity and stub never resolve via different matchers.
    """
    stub = pb.Device(name=d.name)
    if d.HasField("site"):
        stub.site.CopyFrom(d.site)
    if d.HasField("tenant"):
        stub.tenant.CopyFrom(d.tenant)
    if d.HasField("device_type"):
        stub.device_type.CopyFrom(d.device_type)
    if d.HasField("role"):
        stub.role.CopyFrom(d.role)
    if d.HasField("primary_ip4"):
        stub.primary_ip4.CopyFrom(_ip_match_stub(d.primary_ip4))
    if d.HasField("primary_ip6"):
        stub.primary_ip6.CopyFrom(_ip_match_stub(d.primary_ip6))
    if d.asset_tag:
        stub.asset_tag = d.asset_tag
    return stub
