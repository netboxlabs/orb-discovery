# Copyright 2026 NetBox Labs Inc
"""
IOS NAPALM driver subclass adding ``get_interfaces_vlans()``.

Parses ``show interfaces switchport`` via ntc-templates, normalizes each
row into a :class:`custom_napalm._vlan.SwitchportInfo`, and delegates to
the generic classifier. The classifier handles voice promotion, DTP
fallback, wildcard signaling, and clamping — none of that is duplicated here.
"""

import logging
import re

from napalm.base.helpers import canonical_interface_name
from napalm.ios.ios import IOSDriver as NapalmIOSDriver
from ntc_templates.parse import parse_output

from custom_napalm._chassis import ChassisMember, normalize_role, to_payload
from custom_napalm._modules import (
    MemberModules as _MemberModules,
)
from custom_napalm._modules import (
    ModuleBay as _ModuleBay,
)
from custom_napalm._modules import (
    ModuleEntry as _ModuleEntry,
)
from custom_napalm._modules import (
    classify_module_type_cisco,
)
from custom_napalm._modules import (
    to_payload as _modules_to_payload,
)
from custom_napalm._vlan import SwitchportInfo, classify_switchport, parse_vlan_range_string

logger = logging.getLogger(__name__)


# Cisco IOS / IOS-XE Catalyst multigig short-form override.
#
# netutils.constants.BASE_INTERFACES (the table backing
# napalm.base.helpers.canonical_interface_name) maps "Fi" to
# "FiftyGigabitEthernet". On the IOS Catalyst platforms this driver targets,
# "Fi" is the short form of FiveGigabitEthernet (5GBASE-T multigig). Without
# this override, `show interfaces switchport` rows for 5G ports canonicalize
# to "FiftyGigabitEthernet*/..." and fail to match the long-form
# "FiveGigabitEthernet*/..." names emitted by NAPALM's get_interfaces(), so
# the translator silently drops VLAN data for every 5G port.
_IOS_ADDL_NAME_MAP = {
    "Fi": "FiveGigabitEthernet",
    "FI": "FiveGigabitEthernet",
    "fi": "FiveGigabitEthernet",
}


def _maybe_int(v: object) -> int | None:
    """
    Convert a string/int to int, returning None on failure.

    Explicitly rejects ``bool`` (which is a subclass of ``int`` in Python,
    so ``int(True) == 1``). VLAN-ID fields populated from buggy upstream
    parsers must NOT silently turn ``True``/``False`` into VID 1/0 — the
    classifier's bool-rejection in ``_vlan.coerce_vid`` only fires if the
    bool reaches it un-coerced.
    """
    if isinstance(v, bool):
        return None
    try:
        return int(v)  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return None


def _normalize_admin_mode(raw: str) -> str | None:
    """Map a raw IOS admin_mode string to 'access', 'trunk', 'dynamic', or None."""
    if "access" in raw:
        return "access"
    if "trunk" in raw:
        return "trunk"
    if "dynamic" in raw:
        return "dynamic"
    return None


def _normalize_oper_mode(raw: str) -> str | None:
    """Map a raw IOS operational mode string to 'access', 'trunk', 'routed', or None."""
    if "access" in raw:
        return "access"
    if "trunk" in raw:
        return "trunk"
    if "routed" in raw:
        return "routed"
    return None


def _parse_trunking_vlans(raw_trunking: list) -> list[int] | str | None:
    """
    Convert an ntc-templates trunking_vlans token list to allowed_vlans.

    Returns ``"all"`` for wildcard inputs, a list of int VIDs for explicit
    ranges, or None when the token list is empty. Logs a WARNING when the
    caller supplied non-empty, non-NONE tokens that parsed to nothing — this
    prevents silent trunk-all promotion on malformed CLI output.
    """
    if not raw_trunking:
        return None
    spec = ",".join(t for t in raw_trunking if t)
    vids, is_wildcard = parse_vlan_range_string(spec)
    if is_wildcard:
        return "all"
    has_input = any((tok or "").strip() for tok in raw_trunking)
    has_none = any((tok or "").strip().upper() == "NONE" for tok in raw_trunking)
    if has_input and not has_none and not vids:
        logger.warning(
            "trunking_vlans=%r could not be parsed; "
            "treating as plain trunk with no tagged VLANs",
            raw_trunking,
        )
    return vids


