#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Stubs Unit Tests."""

from netboxlabs.diode.sdk.diode.v1 import ingester_pb2 as pb
from netboxlabs.diode.sdk.ingester import Entity

from device_discovery.stubs import (
    _current_device_from,
    _device_match_stub,
    _ip_match_stub,
)


def test_ip_match_stub_keeps_address_and_vrf_drops_assigned_object():
    rich = pb.IPAddress(address="192.0.2.1/24")
    rich.vrf.CopyFrom(pb.VRF(name="mgmt"))
    rich.assigned_object_interface.CopyFrom(pb.Interface(name="eth0"))
    rich.description = "uplink"
    rich.status = "active"

    stub = _ip_match_stub(rich)

    assert stub.address == "192.0.2.1/24"
    assert stub.HasField("vrf")
    assert stub.vrf.name == "mgmt"
    assert not stub.HasField("assigned_object_interface")
    assert stub.description == ""
    assert stub.status == ""


def test_ip_match_stub_no_vrf():
    rich = pb.IPAddress(address="10.0.0.1/24")
    stub = _ip_match_stub(rich)
    assert stub.address == "10.0.0.1/24"
    assert not stub.HasField("vrf")


def test_current_device_from_finds_device():
    device_entity = Entity(device=pb.Device(name="sw1"))
    iface_entity = Entity(interface=pb.Interface(name="eth0"))
    result = _current_device_from([iface_entity, device_entity])
    assert result is not None
    assert result.name == "sw1"


def test_current_device_from_no_device_returns_none():
    iface_entity = Entity(interface=pb.Interface(name="eth0"))
    assert _current_device_from([iface_entity]) is None


def test_current_device_from_empty_returns_none():
    assert _current_device_from([]) is None


def test_device_match_stub_keeps_required_fields_drops_rest():
    rich = pb.Device(
        name="sw1",
        serial="FCW1234X5YZ",
        status="active",
        description="ignore me",
        comments="and me",
    )
    rich.site.CopyFrom(pb.Site(name="dc1"))
    rich.tenant.CopyFrom(pb.Tenant(name="acme"))
    rich.role.CopyFrom(pb.DeviceRole(name="access-switch"))
    rich.device_type.CopyFrom(pb.DeviceType(model="Catalyst 9300"))
    rich.platform.CopyFrom(pb.Platform(name="ios-xe"))
    rich.primary_ip4.CopyFrom(pb.IPAddress(address="192.0.2.10/24"))
    rich.primary_ip4.assigned_object_interface.CopyFrom(pb.Interface(name="eth0"))
    rich.primary_ip6.CopyFrom(pb.IPAddress(address="2001:db8::1/64"))
    rich.asset_tag = "ASSET-001"

    stub = _device_match_stub(rich)

    # Matcher / required fields kept.
    assert stub.name == "sw1"
    assert stub.HasField("site") and stub.site.name == "dc1"
    assert stub.HasField("tenant") and stub.tenant.name == "acme"
    assert stub.HasField("role") and stub.role.name == "access-switch"
    assert stub.HasField("device_type") and stub.device_type.model == "Catalyst 9300"
    assert stub.asset_tag == "ASSET-001"

    # PrimaryIp4/6 stubbed (no assigned_object_interface) — cycle break.
    assert stub.HasField("primary_ip4")
    assert stub.primary_ip4.address == "192.0.2.10/24"
    assert not stub.primary_ip4.HasField("assigned_object_interface")
    assert stub.HasField("primary_ip6")
    assert stub.primary_ip6.address == "2001:db8::1/64"

    # Non-matcher / non-required fields cleared.
    assert not stub.HasField("platform")
    assert stub.serial == ""
    assert stub.status == ""
    assert stub.description == ""
    assert stub.comments == ""


def test_device_match_stub_minimal_rich():
    rich = pb.Device(name="sw1")
    rich.site.CopyFrom(pb.Site(name="dc1"))
    stub = _device_match_stub(rich)
    assert stub.name == "sw1"
    assert stub.HasField("site")
    assert not stub.HasField("primary_ip4")
    assert not stub.HasField("primary_ip6")
    assert stub.asset_tag == ""


def test_device_match_stub_no_asset_tag():
    rich = pb.Device(name="sw1")
    stub = _device_match_stub(rich)
    assert stub.asset_tag == ""
