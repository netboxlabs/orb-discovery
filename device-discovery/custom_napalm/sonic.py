# Copyright 2026 NetBox Labs Inc
"""
Custom Dell SONiC NAPALM driver.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses Netmiko with the ``dell_sonic`` device type for SSH transport.
All CLI parsing uses regex (no ntc-templates exist for this platform).
"""

import logging
import re

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.netmiko_helpers import netmiko_args

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Config sanitisation — Dell SONiC sensitive patterns
# ---------------------------------------------------------------------------
_PASSWORD_RE = re.compile(
    r"((?:encrypted-password|password|auth-password)\s+)\S+",
    re.IGNORECASE,
)
_TACACS_KEY_RE = re.compile(
    r"(tacacs-server\s+.*\s+key\s+)\S+",
    re.IGNORECASE,
)
_RADIUS_KEY_RE = re.compile(
    r"(radius-server\s+.*\s+key\s+)\S+",
    re.IGNORECASE,
)
_SNMP_COMMUNITY_RE = re.compile(
    r"(snmp-server\s+community\s+)\S+",
    re.IGNORECASE,
)
_SECRET_RE = re.compile(
    r"(\bsecret\s+)\S+",
    re.IGNORECASE,
)
_ENABLE_PASSWORD_RE = re.compile(
    r"(enable\s+password\s+)\S+",
    re.IGNORECASE,
)
_BGP_PASSWORD_RE = re.compile(
    r"(neighbor\s+\S+\s+password\s+)\S+",
    re.IGNORECASE,
)


def _sanitize_config(text: str) -> str:
    """Replace sensitive values in configuration text with ``<redacted>``."""
    text = _PASSWORD_RE.sub(r"\1<redacted>", text)
    text = _TACACS_KEY_RE.sub(r"\1<redacted>", text)
    text = _RADIUS_KEY_RE.sub(r"\1<redacted>", text)
    text = _SNMP_COMMUNITY_RE.sub(r"\1<redacted>", text)
    text = _SECRET_RE.sub(r"\1<redacted>", text)
    text = _ENABLE_PASSWORD_RE.sub(r"\1<redacted>", text)
    text = _BGP_PASSWORD_RE.sub(r"\1<redacted>", text)
    return text


# ---------------------------------------------------------------------------
# Uptime parsing
# ---------------------------------------------------------------------------
_HOUR_SECONDS = 3600
_DAY_SECONDS = 24 * _HOUR_SECONDS
_WEEK_SECONDS = 7 * _DAY_SECONDS
_YEAR_SECONDS = 365 * _DAY_SECONDS

# Regex for interface names in SONiC CLI output
_INTF_RE = r"(Ethernet\d+|PortChannel\d+|Vlan\d+|Loopback\d+|Management\d+)"