def _ios_row_to_switchport_info(row: dict) -> SwitchportInfo:
    """
    Build a SwitchportInfo from one ntc-templates ``show interfaces switchport`` row.

    Field mapping rules:
      - ``switchport`` "Disabled" / falsy   → enabled=False (routed downstream)
      - ``admin_mode`` is the trusted intent signal; "dynamic auto" /
        "dynamic desirable" map to ``"dynamic"`` so the classifier falls
        back to oper_mode.
      - ``mode`` (operational) is normalized similarly.
      - ``trunking_vlans`` is a list of tokens — flattened via comma-join
        and handed to ``parse_vlan_range_string``; literal "ALL" / "NONE"
        tokens are detected by the helper.
    """
    switchport = (row.get("switchport") or "").lower()
    if "disabled" in switchport:
        return SwitchportInfo(
            enabled=False, admin_mode=None, oper_mode=None,
            access_vlan=None, native_vlan=None, allowed_vlans=None,
        )

    admin = _normalize_admin_mode((row.get("admin_mode") or "").lower())
    oper = _normalize_oper_mode((row.get("mode") or "").lower())
    allowed = _parse_trunking_vlans(row.get("trunking_vlans") or [])

    return SwitchportInfo(
        enabled=True,
        admin_mode=admin,  # type: ignore[arg-type]
        oper_mode=oper,    # type: ignore[arg-type]
        access_vlan=_maybe_int(row.get("access_vlan", "")),
        native_vlan=_maybe_int(row.get("native_vlan", "")),
        allowed_vlans=allowed,
        voice_vlan=_maybe_int(row.get("voice_vlan", "")),
    )


class IOSDriver(NapalmIOSDriver):
    """Cisco IOS NAPALM driver with VLAN-interface association support."""

    def get_interfaces_vlans(self) -> dict[str, dict]:
        """
        Return per-interface VLAN config.

        Output shape per interface::

            {"mode": "access"|"trunk"|"trunk-all"|"routed",
             "tagged": list[int], "untagged": int | None}

        Parses ``show interfaces switchport`` via ntc-templates. Always
        canonicalizes interface names (``Gi1/0/1`` → ``GigabitEthernet1/0/1``)
        because NAPALM IOS's default ``get_interfaces()`` returns long-form
        from ``show interfaces``, while ``show interfaces switchport`` emits
        short-form. Without canonicalization the translator's exact-name
        match silently misses associations.
        """
        output = self._send_command("show interfaces switchport")
        if not output:
            return {}

        try:
            rows = parse_output(
                platform="cisco_ios",
                command="show interfaces switchport",
                data=output,
            )
        except Exception:
            logger.debug(
                "ntc-templates failed to parse 'show interfaces switchport'",
                exc_info=True,
            )
            return {}

        result: dict[str, dict] = {}
        for row in rows or []:
            ifname = row.get("interface")
            if not ifname:
                continue
            ifname = canonical_interface_name(ifname, addl_name_map=_IOS_ADDL_NAME_MAP)
            info = _ios_row_to_switchport_info(row)
            result[ifname] = classify_switchport(info)
        return result

    def get_chassis_members(self) -> dict | None:
        """
        Return stack-member info for Cisco StackWise (Catalyst 3850/9300/2960X/...).

        Standalone IOS returns None (no stack rows, or a single populated slot).
        Stack of N populated members returns the payload shape consumed by
        device_discovery.translate's VC emission path.
        """
        return _ios_get_chassis_members_impl(self)

    def get_modules(self) -> dict | None:
        """
        Return Module / ModuleBay inventory for a standalone modular chassis.

        Parses ``show inventory`` for ``Slot N <role>`` rows (linecards and
        supervisors on Catalyst 9400 / 9600 and similar IOS-XE chassis) and
        interface-name rows (transceivers). Attaches transceiver entries as
        ``sub_bays`` of the parent linecard. ``show ip interface brief``
        provides the per-slot interface enumeration used to build
        ``interfaces_by_bay``.

        Returns ``None`` for non-modular chassis (no ``Slot N`` rows) and
        when ``show inventory`` fails to parse. Virtual-chassis-of-modular
        composition is deferred — ``policy.runner._collect_modules``
        gates this call behind a ``data["chassis_members"]`` check and
        does not invoke it on VC members, so this method does not need
        to detect that case itself.
        """
        return _ios_get_modules_impl(self)


