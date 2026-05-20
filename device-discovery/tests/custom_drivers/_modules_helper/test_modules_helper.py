# Copyright 2026 NetBox Labs Inc
"""Unit tests for custom_napalm._modules helper."""

from __future__ import annotations

import pytest

from custom_napalm._modules import (
    MAX_BAY_DEPTH,
    ModuleBay,
    ModuleEntry,
    classify_module_type_cisco,
    classify_module_type_junos,
    to_payload,
)

# ---- to_payload validation ----------------------------------------------


def _bay(name: str, *, serial: str = "S1", mtype: str = "linecard",
         model: str = "M1", description: str = "",
         sub_bays: list[ModuleBay] | None = None) -> ModuleBay:
    """Test helper: build a ModuleBay with sane defaults."""
    return ModuleBay(
        name=name,
        position=name,
        module=ModuleEntry(
            model=model,
            serial=serial,
            type=mtype,  # type: ignore[arg-type]
            description=description,
            sub_bays=sub_bays or [],
        ),
    )


def test_to_payload_empty_input_returns_none() -> None:
    """Empty bays list → None (no payload to emit)."""
    assert to_payload([]) is None


def test_to_payload_all_invalid_bays_returns_none() -> None:
    """When every bay fails validation, the helper returns None."""
    assert to_payload([_bay("1", serial="")]) is None


def test_to_payload_drops_bay_with_empty_serial() -> None:
    """A bay whose module.serial is empty is dropped; valid bays survive."""
    payload = to_payload([_bay("1", serial=""), _bay("2", serial="OK2")])
    assert payload is not None
    names = [b["name"] for b in payload["bays"]]
    assert names == ["2"]


def test_to_payload_drops_bay_with_whitespace_only_serial() -> None:
    """A bay whose serial is whitespace-only is treated as empty."""
    payload = to_payload([_bay("1", serial="   "), _bay("2", serial="OK2")])
    assert payload is not None
    assert [b["name"] for b in payload["bays"]] == ["2"]


def test_to_payload_drops_bay_with_unknown_type() -> None:
    """A bay whose module.type is outside the enum is dropped."""
    bay = ModuleBay(
        name="1", position="1",
        module=ModuleEntry(model="M1", serial="S1", type="bogus"),  # type: ignore[arg-type]
    )
    assert to_payload([bay]) is None


def test_to_payload_drops_bay_with_no_module() -> None:
    """An empty bay (module is None) is not emitted to NetBox today."""
    bay = ModuleBay(name="1", position="1", module=None)
    assert to_payload([bay]) is None


def test_to_payload_dedupes_interfaces_by_bay() -> None:
    """Duplicate ifnames in interfaces_by_bay collapse to first occurrence."""
    payload = to_payload(
        [_bay("1")],
        interfaces_by_bay={"1": ["Te1/0/1", "Te1/0/1", "Te1/0/2", "Te1/0/1"]},
    )
    assert payload is not None
    assert payload["interfaces_by_bay"]["1"] == ["Te1/0/1", "Te1/0/2"]


def test_to_payload_rejects_4tuple_ifname_warn_and_drop() -> None:
    """
    Cisco 4-tuple ifname (VC-of-modular territory) is out of v1.

    Translator depends on the bay-name → ifname map being parseable as
    a 2- or 3-tuple Cisco/Junos ifname; 4-tuple entries are warn-dropped
    so the interface stays chassis-owned.
    """
    payload = to_payload(
        [_bay("1")],
        interfaces_by_bay={"1": ["HundredGigE1/2/0/1", "Te1/0/1"]},
    )
    assert payload is not None
    assert payload["interfaces_by_bay"]["1"] == ["Te1/0/1"]


def test_to_payload_rejects_non_string_ifname_warn_and_drop() -> None:
    """
    Non-string entries in interfaces_by_bay are warn-dropped, never raised.

    The helper's contract is to be forgiving of bad-shape input from
    drivers — None or an int slipping into an ifnames list must not
    TypeError through the regex match.
    """
    payload = to_payload(
        [_bay("1")],
        interfaces_by_bay={"1": [None, 42, "Te1/0/1"]},  # type: ignore[list-item]
    )
    assert payload is not None
    assert payload["interfaces_by_bay"]["1"] == ["Te1/0/1"]


def test_to_payload_rejects_non_iterable_ifnames_value() -> None:
    """
    A non-list value in interfaces_by_bay is warn-dropped, not iterated.

    The helper used to iterate the value blindly, which would TypeError
    on None or an int. Non-list values now degrade to an empty list for
    that bay so to_payload remains forgiving of buggy driver shapes.
    """
    payload = to_payload(
        [_bay("1")],
        interfaces_by_bay={"1": None, "2": 42, "3": ["Te3/0/1"]},  # type: ignore[dict-item]
    )
    assert payload is not None
    assert payload["interfaces_by_bay"] == {"1": [], "2": [], "3": ["Te3/0/1"]}


