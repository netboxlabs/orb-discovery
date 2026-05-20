#!/usr/bin/env python
# Copyright 2026 NetBox Labs Inc
"""Unit tests for device_discovery.translate_modules."""

from __future__ import annotations

import logging

from netboxlabs.diode.sdk.ingester import Device, DeviceType, Entity, Manufacturer

from device_discovery.policy.models import Options
from device_discovery.translate_modules import emit_modules_if_requested


def _make_device(name: str = "test-router", vendor: str = "Cisco") -> Device:
    """Test helper: build a minimal Device with manufacturer set on its DeviceType."""
    return Device(
        name=name,
        device_type=DeviceType(model="C9606R", manufacturer=Manufacturer(name=vendor)),
    )


def _linecard_payload() -> dict:
    """One-bay one-linecard happy-path payload."""
    return {
        "bays": [
            {
                "name": "1",
                "position": "1",
                "module": {
                    "model": "C9400-LC-48U",
                    "serial": "FOC1",
                    "description": "48-port UPOE+",
                    "type": "linecard",
                    "sub_bays": [],
                },
            },
        ],
        "interfaces_by_bay": {"1": ["Te1/0/1", "Te1/0/2"]},
    }


def _linecard_with_transceiver_payload() -> dict:
    """Linecard with one nested transceiver — exercises depth-2 emission."""
    return {
        "bays": [
            {
                "name": "1",
                "position": "1",
                "module": {
                    "model": "C9400-LC-48U",
                    "serial": "FOC1",
                    "description": "48-port UPOE+",
                    "type": "linecard",
                    "sub_bays": [
                        {
                            "name": "Te1/0/1",
                            "position": "Te1/0/1",
                            "module": {
                                "model": "SFP-10G-LR",
                                "serial": "FNS1",
                                "description": "10GBASE-LR",
                                "type": "transceiver",
                                "sub_bays": [],
                            },
                        },
                    ],
                },
            },
        ],
        "interfaces_by_bay": {"1": ["Te1/0/1", "Te1/0/2"]},
    }


# ---- mode gating ---------------------------------------------------------


def test_off_mode_returns_empty_map_and_does_not_touch_entities() -> None:
    """discover_modules == 'off' → no payload read, entities unchanged."""
    entities: list = []
    data = {"modules": _linecard_payload()}
    iface_module_map = emit_modules_if_requested(
        data, Options(discover_modules="off"), _make_device(), entities,
    )
    assert iface_module_map == {}
    assert entities == []


def test_missing_modules_key_returns_empty_map() -> None:
    """When data['modules'] is absent, fall through cleanly."""
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {}, Options(discover_modules="linecards"), _make_device(), entities,
    )
    assert iface_module_map == {}
    assert entities == []


def test_none_modules_payload_returns_empty_map() -> None:
    """data['modules'] == None (driver get_modules() returned None) → no emission."""
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": None}, Options(discover_modules="linecards"), _make_device(), entities,
    )
    assert iface_module_map == {}
    assert entities == []


def test_empty_bays_returns_empty_map() -> None:
    """Payload with bays=[] → no emission."""
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": {"bays": [], "interfaces_by_bay": {}}},
        Options(discover_modules="linecards"),
        _make_device(),
        entities,
    )
    assert iface_module_map == {}
    assert entities == []


# ---- linecards mode ------------------------------------------------------


def test_linecards_mode_emits_top_level_bay_and_module() -> None:
    """Linecards mode emits one ModuleBay + one Module per linecard."""
    entities: list = []
    data = {"modules": _linecard_payload()}
    iface_module_map = emit_modules_if_requested(
        data, Options(discover_modules="linecards"), _make_device(), entities,
    )
    bays = [e for e in entities if e.HasField("module_bay")]
    modules = [e for e in entities if e.HasField("module")]
    assert len(bays) == 1
    assert len(modules) == 1
    assert bays[0].module_bay.name == "1"
    assert modules[0].module.serial == "FOC1"
    assert modules[0].module.module_type.model == "C9400-LC-48U"
    assert modules[0].module.module_type.manufacturer.name == "Cisco"
    # Interface routing: both ifnames in interfaces_by_bay map to the linecard.
    assert set(iface_module_map.keys()) == {"Te1/0/1", "Te1/0/2"}


def test_linecards_mode_drops_transceiver_subbays() -> None:
    """Linecards mode emits the parent linecard but NOT the nested transceiver."""
    entities: list = []
    data = {"modules": _linecard_with_transceiver_payload()}
    iface_module_map = emit_modules_if_requested(
        data, Options(discover_modules="linecards"), _make_device(), entities,
    )
    modules = [e.module for e in entities if e.HasField("module")]
    assert len(modules) == 1  # only the linecard, transceiver dropped
    assert modules[0].module_type.model == "C9400-LC-48U"
    # Interfaces still get attached — to the parent linecard, not transceiver.
    assert iface_module_map["Te1/0/1"].module_type.model == "C9400-LC-48U"


