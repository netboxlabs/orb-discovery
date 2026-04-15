# Copyright 2026 NetBox Labs Inc
# Based on napalm-arubaos-switch (Apache-2.0): https://github.com/napalm-automation-community/napalm-arubaos-switch
"""
Custom ArubaOS-Switch NAPALM driver (HP ProCurve legacy line).

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses Netmiko (hp_procurve device type) and ntc-templates 9.x for structured
CLI parsing wherever templates are available; falls back to ipaddress for
subnet-mask-to-prefix-length conversion.
"""

import ipaddress
import logging
import re

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.netmiko_helpers import netmiko_args
from ntc_templates.parse import parse_output
from textfsm.parser import TextFSMError

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Config sanitization
# ---------------------------------------------------------------------------

# HP/Aruba ProCurve: "password manager [sha1] <hash>" / "password operator [sha1] <hash>"
# Redact the full line after "password manager/operator" (algorithm + hash)
_PASSWORD_RE = re.compile(
    r"(password\s+(?:manager|operator))\s+.*",
    re.IGNORECASE,
)

# "radius-server [host] <ip> key <secret>" — RADIUS shared secret
_RADIUS_KEY_RE = re.compile(
    r"(radius-server\b[^\n]*?\bkey)\s+\S+",
    re.IGNORECASE,
)

# "tacacs-server [host] <ip> key <secret>" — TACACS+ shared secret
_TACACS_KEY_RE = re.compile(
    r"(tacacs-server\b[^\n]*?\bkey)\s+\S+",
    re.IGNORECASE,
)

# "snmp-server community <string> ..." — SNMP community string
_SNMP_COMMUNITY_RE = re.compile(
    r"(snmp-server\s+community)\s+\S+",
    re.IGNORECASE,
)


def _sanitize_config(text: str) -> str:
    text = _PASSWORD_RE.sub(r"\1 <redacted>", text)
    text = _RADIUS_KEY_RE.sub(r"\1 <redacted>", text)
    text = _TACACS_KEY_RE.sub(r"\1 <redacted>", text)
    text = _SNMP_COMMUNITY_RE.sub(r"\1 <redacted>", text)
    return text


# ---------------------------------------------------------------------------
# Uptime helpers
# ---------------------------------------------------------------------------

_UPTIME_FACTORS = {
    "second": 1,
    "minute": 60,
    "hour": 3600,
    "day": 86400,
    "week": 604800,
}


def _parse_uptime(uptime_str: str) -> float:
    """
    Convert an ArubaOS-Switch single-unit uptime string to seconds.

    ``show system`` returns a value like "5 days" or "24 hours".
    """
    m = re.match(r"(\d+)\s+(\w+)", uptime_str.strip())
    if not m:
        return -1.0
    value = int(m.group(1))
    unit = m.group(2).lower().rstrip("s")  # strip trailing 's' for plurals
    factor = _UPTIME_FACTORS.get(unit)
    if factor is None:
        return -1.0
    return float(value * factor)


def _mask_to_prefix(subnet_mask: str) -> int:
    """Convert dotted-decimal subnet mask to prefix length."""
    try:
        return ipaddress.IPv4Network(f"0.0.0.0/{subnet_mask}", strict=False).prefixlen
    except ValueError:
        return -1


