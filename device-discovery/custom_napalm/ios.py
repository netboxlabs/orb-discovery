# Copyright 2026 NetBox Labs Inc
"""IOS NAPALM driver subclass adding get_interfaces_vlans()."""

import logging

from napalm.ios.ios import IOSDriver as NapalmIOSDriver
from ntc_templates.parse import parse_output

logger = logging.getLogger(__name__)


def _expand_ios_vlan_list(items: list[str]) -> list[int]:
    """
    Expand ntc-templates trunking_vlans list into int VIDs.

    Each item is a digit ("10"), a range ("20-30"), "ALL", or "NONE".
    Returns ``[]`` for any expansion that covers the entire 1-4094 dot1q
    range or for the literal sentinels -- avoids fanning out to thousands
    of stub VLANs downstream when a switch advertises a wide-open trunk.
    Out-of-range VIDs (outside 1..4094) are dropped.
    """
    if not items:
        return []
    out: list[int] = []
    for item in items:
        token = (item or "").strip().upper()
        if not token or token in {"ALL", "NONE"}:
            continue
        if "-" in token:
            lo_s, hi_s = token.split("-", 1)
            try:
                lo, hi = int(lo_s), int(hi_s)
            except ValueError:
                continue
            out.extend(range(lo, hi + 1))
        else:
            try:
                out.append(int(token))
            except ValueError:
                continue
    out = [v for v in out if 1 <= v <= 4094]
    if out and min(out) <= 1 and max(out) >= 4094 and len(set(out)) >= 4094:
        return []
    return out


def _classify_ios_switchport_row(row: dict) -> dict:
    """
    Map one ntc-templates parsed row to NAPALM #919 jobec shape.

    Returns ``{"mode": "access"|"trunk"|"routed", "tagged": list[int],
    "untagged": int | None}``.

    Voice-VLAN promotion: an access port with a configured voice VLAN is
    reported as ``mode=trunk, untagged=access_vid, tagged=[voice_vid]``
    because NetBox's ``access`` mode disallows tagged VLANs.

    DTP-negotiated ports (admin_mode = "dynamic auto" / "dynamic desirable")
    fall back to operational ``mode`` for classification, since the admin
    string itself contains neither "access" nor "trunk".
    """
    switchport = (row.get("switchport") or "").lower()
    admin_mode = (row.get("admin_mode") or "").lower()
    oper_mode = (row.get("mode") or "").lower()

    if "disabled" in switchport:
        return {"mode": "routed", "tagged": [], "untagged": None}

    # Pick the most useful mode signal. Admin tells us the user's intent;
    # operational tells us what the port actually negotiated to. For DTP
    # modes ("dynamic auto", "dynamic desirable") admin_mode is unhelpful,
    # so fall back to oper_mode. If both are empty, treat as routed.
    effective = admin_mode if (
        "access" in admin_mode or "trunk" in admin_mode
    ) else oper_mode

    if not effective or "routed" in effective:
        return {"mode": "routed", "tagged": [], "untagged": None}

    def _to_int(value: str) -> int | None:
        try:
            return int(value)
        except (ValueError, TypeError):
            return None

    access_vid = _to_int(row.get("access_vlan", ""))
    native_vid = _to_int(row.get("native_vlan", ""))
    voice_vid = _to_int(row.get("voice_vlan", ""))

    if "access" in effective:
        if voice_vid:
            return {
                "mode": "trunk",
                "tagged": [voice_vid],
                "untagged": access_vid,
            }
        return {
            "mode": "access",
            "tagged": [],
            "untagged": access_vid,
        }
    if "trunk" in effective:
        tagged = _expand_ios_vlan_list(row.get("trunking_vlans") or [])
        if native_vid is not None:
            tagged = [v for v in tagged if v != native_vid]
        return {
            "mode": "trunk",
            "tagged": tagged,
            "untagged": native_vid,
        }
    return {"mode": "routed", "tagged": [], "untagged": None}


class IOSDriver(NapalmIOSDriver):
    """Cisco IOS NAPALM driver with VLAN-interface association support."""

    def get_interfaces_vlans(self) -> dict[str, dict]:
        """
        Return per-interface VLAN config (NAPALM #919 jobec shape).

        Parses ``show interfaces switchport`` via ntc-templates.
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
            result[ifname] = _classify_ios_switchport_row(row)
        return result