# Map of ``show version`` field names to extraction regexes.
# Each tuple: (field_key, compiled regex).  The first capture group is the value.
_VERSION_PATTERNS = [
    ("os_version", re.compile(r"(?:SONiC\s+)?Software\s+Version\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("hwsku", re.compile(r"HwSKU\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("product", re.compile(r"Product\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("platform", re.compile(r"Platform\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("serial_number", re.compile(r"Serial\s+Number\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("uptime", re.compile(r"(?:Up\s*[Tt]ime|Uptime)\s*[:\-]\s*(.+)", re.IGNORECASE)),
    ("hostname", re.compile(r"Hostname\s*[:\-]\s*(.+)", re.IGNORECASE)),
]


def _parse_uptime(uptime_str: str) -> float:
    """Convert a SONiC uptime string to total seconds."""
    seconds = 0.0

    for pattern, factor in (
        (r"(\d+)\s+year", _YEAR_SECONDS),
        (r"(\d+)\s+week", _WEEK_SECONDS),
        (r"(\d+)\s+day", _DAY_SECONDS),
        (r"(\d+)\s+hour", _HOUR_SECONDS),
        (r"(\d+)\s+minute", 60),
        (r"(\d+)\s+second", 1),
    ):
        m = re.search(pattern, uptime_str, re.IGNORECASE)
        if m:
            seconds += int(m.group(1)) * factor

    # HH:MM:SS component (alternative format)
    m = re.search(r"(\d+):(\d+):(\d+)", uptime_str)
    if m:
        seconds += int(m.group(1)) * _HOUR_SECONDS
        seconds += int(m.group(2)) * 60
        seconds += int(m.group(3))

    return seconds


def _parse_speed(speed_str: str) -> float:
    """Convert a speed string like ``100G`` or ``1G`` to Mbps."""
    if not speed_str:
        return -1.0
    speed_str = speed_str.strip().upper()
    m = re.match(r"(\d+(?:\.\d+)?)\s*([GTM])?", speed_str)
    if not m:
        return -1.0
    val = float(m.group(1))
    unit = m.group(2) or ""
    if unit == "G":
        return val * 1000.0
    if unit == "T":
        return val * 1000000.0
    if unit == "M":
        return val
    return val


def _parse_version_fields(output: str) -> dict:
    """Extract key/value pairs from ``show version`` output."""
    fields: dict[str, str] = {}
    for line in output.splitlines():
        stripped = line.strip()
        for key, pattern in _VERSION_PATTERNS:
            m = pattern.match(stripped)
            if m:
                fields[key] = m.group(1).strip()
                break
    return fields


class SONiCDriver(_napalm_base.NetworkDriver):
    """Dell SONiC NAPALM driver (read-only subset for device-discovery)."""

    def __init__(self, hostname, username, password, timeout=60, optional_args=None):
        """Initialise connection parameters."""
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
            "dell_sonic", netmiko_optional_args=self.netmiko_optional_args
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

    # ------------------------------------------------------------------
    # NAPALM getters
    # ------------------------------------------------------------------

    def get_facts(self) -> dict:
        """Return general device facts."""
        output = self.device.send_command("show version")
        if not output:
            return {}

        fields = _parse_version_fields(output)

        # Model priority: HwSKU > Product > Platform
        model = fields.get("hwsku") or fields.get("product") or fields.get("platform", "Unknown")

        uptime = _parse_uptime(fields["uptime"]) if "uptime" in fields else -1.0

        # Build interface list from show interfaces status
        interface_list = _extract_interface_names(
            self.device.send_command("show interfaces status")
        )

        return {
            "hostname": fields.get("hostname", "Unknown"),
            "vendor": "Dell",
            "model": model,
            "os_version": fields.get("os_version", "Unknown"),
            "serial_number": fields.get("serial_number", "Unknown"),
            "uptime": float(uptime),
            "fqdn": "Unknown",
            "interface_list": sorted(interface_list),
        }

    def get_interfaces(self) -> dict:
        """Return interface details keyed by interface name."""
        output = self.device.send_command("show interfaces status")
        if not output:
            return {}

        interfaces = {}
        for line in output.splitlines():
            m = re.match(
                _INTF_RE
                + r"\s+(up|down)\s+(up|down)"
                r"(?:\s+(\S+))?"   # speed
                r"\s+(.*?)"        # description (may be empty)
                r"\s+(\d+)\s*$",   # mtu (anchored to end of line)
                line,
            )
            if m:
                interfaces[m.group(1)] = {
                    "is_up": m.group(3) == "up",
                    "is_enabled": m.group(2) == "up",
                    "description": (m.group(5) or "").strip(),
                    "last_flapped": -1.0,
                    "mtu": int(m.group(6)),
                    "speed": _parse_speed(m.group(4) or ""),
                    "mac_address": "",
                }

        return interfaces

    def get_interfaces_ip(self) -> dict:
        """Return IP addresses per interface."""
        interfaces_ip: dict = {}

        # --- IPv4 ---
        ipv4_out = self.device.send_command("show ip interface")
        if ipv4_out:
            for line in ipv4_out.splitlines():
                m = re.match(_INTF_RE + r"\s+(\d+\.\d+\.\d+\.\d+)/(\d+)", line)
                if m:
                    interfaces_ip.setdefault(m.group(1), {}).setdefault("ipv4", {})[m.group(2)] = {
                        "prefix_length": int(m.group(3))
                    }

        # --- IPv6 ---
        ipv6_out = self.device.send_command("show ipv6 interface")
        if ipv6_out:
            for line in ipv6_out.splitlines():
                m = re.match(_INTF_RE + r"\s+([0-9a-fA-F:]+)/(\d+)", line)
                if m:
                    interfaces_ip.setdefault(m.group(1), {}).setdefault("ipv6", {})[m.group(2)] = {
                        "prefix_length": int(m.group(3))
                    }

        return interfaces_ip

    def get_config(
        self,
        retrieve: str = "all",
        full: bool = False,
        sanitized: bool = False,
        format: str = "text",
    ) -> models.ConfigDict:
        """Return device configuration."""
        config: models.ConfigDict = {"running": "", "candidate": "", "startup": ""}

        if retrieve.lower() in ("running", "all"):
            config["running"] = self.device.send_command("show running-configuration")

        if sanitized:
            for key in ("running", "candidate", "startup"):
                if config[key]:
                    config[key] = _sanitize_config(config[key])

        return config

    def get_vlans(self) -> dict:
        """Return VLAN information keyed by VLAN ID string."""
        output = self.device.send_command("show vlan brief")
        if not output:
            return {}

        vlans: dict = {}
        for line in output.splitlines():
            m = re.match(r"\s*(\d+)\s+(\S+)\s+(.*?)\s+(active|suspend)", line)
            if m:
                members_str = m.group(3).strip()
                vlans[m.group(1)] = {
                    "name": m.group(2),
                    "interfaces": [i.strip() for i in members_str.split(",") if i.strip()] if members_str else [],
                }

        return vlans


def _extract_interface_names(output: str) -> list[str]:
    """Extract interface names from ``show interfaces status`` output."""
    names = []
    if output:
        for line in output.splitlines():
            m = re.match(_INTF_RE + r"\s", line)
            if m:
                names.append(m.group(1))
    return names
