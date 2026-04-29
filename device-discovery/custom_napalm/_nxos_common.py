# Copyright 2026 NetBox Labs Inc
"""
Shared NX-OS field mapper for the nxos (NX-API) and nxos_ssh subclass drivers.

Both drivers ultimately produce the same per-interface row shape — NX-API
emits it as JSON, ntc-templates emits it as a dict from CLI parsing.

Field-name reconciliation
-------------------------
NX-API JSON fields:
    interface, switchport, admin_mode, oper_mode, access_vlan, native_vlan,
    voice_vlan, trunk_vlans

ntc-templates ``cisco_nxos_show_interface_switchport`` template fields
(observed in ntc-templates 7.x as installed in this venv):
    interface, switchport, mode (= operational mode only — admin_mode is NOT
    parsed by the template), access_vlan, access_vlan_name, native_vlan,
    native_vlan_name, trunking_vlans, voice_vlan

The mapper accepts BOTH naming conventions and normalizes them. NX-OS-SSH
rows lack ``admin_mode`` because ``show interface switchport`` does not
print it on every NX-OS release; the operational ``mode`` field is the
only signal available, so we use it as both admin and oper. This is
correct for NX-OS in practice — DTP (the reason IOS distinguishes admin
vs oper) is rarely used on NX-OS, and when it IS used the operational
mode reflects the negotiated state.
"""

from __future__ import annotations

from custom_napalm._vlan import SwitchportInfo, parse_vlan_range_string


def _maybe_int(value: object) -> int | None:
    """Coerce to int or None; treats the string 'none' / empty / non-numeric as None."""
    if value in (None, "", "none", "None", "NONE"):
        return None
    try:
        return int(value)  # type: ignore[arg-type]
    except (TypeError, ValueError):
        return None


def _normalize_admin(value: str) -> str | None:
    """Normalize a raw admin-mode string to 'access', 'trunk', 'dynamic', or None."""
    raw = (value or "").lower()
    if "access" in raw:
        return "access"
    if "trunk" in raw:
        return "trunk"
    if "dynamic" in raw:
        return "dynamic"
    return None


def _normalize_oper(value: str) -> str | None:
    """Normalize a raw oper-mode string to 'access', 'trunk', 'routed', or None."""
    raw = (value or "").lower()
    if "access" in raw:
        return "access"
    if "trunk" in raw:
        return "trunk"
    if "routed" in raw:
        return "routed"
    return None


def _read_admin_mode(row: dict) -> str:
    """Return raw admin_mode string, falling back to ``mode`` (ntc-templates alias)."""
    return row.get("admin_mode") or row.get("mode") or ""


def _read_oper_mode(row: dict) -> str:
    """Return raw oper_mode string, falling back to ``mode`` (ntc-templates alias)."""
    return row.get("oper_mode") or row.get("mode") or ""


def _read_trunk_vlans(row: dict) -> str:
    """Return raw trunk_vlans, accepting NX-API key ``trunk_vlans`` or ntc alias ``trunking_vlans``."""
    return row.get("trunk_vlans") or row.get("trunking_vlans") or ""


def nxos_row_to_switchport_info(row: dict) -> SwitchportInfo:
    """
    Build a SwitchportInfo from a normalized NX-OS row dict.

    Accepts BOTH NX-API (JSON) and ntc-templates (CLI-parsed) shapes::

        NX-API:     {"interface", "switchport", "admin_mode", "oper_mode",
                     "access_vlan", "native_vlan", "voice_vlan", "trunk_vlans"}
        ntc:        {"interface", "switchport", "mode",
                     "access_vlan", "native_vlan", "voice_vlan", "trunking_vlans"}

    NX-API uses "Enabled"/"Disabled" for ``switchport``; ntc-templates may
    emit "Enabled"/"Disabled" or other casing. Both are tolerated.

    When ntc-templates rows lack a separate ``admin_mode``, the ``mode``
    field (operational) is used as both admin AND oper signals — which
    keeps DTP-fallback correctness on the rare NX-OS deployment that
    actually negotiates DTP, and is exactly equivalent to "admin = oper"
    on the common case where DTP is disabled.
    """
    if (row.get("switchport") or "").lower() == "disabled":
        return SwitchportInfo(
            enabled=False, admin_mode=None, oper_mode=None,
            access_vlan=None, native_vlan=None, allowed_vlans=None,
        )

    trunk_vlans_raw = _read_trunk_vlans(row)
    if trunk_vlans_raw:
        vids, is_wildcard = parse_vlan_range_string(trunk_vlans_raw)
        allowed: list[int] | str | None = "all" if is_wildcard else vids
    else:
        allowed = None

    return SwitchportInfo(
        enabled=True,
        admin_mode=_normalize_admin(_read_admin_mode(row)),  # type: ignore[arg-type]
        oper_mode=_normalize_oper(_read_oper_mode(row)),     # type: ignore[arg-type]
        access_vlan=_maybe_int(row.get("access_vlan")),
        native_vlan=_maybe_int(row.get("native_vlan")),
        allowed_vlans=allowed,
        voice_vlan=_maybe_int(row.get("voice_vlan")),
    )