# Two NAME formats are seen in the wild for stack members:
#   "Switch 1"   — Catalyst 3850/9300/2960X StackWise (most common)
#   "1"          — Some IOS / IOS-XE versions emit just the slot number
# Anything else (e.g. "Chassis", "GigabitEthernet1/0/1") is ignored — the caller
# treats an empty index as "no per-member inventory available" and the affected
# members are dropped by to_payload().
_INVENTORY_NAME_RE = re.compile(r"^(?:Switch\s+)?(\d+)$", re.IGNORECASE)


def _index_inventory_by_switch(rows: list[dict]) -> tuple[dict[int, str], dict[int, str]]:
    """
    Return (serial_by_switch_id, model_by_switch_id) parsed from `show inventory`.

    Matches NAME values of the form 'Switch N' or bare 'N' (case-insensitive). On
    standalone IOS the NAME is 'Chassis' and yields empty dicts — caller treats
    that as "no per-member inventory available".
    """
    serial_by_id: dict[int, str] = {}
    model_by_id: dict[int, str] = {}
    for row in rows or []:
        m = _INVENTORY_NAME_RE.match((row.get("name") or "").strip())
        if not m:
            continue
        sid = int(m.group(1))
        sn = (row.get("sn") or "").strip()
        pid = (row.get("pid") or "").strip()
        if sn:
            serial_by_id[sid] = sn
        if pid:
            model_by_id[sid] = pid
    return serial_by_id, model_by_id


def _ios_get_chassis_members_impl(driver) -> dict | None:
    """Implementation of IOSDriver.get_chassis_members (factored for testability)."""
    try:
        detail_out = driver.device.send_command("show switch detail")
        detail_rows = parse_output(
            platform="cisco_ios",
            command="show switch detail",
            data=detail_out or "",
        )
    except Exception as e:
        logger.warning("ios.get_chassis_members: show switch detail failed: %s", e)
        return None

    if not detail_rows:
        return None

    try:
        inv_out = driver.device.send_command("show inventory")
        inv_rows = parse_output(
            platform="cisco_ios",
            command="show inventory",
            data=inv_out or "",
        )
    except Exception as e:
        logger.warning("ios.get_chassis_members: show inventory failed: %s", e)
        inv_rows = []

    serial_by_id, model_by_id = _index_inventory_by_switch(inv_rows or [])

    members: list[ChassisMember] = []
    for row in detail_rows:
        sid = _maybe_int(row.get("switch"))
        if sid is None:
            continue
        members.append(
            ChassisMember(
                id=sid,
                serial=serial_by_id.get(sid, ""),
                model=model_by_id.get(sid),
                role=normalize_role(row.get("role")),
                priority=_maybe_int(row.get("priority")),
                mac=row.get("mac_address") or row.get("mac") or None,
                state=row.get("state") or None,
            )
        )

    return to_payload(members, domain=None)


# ---- module inventory ----------------------------------------------------
#
# `show inventory` row patterns that drive emission:
#
#   "Slot 1 Linecard"      — physical line card on a Catalyst 9400/9600 modular
#   "Slot 1 Supervisor"    — supervisor module (treated as its own type)
#   "Slot 3 - Supervisor"  — hyphenated variant seen on some IOS-XE versions
#
# Plus transceiver rows whose NAME is an interface short / long form, e.g.
# "Te1/0/1" or "TenGigabitEthernet1/0/1". A 4-tuple ifname like
# "Te1/2/0/1" belongs to VC-of-modular composition, which is deferred —
# it never matches the regex below and the _modules helper drops the
# entry at the payload boundary if it slips through some other path.
#
# IMPORTANT: only ``Slot N`` is matched here. The earlier ``module|Module``
# alternation also caught ``"module 0"`` rows emitted by non-modular
# IOS-XE platforms (ISR/ASR route processors), which would falsely
# materialize a phantom slot-0 ModuleBay on every such device.
#
# The negative lookahead ``(?!\s*/)`` after the slot number rejects
# sub-slot inventory rows like ``"Slot 0/0"`` or ``"Slot 2/1/0"``
# that some platforms emit for SPA/controller positions. Without it,
# those rows would silently partial-match as a top-level slot and
# materialize phantom linecards.
_INVENTORY_SLOT_RE = re.compile(
    r"^Slot\s+(\d+)(?!\s*/)(?:\s*[-:]?\s*(\w+))?",
    re.IGNORECASE,
)

