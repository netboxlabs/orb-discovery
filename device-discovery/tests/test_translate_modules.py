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

    NetBox requires ``device`` on every ModuleBay and Module, including
    nested sub-bays — the Diode reconciler rejects bays/modules emitted
    without it (``Field device is required``). The sub-bay also sets
    ``module`` to the parent linecard so NetBox places it under the
    right slot.
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
    # Top-level bay: device-rooted, no module parent.
    top_bay = bays[0]
    assert top_bay.name == "1"
    assert top_bay.device.name == "test-router"
    assert not top_bay.HasField("module")
    # Sub-bay: BOTH device (chassis scope) AND module (parent linecard).
    sub_bay = bays[1]
    assert sub_bay.name == "Te1/0/1"
    assert sub_bay.device.name == "test-router"
    assert sub_bay.module.serial == "FOC1"
    # Transceiver module also carries the chassis device + its sub-bay.
    transceiver = modules[1]
    assert transceiver.module_type.model == "SFP-10G-LR"
    assert transceiver.device.name == "test-router"
    assert transceiver.module_bay.name == "Te1/0/1"
    assert transceiver.serial == "FNS1"


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


def test_malformed_sub_bay_does_not_drop_parent_bay(caplog) -> None:
    """
    One bad sub-bay must not take down the parent linecard's emission.

    Earlier the recursive call lived inside the top-level bay's
    try/except, so any exception raised inside _emit_bay_recursive
    while processing a sub-bay would skip the whole parent bay (and
    its already-emitted Module). The fix wraps each sub-bay in its
    own guard.
    """
    payload = {
        "bays": [
            {
                "name": "1", "position": "1",
                "module": {
                    "model": "C9400-LC-48U", "serial": "FOC1", "description": "",
                    "type": "linecard",
                    "sub_bays": [
                        # Missing "module" key → AttributeError inside recursion.
                        {"name": "BROKEN", "position": "BROKEN"},
                        {
                            "name": "Te1/0/2", "position": "Te1/0/2",
                            "module": {
                                "model": "SFP-10G-LR", "serial": "FNS2",
                                "description": "", "type": "transceiver",
                                "sub_bays": [],
                            },
                        },
                    ],
                },
            },
        ],
        "interfaces_by_bay": {},
    }
    entities: list = []
    with caplog.at_level(logging.WARNING, logger="device_discovery.translate_modules"):
        emit_modules_if_requested(
            {"modules": payload}, Options(discover_modules="full"),
            _make_device(), entities,
        )
    modules = [e.module for e in entities if e.HasField("module")]
    # Linecard + the sibling transceiver survive; broken sub-bay is dropped.
    serials = sorted(m.serial for m in modules)
    assert serials == ["FNS2", "FOC1"]
    assert any("sub-bay" in r.message for r in caplog.records)


def test_non_dict_sub_bay_logged_and_skipped(caplog) -> None:
    """A non-dict element in sub_bays is logged and skipped without raising."""
    payload = {
        "bays": [
            {
                "name": "1", "position": "1",
                "module": {
                    "model": "C9400-LC-48U", "serial": "FOC1", "description": "",
                    "type": "linecard",
                    "sub_bays": ["garbage-not-a-dict"],
                },
            },
        ],
        "interfaces_by_bay": {},
    }
    entities: list = []
    with caplog.at_level(logging.WARNING, logger="device_discovery.translate_modules"):
        emit_modules_if_requested(
            {"modules": payload}, Options(discover_modules="full"),
            _make_device(), entities,
        )
    modules = [e.module for e in entities if e.HasField("module")]
    assert len(modules) == 1  # parent linecard still emits
    assert any("not a dict" in r.message for r in caplog.records)


def test_malformed_interfaces_by_bay_does_not_block_emission(caplog) -> None:
    """
    A non-dict ``interfaces_by_bay`` must not crash bay/module emission.

    The translator normalizes it to ``{}`` so per-interface routing is
    simply empty; the rest of the payload still emits cleanly.
    """
    payload = {
        "bays": [
            {
                "name": "1", "position": "1",
                "module": {
                    "model": "C9400-LC-48U", "serial": "FOC1", "description": "",
                    "type": "linecard",
                    "sub_bays": [],
                },
            },
        ],
        # A driver returning None here used to AttributeError inside the loop.
        "interfaces_by_bay": None,
    }
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": payload}, Options(discover_modules="linecards"),
        _make_device(), entities,
    )
    modules = [e for e in entities if e.HasField("module")]
    assert len(modules) == 1
    assert iface_module_map == {}


