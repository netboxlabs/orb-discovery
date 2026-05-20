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
import re
from dataclasses import dataclass, field
from typing import Literal

logger = logging.getLogger(__name__)

# A 4-tuple Cisco-style ifname like "HundredGigE1/2/0/1" — used to detect
# and reject VC-of-modular routing keys at the helper boundary so the
# translator never sees them. Standalone-modular chassis use 2- or 3-tuple
# names; the 4-tuple form encodes member id in the first slot and belongs
# to the deferred stacked-modular composition work.
_CISCO_4TUPLE_RE = re.compile(r"^[A-Za-z]+\d+/\d+/\d+/\d+$")


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
    validated = [b for b in (_validate_bay(bay, depth=1) for bay in bays) if b]
    if not validated:
        return None
    cleaned_ifaces = _validate_interfaces_by_bay(interfaces_by_bay or {})
    return {"bays": validated, "interfaces_by_bay": cleaned_ifaces}


def _validate_bay(bay: ModuleBay, *, depth: int) -> dict | None:
    """Recursively validate a single ``ModuleBay``. Returns serialized dict or None."""
    if depth > MAX_BAY_DEPTH:
        logger.warning(
            "module bay dropped: nested deeper than depth %d",
            MAX_BAY_DEPTH,
            extra={"bay_name": bay.name, "depth": depth},
        )
        return None
    if bay.module is None:
        logger.warning("module bay dropped: empty bay", extra={"bay_name": bay.name})
        return None
    serial = (bay.module.serial or "").strip()
    if not serial:
        logger.warning("module bay dropped: empty serial", extra={"bay_name": bay.name})
        return None
    if bay.module.type not in _VALID_TYPES:
        logger.warning(
            "module bay dropped: invalid type %r",
            bay.module.type,
            extra={"bay_name": bay.name},
        )
        return None
    sub_bays: list[dict] = []
    for sub in bay.module.sub_bays:
        validated_sub = _validate_bay(sub, depth=depth + 1)
        if validated_sub is not None:
            sub_bays.append(validated_sub)
    return {
        "name": bay.name,
        "position": bay.position,
        "module": {
            "model": bay.module.model,
            "serial": serial,
            "description": bay.module.description,
            "type": bay.module.type,
            "sub_bays": sub_bays,
        },
    }


def _validate_interfaces_by_bay(
    interfaces_by_bay: dict[str, list[str]],
) -> dict[str, list[str]]:
    """
    Dedupe and reject 4-tuple Cisco ifnames (VC-of-modular territory).

    Non-string entries (a buggy driver returning ``None`` or an int) are
    warn-dropped before the regex match, so this helper upholds the
    ``to_payload`` docstring promise of never raising on data shape.
    """
    cleaned: dict[str, list[str]] = {}
    for bay_name, ifnames in interfaces_by_bay.items():
        seen: set[str] = set()
        keep: list[str] = []
        for name in ifnames:
            if not isinstance(name, str):
                logger.warning(
                    "interfaces_by_bay entry dropped: non-string ifname",
                    extra={"bay_name": bay_name, "ifname": repr(name)[:80]},
                )
                continue
            if name in seen:
                continue
            if _CISCO_4TUPLE_RE.match(name):
                logger.warning(
                    "interfaces_by_bay entry dropped: Cisco 4-tuple ifname "
                    "(VC-of-modular is deferred to a follow-up)",
                    extra={"bay_name": bay_name, "ifname": name},
                )
                continue
            seen.add(name)
            keep.append(name)
        cleaned[bay_name] = keep
    return cleaned