def test_to_payload_keeps_subbay_at_depth_2() -> None:
    """Cisco shape: chassis → linecard → transceiver. Depth 2."""
    transceiver = _bay("Te1/0/1", serial="SFP_SN", mtype="transceiver", model="SFP-10G-LR")
    linecard = _bay("1", serial="LC_SN", model="C9400-LC", sub_bays=[transceiver])
    payload = to_payload([linecard])
    assert payload is not None
    assert payload["bays"][0]["module"]["sub_bays"][0]["module"]["type"] == "transceiver"


def test_to_payload_keeps_subbay_at_depth_3() -> None:
    """Junos shape: chassis → FPC → PIC → transceiver. Depth 3."""
    transceiver = _bay("xe-0/0/0", serial="SFP_SN", mtype="transceiver")
    pic = _bay("PIC 0", serial="PIC_SN", sub_bays=[transceiver])
    fpc = _bay("FPC 0", serial="FPC_SN", sub_bays=[pic])
    payload = to_payload([fpc])
    assert payload is not None
    fpc_p = payload["bays"][0]
    pic_p = fpc_p["module"]["sub_bays"][0]
    trans_p = pic_p["module"]["sub_bays"][0]
    assert trans_p["module"]["type"] == "transceiver"


def test_to_payload_drops_subbay_deeper_than_max_depth() -> None:
    """Anything below depth 3 gets warn-dropped at the offending level."""
    too_deep = _bay("level4", serial="X")  # would be at depth 4
    leaf = _bay("level3", serial="L3", sub_bays=[too_deep])
    mid = _bay("level2", serial="L2", sub_bays=[leaf])
    top = _bay("level1", serial="L1", sub_bays=[mid])
    payload = to_payload([top])
    assert payload is not None
    top_p = payload["bays"][0]
    mid_p = top_p["module"]["sub_bays"][0]
    leaf_p = mid_p["module"]["sub_bays"][0]
    assert leaf_p["module"]["sub_bays"] == []


def test_to_payload_happy_path_serialized_shape() -> None:
    """End-to-end serialization round-trip across all fields and depth-2 nesting."""
    transceiver = _bay("Te1/0/1", serial="FNS1", mtype="transceiver", model="SFP-10G-LR",
                       description="10GBASE-LR")
    linecard = _bay("1", serial="FOC1", model="C9400-LC-48U",
                    description="48-port UPOE+ line card", sub_bays=[transceiver])
    payload = to_payload([linecard], interfaces_by_bay={"1": ["Te1/0/1", "Te1/0/2"]})
    assert payload == {
        "bays": [
            {
                "name": "1",
                "position": "1",
                "module": {
                    "model": "C9400-LC-48U",
                    "serial": "FOC1",
                    "description": "48-port UPOE+ line card",
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


def test_max_bay_depth_is_three() -> None:
    """Sanity-pin the depth-3 contract — change with intent."""
    assert MAX_BAY_DEPTH == 3


# ---- classify_module_type_cisco -----------------------------------------


@pytest.mark.parametrize(
    "pid, expected",
    [
        ("SFP-10G-LR", "transceiver"),
        ("QSFP-40G-SR4", "transceiver"),
        ("QSFP28-100G-AOC", "transceiver"),
        ("QSFP-DD-400G", "transceiver"),
        ("GLC-SX-MMD", "transceiver"),
        ("CFP-100G-LR4", "transceiver"),
        ("C9400-LC-48U", "linecard"),
        ("C9400-SUP-1XL", "linecard"),  # supervisor maps to linecard in v1
        ("PWR-C4-9000WAC", "linecard"),  # psu maps to linecard in v1
        ("", "linecard"),
        ("sfp-10g-lr", "transceiver"),  # case-insensitive
    ],
)
def test_classify_module_type_cisco(pid: str, expected: str) -> None:
    """Cisco PID → ModuleType: transceiver vs default-linecard."""
    assert classify_module_type_cisco(pid) == expected


# ---- classify_module_type_junos -----------------------------------------


@pytest.mark.parametrize(
    "description, expected",
    [
        ("10GBASE-LR SFP+ transceiver", "transceiver"),
        ("SFP-T 1000Base-T", "transceiver"),
        ("QSFP+ 40GBASE-SR4", "transceiver"),
        ("MPC4E 32x10GE + 2x100GE", "linecard"),
        ("Routing Engine RE-S-1800x4", "linecard"),
        ("Power Supply", "linecard"),
        ("", "linecard"),
    ],
)
def test_classify_module_type_junos(description: str, expected: str) -> None:
    """Junos description → ModuleType: transceiver vs default-linecard."""
    assert classify_module_type_junos(description) == expected