def test_non_dict_bay_in_payload_logged_and_skipped(caplog) -> None:
    """A non-dict element inside payload['bays'] must not crash the loop."""
    payload = {
        "bays": [
            "garbage-not-a-dict",
            {
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
    assert any("not a dict" in r.message for r in caplog.records)


def test_linecards_mode_drops_non_transceiver_sub_bays() -> None:
    """
    Linecards mode drops ALL sub_bays regardless of type, not just transceivers.

    A nested fan / psu / supervisor sub-bay (rare in real chassis but
    representable in the payload) must also be filtered out so that
    operator dashboards show only the top-level inventory.
    """
    payload = {
        "bays": [
            {
                "name": "1", "position": "1",
                "module": {
                    "model": "C9400-LC-48U", "serial": "FOC1", "description": "",
                    "type": "linecard",
                    "sub_bays": [
                        {
                            "name": "FAN-1", "position": "FAN-1",
                            "module": {
                                "model": "C9400-FAN", "serial": "FAN_SN",
                                "description": "", "type": "fan",
                                "sub_bays": [],
                            },
                        },
                    ],
                },
            },
        ],
        "interfaces_by_bay": {},
    }
    entities: list = []
    emit_modules_if_requested(
        {"modules": payload}, Options(discover_modules="linecards"),
        _make_device(), entities,
    )
    modules = [e.module for e in entities if e.HasField("module")]
    # Only the linecard emits — the nested fan sub-bay is dropped.
    assert len(modules) == 1
    assert modules[0].module_type.model == "C9400-LC-48U"


def test_full_mode_module_reuses_device_manufacturer_reference() -> None:
    """
    Module ModuleType.manufacturer must value-equal the Device's manufacturer.

    v1 inherits the device's manufacturer for every installed Module —
    constructing a fresh Manufacturer(name=...) here would lose any extra
    fields the upstream driver populated (slug, custom_field_data, etc.).
    """
    entities: list = []
    data = {"modules": _linecard_payload()}
    device = _make_device(vendor="Cisco")
    emit_modules_if_requested(
        data, Options(discover_modules="linecards"), device, entities,
    )
    module = next(e.module for e in entities if e.HasField("module"))
    # Same name, and the manufacturer message round-trips via SerializeToString
    # to the same bytes as the device's. Protobuf messages are equal iff their
    # serialized bytes are equal, so this catches drift in any non-name field.
    assert (
        module.module_type.manufacturer.SerializeToString()
        == device.device_type.manufacturer.SerializeToString()
    )


# ---- iface_module_map deepest-wins --------------------------------------


def test_metric_counters_invoked_when_enabled(monkeypatch) -> None:
    """
    Module / bay emission each bump their counters with vendor + type attributes.

    Stubs ``get_metric`` to a recording fake so we don't depend on the
    OTel SDK being wired up in the test environment. The vc_of_modular
    drop counter is fired upstream in
    ``policy.runner._collect_modules`` and is covered there.
    """
    import device_discovery.translate_modules as tm

    calls: list[tuple[str, int, dict]] = []

    class _FakeCounter:
        def __init__(self, name):
            self.name = name

        def add(self, value, attrs):
            calls.append((self.name, value, dict(attrs)))

    counters: dict[str, _FakeCounter] = {}

    def fake_get_metric(name: str):
        counters.setdefault(name, _FakeCounter(name))
        return counters[name]
    monkeypatch.setattr(tm, "get_metric", fake_get_metric)

    entities: list = []
    data = {"modules": _linecard_with_transceiver_payload()}
    emit_modules_if_requested(
        data, Options(discover_modules="full"), _make_device(), entities,
    )
    bay_counts = [c for c in calls if c[0] == "module_bays_emitted"]
    mod_counts = [c for c in calls if c[0] == "modules_emitted"]
    assert len(bay_counts) == 2  # parent bay + transceiver sub-bay
    assert len(mod_counts) == 2  # linecard + transceiver
    # Linecard emits with type=linecard; transceiver with type=transceiver.
    assert {m[2].get("type") for m in mod_counts} == {"linecard", "transceiver"}
    assert all(c[2].get("vendor") == "Cisco" for c in mod_counts + bay_counts)


def test_metric_counters_noop_when_disabled(monkeypatch) -> None:
    """get_metric() returns None when OTel export was never configured — no AttributeError."""
    import device_discovery.translate_modules as tm
    monkeypatch.setattr(tm, "get_metric", lambda _name: None)
    entities: list = []
    iface_module_map = emit_modules_if_requested(
        {"modules": _linecard_payload()}, Options(discover_modules="linecards"),
        _make_device(), entities,
    )
    # Emission still succeeds with metrics disabled (the production default).
    assert any(e.HasField("module") for e in entities)
    assert iface_module_map  # interfaces routed


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