class ArubaOSSDriver(_napalm_base.NetworkDriver):
    """ArubaOS-Switch NAPALM driver (HP ProCurve legacy line)."""

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
            "hp_procurve", netmiko_optional_args=self.netmiko_optional_args
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
        hostname = os_version = serial_number = "Unknown"
        uptime = -1.0

        sys_out = self.device.send_command("show system")
        try:
            parsed_sys = parse_output(
                platform="hp_procurve", command="show system", data=sys_out
            )
        except TextFSMError:
            logger.warning("Failed to parse show system output")
            parsed_sys = []
        if parsed_sys:
            row = parsed_sys[0]
            hostname = row.get("name", "Unknown") or "Unknown"
            os_version = row.get("software_version", "Unknown") or "Unknown"
            serial_number = row.get("serial", "Unknown") or "Unknown"
            uptime = _parse_uptime(row.get("uptime", ""))

        brief_out = self.device.send_command("show interfaces brief")
        try:
            parsed_brief = parse_output(
                platform="hp_procurve", command="show interfaces brief", data=brief_out
            )
        except TextFSMError:
            logger.warning("Failed to parse show interfaces brief output")
            parsed_brief = []
        interface_list = [row["port"] for row in parsed_brief if row.get("port")]

        return {
            "hostname": hostname,
            "vendor": "HPE Aruba",
            "model": "Unknown",
            "os_version": os_version,
            "serial_number": serial_number,
            "uptime": uptime,
            "fqdn": "Unknown",
            "interface_list": interface_list,
        }

    def get_interfaces(self) -> dict:
        """Return interface details keyed by interface name."""
        brief_out = self.device.send_command("show interfaces brief")
        try:
            parsed = parse_output(
                platform="hp_procurve", command="show interfaces brief", data=brief_out
            )
        except TextFSMError:
            logger.warning("Failed to parse show interfaces brief output")
            parsed = []
        interfaces = {}
        for row in parsed:
            port = row.get("port", "")
            if not port:
                continue
            enabled = row.get("enabled", "").lower() == "yes"
            status = row.get("status", "").lower() == "up"
            mode = row.get("mode", "")

            # Infer speed (Mbps) from mode field ("1000FDx" → 1000.0, "10GigFDx" → 10000.0)
            speed = _parse_speed(mode)

            interfaces[port] = {
                "is_up": status,
                "is_enabled": enabled,
                "description": "",
                "last_flapped": -1.0,
                "mtu": -1,
                "speed": speed,
                "mac_address": "",
            }
        return interfaces

    def get_interfaces_ip(self) -> dict:
        """Return IP addresses per interface."""
        interfaces_ip: dict = {}

        ip_out = self.device.send_command("show ip")
        try:
            parsed = parse_output(platform="hp_procurve", command="show ip", data=ip_out)
        except TextFSMError:
            logger.warning("Failed to parse show ip output")
            parsed = []
        for row in parsed:
            vlan_name = row.get("vlan_name", "").strip()
            ip_addr = row.get("ip_address", "").strip()
            mask = row.get("subnet_mask", "").strip()
            if not vlan_name or not ip_addr or not mask:
                continue
            prefix_len = _mask_to_prefix(mask)
            if prefix_len < 0:
                continue
            interfaces_ip.setdefault(vlan_name, {}).setdefault("ipv4", {})[ip_addr] = {
                "prefix_length": prefix_len
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
            config["running"] = self.device.send_command("show running-config")
        if retrieve.lower() in ("startup", "all"):
            config["startup"] = self.device.send_command("show startup-config")

        if sanitized:
            for key in ("running", "candidate", "startup"):
                if config[key]:
                    config[key] = _sanitize_config(config[key])

        return config

    def get_vlans(self) -> dict:
        """Return VLAN information keyed by VLAN ID string."""
        vlan_out = self.device.send_command("show vlans")
        try:
            parsed = parse_output(platform="hp_procurve", command="show vlans", data=vlan_out)
        except TextFSMError:
            logger.warning("Failed to parse show vlans output")
            parsed = []

        vlans: dict = {}
        for row in parsed:
            vlan_id = row.get("vlan_id", "")
            if not vlan_id:
                continue
            vlan_name = row.get("vlan_name", "").strip() or vlan_id
            vlans[vlan_id] = {"name": vlan_name, "interfaces": []}

        return vlans


# ---------------------------------------------------------------------------
# Speed helper
# ---------------------------------------------------------------------------

_SPEED_MAP = {
    "10GigFDx": 10000.0,
    "10GigHDx": 10000.0,
    "1000FDx": 1000.0,
    "1000HDx": 1000.0,
    "100FDx": 100.0,
    "100HDx": 100.0,
    "10FDx": 10.0,
    "10HDx": 10.0,
}


def _parse_speed(mode: str) -> float:
    """Infer speed in Mbps from the Netmiko interface mode string."""
    return _SPEED_MAP.get(mode, -1.0)
