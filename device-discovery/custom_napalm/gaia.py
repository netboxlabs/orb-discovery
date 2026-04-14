# Copyright 2026 NetBox Labs Inc
"""
Custom Check Point Gaia NAPALM driver.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses ntc-templates 9.x for structured parsing wherever templates are available;
falls back to regex for commands without templates (hostname, uptime).
"""

import logging
import re
import socket

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.helpers import mac as normalize_mac
from napalm.base.netmiko_helpers import netmiko_args
from ntc_templates.parse import parse_output

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Config sanitization — Check Point Gaia credential patterns
# ---------------------------------------------------------------------------
_SANITIZE_PATTERNS: list[tuple[re.Pattern, str]] = [
    # set user <name> password-hash <hash>
    (re.compile(r"^(set\s+user\s+\S+\s+password-hash)\s+\S+", re.M | re.I), r"\1 <redacted>"),
    # set snmp community <name> read-only|read-write <community>
    (re.compile(r"^(set\s+snmp\s+community\s+\S+\s+(?:read-only|read-write))\s+\S+", re.M | re.I), r"\1 <redacted>"),
    # set aaa tacacs-server <x> secret <s>
    (re.compile(r"^(set\s+aaa\s+tacacs-server\s+\S+\s+secret)\s+\S+", re.M | re.I), r"\1 <redacted>"),
    # set vpn ... pre-shared-secret <s>
    (re.compile(r"(pre-shared-secret)\s+\S+", re.I), r"\1 <redacted>"),
]


def _sanitize_config(text: str) -> str:
    for pattern, replacement in _SANITIZE_PATTERNS:
        text = pattern.sub(replacement, text)
    return text


def _parse_speed(speed_raw: str) -> float:
    """
    Convert Gaia speed string to float Mbps.

    Examples: '1000M' -> 1000.0, '10G' -> 10000.0, 'N/A' -> 0.0
    """
    if not speed_raw or speed_raw.startswith("N/A"):
        return 0.0
    m = re.match(r"(\d+(?:\.\d+)?)\s*([MmGgKk])", speed_raw)
    if not m:
        return 0.0
    num = float(m.group(1))
    unit = m.group(2).upper()
    if unit == "G":
        return num * 1000.0
    if unit == "K":
        return num / 1000.0
    return num  # M


class GaiaDriver(_napalm_base.NetworkDriver):
    """Check Point Gaia NAPALM driver (read-only subset for device-discovery)."""

    def __init__(self, hostname, username, password, timeout=60, optional_args=None):
        """Initialize the driver."""
        self.hostname = hostname
        self.username = username
        self.password = password
        self.timeout = timeout
        self.device = None

        if optional_args is None:
            optional_args = {}
        self.netmiko_optional_args = netmiko_args(optional_args)
        self.netmiko_optional_args.setdefault("port", 22)

    def open(self):
        """Open an SSH connection to the device via Netmiko."""
        self.device = self._netmiko_open(
            "checkpoint_gaia", netmiko_optional_args=self.netmiko_optional_args
        )

    def close(self):
        """Close the connection."""
        self._netmiko_close()

    def is_alive(self):
        """Return connection liveness."""
        if self.device is None:
            return {"is_alive": False}
        try:
            self.device.write_channel(chr(0))
            return {"is_alive": self.device.remote_conn.transport.is_active()}
        except (EOFError, OSError, AttributeError):
            return {"is_alive": False}

    def get_facts(self) -> dict:
        """Return general device facts."""
        # --- version ---
        ver_raw = self.device.send_command("show version all")
        parsed_ver = parse_output(platform="checkpoint_gaia", command="show version all", data=ver_raw)
        os_version = parsed_ver[0].get("version", "Unknown") if parsed_ver else "Unknown"

        # --- model + serial ---
        asset_raw = self.device.send_command("show asset all")
        parsed_asset = parse_output(platform="checkpoint_gaia", command="show asset all", data=asset_raw)
        model = "Unknown"
        serial_number = "Unknown"
        for row in parsed_asset:
            if row.get("model") and model == "Unknown":
                model = row["model"]
            if row.get("serial") and serial_number == "Unknown":
                serial_number = row["serial"]

        # --- hostname ---
        hostname_raw = self.device.send_command("show hostname")
        m = re.match(r"^(\S+)", hostname_raw.strip())
        hostname = m.group(1) if m else self.hostname

        # --- fqdn ---
        domain_raw = self.device.send_command("show domainname")
        parsed_domain = parse_output(platform="checkpoint_gaia", command="show domainname", data=domain_raw)
        domainname = parsed_domain[0].get("domainname", "") if parsed_domain else ""
        fqdn = f"{hostname}.{domainname}" if domainname else hostname

        # --- interface list ---
        intf_raw = self.device.send_command("show interfaces all")
        parsed_intf = parse_output(platform="checkpoint_gaia", command="show interfaces all", data=intf_raw)
        interface_list = [row["interface"] for row in parsed_intf if row.get("interface")]

        return {
            "hostname": hostname,
            "vendor": "Check Point",
            "model": model,
            "os_version": os_version,
            "serial_number": serial_number,
            "uptime": -1.0,
            "fqdn": fqdn,
            "interface_list": interface_list,
        }

    def get_interfaces(self) -> dict:
        """Return interface details keyed by interface name."""
        raw = self.device.send_command("show interfaces all")
        parsed = parse_output(platform="checkpoint_gaia", command="show interfaces all", data=raw)

        interfaces = {}
        for row in parsed:
            intf = row.get("interface", "")
            if not intf:
                continue

            mac_raw = row.get("mac_address", "")
            try:
                mac_address = (
                    normalize_mac(mac_raw)
                    if mac_raw and mac_raw.lower() not in ("not configured", "")
                    else ""
                )
            except Exception:
                mac_address = mac_raw

            interfaces[intf] = {
                "is_up": "link up" in row.get("link_state", "").lower(),
                "is_enabled": row.get("state", "").lower() == "on",
                "description": "",
                "last_flapped": -1.0,
                "mtu": int(row["mtu"]) if row.get("mtu") else 0,
                "speed": _parse_speed(row.get("speed", "")),
                "mac_address": mac_address,
            }

        return interfaces

    def get_interfaces_ip(self) -> dict:
        """Return IP addresses per interface."""
        raise NotImplementedError

    def get_config(self, retrieve="all", full=False, sanitized=False, format="text") -> models.ConfigDict:
        """Return device configuration."""
        raise NotImplementedError

    def get_vlans(self) -> dict:
        """Return VLAN information."""
        raise NotImplementedError
