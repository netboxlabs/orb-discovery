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
        composition is deferred — the translate layer short-circuits with
        a WARNING when ``chassis_members`` is also populated, so this
        method does not need to detect that case itself.
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
# Two `show inventory` row patterns drive emission:
#
#   "Slot 1 Linecard"      — physical line card on a Catalyst 9400/9600 modular
#   "Slot 1 Supervisor"    — supervisor module (treated as its own type)
#   "module 1"             — alternate phrasing seen on some IOS-XE versions
#
# Plus transceiver rows whose NAME is an interface short / long form, e.g.
# "Te1/0/1" or "TenGigabitEthernet1/0/1". A 4-tuple ifname like
# "Te1/2/0/1" belongs to VC-of-modular composition, which is deferred —
# it never matches the regex below and the _modules helper drops the
# entry at the payload boundary if it slips through some other path.
_INVENTORY_SLOT_RE = re.compile(
    r"^(?:Slot|module|Module)\s+(\d+)(?:\s+(\w+))?",
    re.IGNORECASE,
)
_INVENTORY_IFNAME_RE = re.compile(r"^[A-Za-z]+\d+(?:/\d+){1,2}$")
_INTERFACE_SLOT_RE = re.compile(r"^[A-Za-z]+(\d+)/\d+")


def _interface_slot(ifname: str) -> str | None:
    """Extract the leading slot number from a canonical/short Cisco ifname."""
    m = _INTERFACE_SLOT_RE.match(ifname)
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


def _parse_inventory_rows(rows: list[dict]) -> tuple[dict[str, _ModuleBay], dict[str, _ModuleEntry]]:
    """
    Split ``show inventory`` rows into slot bays and transceivers-by-ifname.

    Returns ``(bays_by_slot, transceivers_by_ifname)`` where ``bays_by_slot``
    maps the slot string (``"1"``) to the ModuleBay for that physical slot,
    and ``transceivers_by_ifname`` maps the raw inventory NAME (often the
    short form) to a ModuleEntry. The caller canonicalizes the ifname
    before attaching the transceiver as a sub-bay.
    """
    bays_by_slot: dict[str, _ModuleBay] = {}
    transceivers_by_ifname: dict[str, _ModuleEntry] = {}
    for row in rows or []:
        name = (row.get("name") or "").strip()
        pid = (row.get("pid") or "").strip()
        sn = (row.get("sn") or "").strip()
        descr = (row.get("descr") or "").strip()
        if not (pid and sn):
            continue
        slot_match = _INVENTORY_SLOT_RE.match(name)
        if slot_match:
            slot = slot_match.group(1)
            mtype = _classify_slot_module(pid, slot_match.group(2) or "")
            bays_by_slot[slot] = _ModuleBay(
                name=slot, position=slot,
                module=_ModuleEntry(model=pid, serial=sn, type=mtype, description=descr),
            )
            continue
        if _INVENTORY_IFNAME_RE.match(name):
            transceivers_by_ifname[name] = _ModuleEntry(
                model=pid, serial=sn,
                type=classify_module_type_cisco(pid),
                description=descr,
            )
    return bays_by_slot, transceivers_by_ifname


def _collect_interfaces_by_slot(driver, slots: set[str]) -> dict[str, list[str]]:
    """
    Return ``{slot: [ifname, ...]}`` via ``show ip interface brief``.

    Falls back to an empty mapping when the command fails or the template
    parse errors — operators still get module / module-bay emission, just
    without per-interface module attachment for that cycle.
    """
    out_map: dict[str, list[str]] = {slot: [] for slot in slots}
    try:
        out = driver.device.send_command("show ip interface brief")
        rows = parse_output(
            platform="cisco_ios",
            command="show ip interface brief",
            data=out or "",
        )
    except Exception:
        logger.debug("ios.get_modules: show ip interface brief failed", exc_info=True)
        return out_map
    for row in rows or []:
        ifname = (row.get("interface") or row.get("intf") or "").strip()
        if not ifname:
            continue
        slot = _interface_slot(ifname)
        if slot and slot in out_map:
            out_map[slot].append(ifname)
    return out_map


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

    bays_by_slot, transceivers_by_ifname = _parse_inventory_rows(inv_rows)
    if not bays_by_slot:
        return None

    interfaces_by_bay = _collect_interfaces_by_slot(driver, set(bays_by_slot))

    # Attach transceivers as sub-bays of their slot's linecard. Canonicalize
    # the inventory's raw NAME (often the short form "Te1/0/1") so the
    # sub-bay name matches the long-form ifname produced by
    # get_interfaces() and consumed by the translator's deepest-wins
    # routing.
    for raw_ifname, transceiver in transceivers_by_ifname.items():
        canonical = canonical_interface_name(
            raw_ifname, addl_name_map=_IOS_ADDL_NAME_MAP,
        )
        slot = _interface_slot(canonical)
        if not slot or slot not in bays_by_slot:
            continue
        parent_bay = bays_by_slot[slot]
        # parent_bay.module is always populated by _parse_inventory_rows
        # (a row without serial+pid never produces a ModuleBay), so the
        # downstream attribute access here cannot AttributeError.
        parent_bay.module.sub_bays.append(
            _ModuleBay(name=canonical, position=canonical, module=transceiver),
        )
        # Sub-bay self-routes the transceiver's ifname so full-mode
        # deepest-wins assigns the transceiver as the interface's module.
        interfaces_by_bay.setdefault(canonical, []).append(canonical)

    return _modules_to_payload(
        list(bays_by_slot.values()),
        interfaces_by_bay=interfaces_by_bay,
    )
