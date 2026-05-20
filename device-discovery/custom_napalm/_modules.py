# Copyright 2026 NetBox Labs Inc
"""
Generic, vendor-neutral helper for modular-chassis Module / ModuleBay discovery.

Each driver's optional ``get_modules()`` builds a list of
:class:`ModuleBay` (vendor-neutral intermediates), drops bays whose
serial / type fails validation, and wraps the result with
:func:`to_payload`. The translate layer
(``device_discovery.translate_modules``) consumes the payload and
emits NetBox ``dcim.modulebay`` + ``dcim.module`` entities (plus
recursive sub-bays in ``full`` mode).

Output payload shape (consumed by translate)::

    {
        "bays": [
            {
                "name": "1", "position": "1",
                "module": {
                    "model": "...", "serial": "...",
                    "description": "...", "type": "linecard",
                    "sub_bays": [...],            # recursive, depth-3 cap
                },
            },
            ...
        ],
        "interfaces_by_bay": {"1": ["Te1/0/1", ...]},
    }

Returning ``None`` (no valid bays after validation) signals the
translate layer to keep the existing single-Device path. The helper
never raises on data shape — bad rows are warn-dropped, matching the
forgiving behavior of ``_chassis.py``.
"""

from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import Literal

logger = logging.getLogger(__name__)


ModuleType = Literal["linecard", "supervisor", "fan", "psu", "transceiver"]
_VALID_TYPES: frozenset[str] = frozenset(
    {"linecard", "supervisor", "fan", "psu", "transceiver"}
)

#: Hard depth cap for recursive sub-bay nesting. Cisco modular chassis are
#: depth 2 (chassis -> linecard -> transceiver); Junos is depth 3 (chassis
#: -> FPC -> PIC -> transceiver). Anything deeper is rejected with a
#: warning.
MAX_BAY_DEPTH = 3


@dataclass
class ModuleEntry:
    """One physical module installed in a bay."""

    model: str
    serial: str
    type: ModuleType
    description: str = ""
    sub_bays: list[ModuleBay] = field(default_factory=list)


@dataclass
class ModuleBay:
    """
    One slot in a chassis (or in a parent module) that holds a Module.

    Empty bays (``module is None``) are NOT emitted to NetBox today —
    operators wanting empty-bay layouts pre-populate via the
    devicetype-library templates. Empty entries here are skipped during
    payload construction.
    """

    name: str
    position: str
    module: ModuleEntry | None


# ---- v1 minimal classifiers ----------------------------------------------
#
# Distinguish only "transceiver" vs everything-else. The ``linecards`` mode
# filter relies on this enum to drop transceivers from emission; finer
# classification (supervisor / psu / fan) is a follow-up.

_CISCO_TRANSCEIVER_PREFIXES = (
    "SFP-",
    "SFP+",
    "QSFP-",
    "QSFP+",
    "QSFP28",
    "QSFP-DD",
    "GLC-",
    "X2-",
    "CFP-",
    "CFP2-",
    "CVR-",
)


def classify_module_type_cisco(pid: str) -> ModuleType:
    """
    Map a Cisco PID/model string to a ``ModuleType`` enum value.

    v1: distinguish transceiver; default everything else to ``"linecard"``.
    """
    if not pid:
        return "linecard"
    upper = pid.upper()
    if upper.startswith(_CISCO_TRANSCEIVER_PREFIXES):
        return "transceiver"
    return "linecard"


def classify_module_type_junos(description: str) -> ModuleType:
    """
    Map a Junos description string to a ``ModuleType`` enum value.

    v1: distinguish transceiver; default everything else to ``"linecard"``.
    """
    if not description:
        return "linecard"
    lower = description.lower()
    if "transceiver" in lower or "sfp" in lower or "qsfp" in lower:
        return "transceiver"
    return "linecard"


# ---- payload assembly ----------------------------------------------------


def to_payload(
    bays: list[ModuleBay],
    interfaces_by_bay: dict[str, list[str]] | None = None,
) -> dict | None:
    """
    Validate ``bays`` + ``interfaces_by_bay``, return the wire payload.

    Returns ``None`` when no bay survives validation — drivers pass this
    straight through to signal "fall back to single-Device path." The
    helper never raises on data shape; every drop reason is logged at
    WARNING. Mirrors ``_chassis.to_payload`` forgiveness.
    """
    # NOTE: implementation lands in the next commit so the contract diff is
    # reviewable independently of the validation logic.
    raise NotImplementedError("validation lands in Task 3")