# Virtual-chassis-of-modular slot row, Cat 9400/9500 StackWise Virtual.
# `Switch 1 Slot 2 Linecard` or `Switch 2 Slot 3 - Supervisor`. The same
# (?!\s*/) lookahead rejects sub-slot rows like `Switch 1 Slot 2/0` that
# encode controller positions rather than top-level chassis bays. The
# standalone _INVENTORY_SLOT_RE above is anchored with `^Slot` and so does
# NOT match these VC-prefixed rows — a row matches at most one regex.
_INVENTORY_VC_SLOT_RE = re.compile(
    r"^Switch\s+(\d+)\s+Slot\s+(\d+)(?!\s*/)(?:\s*[-:]?\s*(\w+))?",
    re.IGNORECASE,
)

# Virtual-chassis-of-modular FRU uplink, Cat 9300 stack with network module.
# `Switch 1 FRU Uplink Module 1`. Distinct from VC_SLOT — 9300 doesn't have
# Slot N entries; it exposes its single swappable network module via this
# NAME pattern instead.
_INVENTORY_VC_FRU_RE = re.compile(
    r"^Switch\s+(\d+)\s+FRU\s+Uplink\s+Module\s+(\d+)",
    re.IGNORECASE,
)

# Inventory row NAME that looks like an interface (transceiver row).
# Accepts 2-tuple (e.g. Te1/1), 3-tuple (Te2/0/1 — standalone modular), and
# 4-tuple (HundredGigE1/2/0/1 — Cat 9400/9500 SVL) Cisco ifnames.
_INVENTORY_IFNAME_RE = re.compile(r"^[A-Za-z]+\d+(?:/\d+){1,3}$")
_INTERFACE_SLOT_RE = re.compile(r"^[A-Za-z]+(\d+)/\d+")


# A bare or prefixed "Switch N ..." inventory row marks a member chassis
# in a virtual-chassis stack. Standalone modular chassis (e.g. Cat 9404R)
# emit a single "Chassis" row instead and never have a Switch prefix.
_SWITCH_PREFIX_RE = re.compile(r"^Switch\s+\d+\b", re.IGNORECASE)


def _has_switch_rows(inv_rows: list[dict]) -> bool:
    """Detect VC mode: any inventory row whose NAME starts with ``Switch N``."""
    for row in inv_rows or []:
        if _SWITCH_PREFIX_RE.match((row.get("name") or "").strip()):
            return True
    return False


def _interface_member_id(ifname: str) -> int | None:
    """Extract the leading integer from a Cisco ifname (member id in VC context)."""
    m = re.match(r"^[A-Za-z]+(\d+)/", ifname)
    return int(m.group(1)) if m else None


def _interface_slot(ifname: str, *, depth: int = 1) -> str | None:
    """
    Extract a slot id from a Cisco ifname.

    ``depth=1`` returns the leading integer — that's the slot id on a
    standalone modular chassis (e.g. ``TenGigabitEthernet2/0/1`` → ``2``).
    ``depth=2`` returns the second integer — that's the slot id on a VC
    member chassis (e.g. ``TenGigabitEthernet1/1/1`` → ``1`` for a 9300
    stack FRU uplink, ``HundredGigE1/2/0/1`` → ``2`` for 9400 SVL).
    """
    if depth == 1:
        m = _INTERFACE_SLOT_RE.match(ifname)
        return m.group(1) if m else None
    m = re.match(r"^[A-Za-z]+\d+/(\d+)", ifname)
    return m.group(1) if m else None


