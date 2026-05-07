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