def test_linecards_mode_skips_top_level_transceiver_bay() -> None:
    """A top-level transceiver bay (rare, but possible) is dropped in linecards mode."""
    payload = {
        "bays": [
            {
                "name": "1",
                "position": "1",
                "module": {
                    "model": "SFP-10G-LR",
                    "serial": "FNS1",
                    "description": "",
                    "type": "transceiver",
                    "sub_bays": [],
                },
            },
        ],
        "interfaces_by_bay": {},
    }
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": payload}, Options(discover_modules="linecards"),
        _make_device(), entities,
    )
    assert entities == []
    assert iface_module_map == {}


# ---- full mode -----------------------------------------------------------


def test_full_mode_emits_transceiver_subbay_with_module_parent() -> None:
    """
    Full mode emits the linecard, the sub-bay, AND the transceiver Module.

    The transceiver's ModuleBay must reference the parent Module (not the
    Device) — that's how NetBox represents nested-bay hierarchy.
    """
    entities: list = []
    data = {"modules": _linecard_with_transceiver_payload()}
    emit_modules_if_requested(
        data, Options(discover_modules="full"), _make_device(), entities,
    )
    bays = [e.module_bay for e in entities if e.HasField("module_bay")]
    modules = [e.module for e in entities if e.HasField("module")]
    assert len(bays) == 2  # top-level linecard bay + sub transceiver bay
    assert len(modules) == 2  # linecard + transceiver
    # Top-level bay: device-rooted.
    top_bay = bays[0]
    assert top_bay.name == "1"
    assert top_bay.device.name == "test-router"
    # Sub-bay: module-rooted.
    sub_bay = bays[1]
    assert sub_bay.name == "Te1/0/1"
    assert sub_bay.module.serial == "FOC1"  # parent is the linecard
    # Transceiver module references the sub-bay.
    transceiver = modules[1]
    assert transceiver.module_type.model == "SFP-10G-LR"
    assert transceiver.serial == "FNS1"


# ---- VC short-circuit ---------------------------------------------------


def test_vc_short_circuit_logs_warning_and_emits_nothing(caplog) -> None:
    """When data has BOTH chassis_members AND modules, emit only a WARNING."""
    entities: list = []
    data = {
        "modules": _linecard_payload(),
        "chassis_members": {"members": [{"id": 1}, {"id": 2}], "domain": None},
    }
    with caplog.at_level(logging.WARNING, logger="device_discovery.translate_modules"):
        iface_module_map = emit_modules_if_requested(
            data, Options(discover_modules="linecards"), _make_device(), entities,
        )
    assert iface_module_map == {}
    assert entities == []
    assert any(
        "deferred for virtual chassis" in r.message for r in caplog.records
    )


# ---- malformed-payload fallthrough --------------------------------------


def test_malformed_bay_logged_and_other_bays_continue(caplog) -> None:
    """A bay that raises during emission is logged and other bays still emit."""
    payload = {
        "bays": [
            {  # Missing "module" key → will raise inside _emit_bay_recursive.
                "name": "broken",
                "position": "broken",
            },
            {  # Valid sibling — must still emit.
                "name": "1",
                "position": "1",
                "module": {
                    "model": "C9400-LC-48U",
                    "serial": "FOC1",
                    "description": "",
                    "type": "linecard",
                    "sub_bays": [],
                },
            },
        ],
        "interfaces_by_bay": {},
    }
    entities: list = []
    with caplog.at_level(logging.WARNING, logger="device_discovery.translate_modules"):
        emit_modules_if_requested(
            {"modules": payload}, Options(discover_modules="linecards"),
            _make_device(), entities,
        )
    modules = [e for e in entities if e.HasField("module")]
    assert len(modules) == 1
    assert modules[0].module.serial == "FOC1"
    assert any("malformed module payload" in r.message for r in caplog.records)


def test_non_dict_payload_returns_empty_map() -> None:
    """data['modules'] of a wrong type (e.g. a list) is treated as no payload."""
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": ["not", "a", "dict"]},
        Options(discover_modules="linecards"),
        _make_device(),
        entities,
    )
    assert iface_module_map == {}
    assert entities == []


# ---- iface_module_map deepest-wins --------------------------------------


def test_full_mode_iface_map_uses_deepest_bay_when_payload_specifies() -> None:
    """When the driver maps an ifname to a sub-bay key, that sub-bay wins."""
    payload = _linecard_with_transceiver_payload()
    # Driver populates the sub-bay key as well as the parent — deepest wins.
    payload["interfaces_by_bay"]["Te1/0/1"] = ["Te1/0/1"]
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": payload}, Options(discover_modules="full"),
        _make_device(), entities,
    )
    # The transceiver module wins for Te1/0/1.
    assert iface_module_map["Te1/0/1"].module_type.model == "SFP-10G-LR"
    # Te1/0/2 wasn't in the sub-bay map; it stays on the parent linecard.
    assert iface_module_map["Te1/0/2"].module_type.model == "C9400-LC-48U"