def _classify_slot_module(pid: str, role_hint: str) -> str:
    """
    Pick a ModuleType for a Slot N inventory row.

    Trusts the NAME's role hint first ("Supervisor" / "Sup..." → supervisor),
    then falls back to PID-based classification. Anything not classifiable
    as a transceiver from the PID — supervisor, linecard, fabric, route
    processor — is treated as ``linecard`` for v1.
    """
    role_word = (role_hint or "").lower()
    if role_word.startswith("sup"):
        return "supervisor"
    pid_type = classify_module_type_cisco(pid)
    # A "Slot N" row that PID-classifies as transceiver is almost certainly
    # an inventory mislabel — keep it a linecard rather than risk dropping
    # the bay in linecards mode.
    return "linecard" if pid_type == "transceiver" else pid_type


def _parse_inventory_rows(
    rows: list[dict],
    vc_mode: bool,
) -> tuple[
    dict[int | None, dict[str, _ModuleBay]],
    dict[int | None, dict[str, _ModuleEntry]],
]:
    """
    Split ``show inventory`` rows into per-member slot bays and transceivers.

    Returns ``(bays_by_member_then_slot, transceivers_by_member_then_ifname)``.
    In standalone mode (``vc_mode=False``) both outer dicts have a single
    ``None`` key. In VC mode the outer key is the member id captured from
    ``Switch N ...`` prefixes on the inventory row.
    """
    bays_by_member: dict[int | None, dict[str, _ModuleBay]] = {}
    trans_by_member: dict[int | None, dict[str, _ModuleEntry]] = {}
    for row in rows or []:
        name = (row.get("name") or "").strip()
        pid = (row.get("pid") or "").strip()
        sn = (row.get("sn") or "").strip()
        descr = (row.get("descr") or "").strip()
        if not (pid and sn):
            continue

        if vc_mode:
            vc_slot = _INVENTORY_VC_SLOT_RE.match(name)
            if vc_slot:
                member_id = int(vc_slot.group(1))
                slot = vc_slot.group(2)
                mtype = _classify_slot_module(pid, vc_slot.group(3) or "")
                bays_by_member.setdefault(member_id, {})[slot] = _ModuleBay(
                    name=slot, position=slot,
                    module=_ModuleEntry(
                        model=pid, serial=sn, type=mtype, description=descr,
                    ),
                )
                continue
            vc_fru = _INVENTORY_VC_FRU_RE.match(name)
            if vc_fru:
                member_id = int(vc_fru.group(1))
                slot = vc_fru.group(2)
                # FRU uplink modules have no role hint in NAME, so trust the
                # PID classifier (linecard for non-transceiver Cisco PIDs).
                bays_by_member.setdefault(member_id, {})[slot] = _ModuleBay(
                    name=slot, position=slot,
                    module=_ModuleEntry(
                        model=pid, serial=sn,
                        type=classify_module_type_cisco(pid),
                        description=descr,
                    ),
                )
                continue

        slot_match = _INVENTORY_SLOT_RE.match(name)
        if slot_match:
            # Standalone "Slot N" row — bucketed under member_id=None.
            slot = slot_match.group(1)
            mtype = _classify_slot_module(pid, slot_match.group(2) or "")
            bays_by_member.setdefault(None, {})[slot] = _ModuleBay(
                name=slot, position=slot,
                module=_ModuleEntry(
                    model=pid, serial=sn, type=mtype, description=descr,
                ),
            )
            continue

        if _INVENTORY_IFNAME_RE.match(name):
            # Transceiver row keyed by ifname. In VC mode the leading
            # integer of the ifname is the member id; in standalone there
            # is no member dimension and the transceiver lives in the
            # same None bucket as its parent.
            member_for_transceiver = _interface_member_id(name) if vc_mode else None
            trans_by_member.setdefault(member_for_transceiver, {})[name] = _ModuleEntry(
                model=pid, serial=sn,
                type=classify_module_type_cisco(pid),
                description=descr,
            )
    return bays_by_member, trans_by_member


