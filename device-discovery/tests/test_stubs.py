#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Stubs Unit Tests."""

from netboxlabs.diode.sdk.diode.v1 import ingester_pb2 as pb
from netboxlabs.diode.sdk.ingester import Entity

from device_discovery.stubs import (
    _current_device_from,
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