def _collect_interfaces_by_member_and_slot(
    driver,
    bays_by_member: dict[int | None, dict[str, _ModuleBay]],
    vc_mode: bool,
) -> dict[int | None, dict[str, list[str]]]:
    """
    Bin canonicalized ifnames from ``show ip interface brief`` by member then slot.

    Member key matches the inventory parser: in VC mode the leading integer of
    the ifname is the member id; in standalone mode every ifname binds to the
    single ``None`` member bucket. Falls back to a skeleton populated with
    empty lists when the brief command fails — bays/modules still emit, just
    without per-interface routing for that cycle.
    """
    out: dict[int | None, dict[str, list[str]]] = {
        m: {slot: [] for slot in bays_by_member[m]} for m in bays_by_member
    }
    try:
        raw = driver.device.send_command("show ip interface brief")
        rows = parse_output(
            platform="cisco_ios",
            command="show ip interface brief",
            data=raw or "",
        )
    except Exception:
        logger.debug("ios.get_modules: show ip interface brief failed", exc_info=True)
        return out
    for row in rows or []:
        raw_if = (row.get("interface") or row.get("intf") or "").strip()
        if not raw_if:
            continue
        ifname = canonical_interface_name(raw_if, addl_name_map=_IOS_ADDL_NAME_MAP)
        if vc_mode:
            member_id = _interface_member_id(ifname)
            slot = _interface_slot(ifname, depth=2)
        else:
            member_id = None
            slot = _interface_slot(ifname, depth=1)
        if member_id not in out or slot is None or slot not in out[member_id]:
            continue
        out[member_id][slot].append(ifname)
    return out


def _attach_transceivers(
    bays_by_member: dict[int | None, dict[str, _ModuleBay]],
    transceivers_by_member: dict[int | None, dict[str, _ModuleEntry]],
    interfaces_by_member_and_slot: dict[int | None, dict[str, list[str]]],
    vc_mode: bool,
) -> None:
    """
    Attach each transceiver entry as a sub-bay of its member's parent slot.

    Canonicalizes the transceiver row's ifname so the sub-bay name aligns
    with the long-form name from ``get_interfaces()``. Also self-routes
    the transceiver's ifname into its own sub-bay key so the translator's
    deepest-wins logic assigns the transceiver as the interface's module
    in full mode.
    """
    for member_id, transceivers in transceivers_by_member.items():
        if member_id not in bays_by_member:
            continue
        for raw_ifname, transceiver in transceivers.items():
            canonical = canonical_interface_name(
                raw_ifname, addl_name_map=_IOS_ADDL_NAME_MAP,
            )
            slot = _interface_slot(canonical, depth=2 if vc_mode else 1)
            if slot is None or slot not in bays_by_member[member_id]:
                continue
            parent_bay = bays_by_member[member_id][slot]
            parent_bay.module.sub_bays.append(
                _ModuleBay(name=canonical, position=canonical, module=transceiver),
            )
            interfaces_by_member_and_slot.setdefault(member_id, {}).setdefault(
                canonical, [],
            ).append(canonical)


def _ios_get_modules_impl(driver) -> dict | None:
    """Implementation of IOSDriver.get_modules (factored for testability)."""
    try:
        inv_out = driver.device.send_command("show inventory")
        inv_rows = parse_output(
            platform="cisco_ios",
            command="show inventory",
            data=inv_out or "",
        )
    except Exception as e:
        logger.warning("ios.get_modules: show inventory failed: %s", e)
        return None
    if not inv_rows:
        return None

    vc_mode = _has_switch_rows(inv_rows)
    bays_by_member, transceivers_by_member = _parse_inventory_rows(inv_rows, vc_mode)
    if not bays_by_member:
        return None

    interfaces_by_member_and_slot = _collect_interfaces_by_member_and_slot(
        driver, bays_by_member, vc_mode,
    )
    _attach_transceivers(
        bays_by_member, transceivers_by_member, interfaces_by_member_and_slot, vc_mode,
    )

    return _modules_to_payload({
        member_id: _MemberModules(
            bays=list(bays.values()),
            interfaces_by_bay=interfaces_by_member_and_slot.get(member_id, {}),
        )
        for member_id, bays in bays_by_member.items()
    })
