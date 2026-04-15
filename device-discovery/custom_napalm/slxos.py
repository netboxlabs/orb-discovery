# Copyright 2026 NetBox Labs Inc
"""
Custom Extreme SLX-OS NAPALM driver.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses Netmiko (extreme_slx device type) + ntc-templates for structured parsing
wherever templates are available (show ip interface brief); falls back to regex
for commands without templates (show version, show interface brief, show vlan brief).
"""

import logging
import re

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.netmiko_helpers import netmiko_args
from ntc_templates.parse import parse_output

logger = logging.getLogger(__name__)

# --- config sanitization -------------------------------------------------- #
# "username admin password encrypted <hash>"
_PASSWORD_ENCRYPTED_RE = re.compile(
    r"((?:password|passwd)\s+encrypted)\s+\S+",
    re.IGNORECASE,
)
# "enable secret sha256 <hash>" / "enable secret 8 <hash>"
_ENABLE_SECRET_RE = re.compile(
    r"(enable\s+secret\s+\S+)\s+\S+",
    re.IGNORECASE,
)
# "snmp-server community <string> ro" / "snmp-server community <string> rw"
_SNMP_COMMUNITY_RE = re.compile(
    r"(snmp-server\s+community)\s+\S+(\s+(?:ro|rw))",
    re.IGNORECASE,
)
# "radius-server host <ip> ... key <key>"
_RADIUS_KEY_RE = re.compile(
    r"(\bradius-server\s+host\s+\S+.*?\bkey)\s+\S+",
    re.IGNORECASE,
)
# "tacacs-server host <ip> ... key <key>"
_TACACS_KEY_RE = re.compile(
    r"(\btacacs-server\s+host\s+\S+.*?\bkey)\s+\S+",
    re.IGNORECASE,
)


def _sanitize_config(text: str) -> str:
    text = _PASSWORD_ENCRYPTED_RE.sub(r"\1 <redacted>", text)
    text = _ENABLE_SECRET_RE.sub(r"\1 <redacted>", text)
    text = _SNMP_COMMUNITY_RE.sub(r"\1 <redacted>\2", text)
    text = _RADIUS_KEY_RE.sub(r"\1 <redacted>", text)
    text = _TACACS_KEY_RE.sub(r"\1 <redacted>", text)
    return text


# --- uptime helpers -------------------------------------------------------- #
_HOUR_SECONDS = 3_600
_DAY_SECONDS = 24 * _HOUR_SECONDS
_WEEK_SECONDS = 7 * _DAY_SECONDS
_YEAR_SECONDS = 365 * _DAY_SECONDS


def _parse_uptime(uptime_str: str) -> float:
    """
    Convert an SLX-OS uptime string to seconds.

    Handles the format: "X days, X hours, X minutes, X seconds"
    as well as partial forms (e.g. "5 hours, 2 minutes, 10 seconds").
    """
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
    return seconds


# --- interface brief parsing ----------------------------------------------- #
# Matches SLX-OS "show interface brief" rows:
#   "Ethernet 0/1    up    10G"
#   "Management 1    up    1G"
#   "Port-channel 1  up    -"
#   "Loopback 1      up    -"
#   "Ve 10           up    -"
_INTF_BRIEF_RE = re.compile(
    r"^((?:Ethernet|Management|Port-channel|Loopback|Ve)\s+\S+)\s+(up|down)\s+(\S+)",
    re.M | re.IGNORECASE,
)

# --- vlan brief parsing ---------------------------------------------------- #
# Matches leading VLAN row: "1     Default         active   Eth 0/1 Eth 0/2"
# The VLAN ID and name are captured; ports are on the same line or continuation lines.
_VLAN_ROW_RE = re.compile(
    r"^(\d+)\s+(\S+)\s+(?:active|inactive)\s*(.*)?$",
    re.M,
)
# Port tokens like "Eth 0/1" appearing in VLAN output
_VLAN_PORT_RE = re.compile(r"((?:Eth|Po)\s*\S+)")


class SLXOSDriver(_napalm_base.NetworkDriver):
    """Extreme SLX-OS NAPALM driver (read-only subset for device-discovery)."""

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
            "extreme_slx", netmiko_optional_args=self.netmiko_optional_args
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

    # ---------------------------------------------------------------------- #
    # NAPALM getters
    # ---------------------------------------------------------------------- #

    def get_facts(self) -> dict:
        """Return general device facts."""
        hostname = "Unknown"
        model = "Unknown"
        os_version = "Unknown"
        serial_number = "Unknown"
        uptime: float = -1.0

        ver_output = self.device.send_command("show version")
        if ver_output:
            # Hostname: "System Name: slx9640"
            m = re.search(r"System\s+Name\s*:\s*(\S+)", ver_output, re.IGNORECASE)
            if m:
                hostname = m.group(1)

            # Model: "Chassis information for: SLX_9640" or "Chassis Type: SLX 9640"
            m = re.search(
                r"Chassis(?:\s+information\s+for|\s+Type)\s*:\s+(.+)",
                ver_output,
                re.IGNORECASE,
            )
            if m:
                # Normalise "SLX_9640" → "SLX 9640"
                model = m.group(1).strip().replace("_", " ")

            # OS version: "SLX-OS Software Version: SLX-OS 20.2.3"
            m = re.search(
                r"SLX-OS\s+Software\s+Version\s*:\s*\S+\s+(\S+)",
                ver_output,
                re.IGNORECASE,
            )
            if not m:
                # Fallback: "SW-Version: SLX-OS_v20.2.3_SLX_9640" → extract version token
                m = re.search(r"SW-Version\s*:\s*\S+?v?(\d+\.\d+\S*)", ver_output, re.IGNORECASE)
            if m:
                os_version = m.group(1)

            # Serial number: "SN: FTX2244H01B3"
            m = re.search(r"\bSN\s*:\s+(\S+)", ver_output, re.IGNORECASE)
            if m:
                serial_number = m.group(1)

            # Uptime: "System uptime: 0 days, 2 hours, 17 minutes, 30 seconds"
            m = re.search(r"System\s+uptime\s*:\s*(.+)", ver_output, re.IGNORECASE)
            if m:
                uptime = _parse_uptime(m.group(1))

        # Interface list: reuse show ip interface brief (ntc-template)
        ip_brief_output = self.device.send_command("show ip interface brief")
        interface_list = self._parse_interface_list(ip_brief_output)

        return {
            "hostname": hostname,
            "vendor": "Extreme",
            "model": model,
            "os_version": os_version,
            "serial_number": serial_number,
            "uptime": uptime,
            "fqdn": "Unknown",
            "interface_list": interface_list,
        }

    def get_interfaces(self) -> dict:
        """Return interface details keyed by interface name."""
        output = self.device.send_command("show interface brief")
        if not output:
            return {}

        interfaces = {}
        for m in _INTF_BRIEF_RE.finditer(output):
            name = m.group(1).strip()
            state = m.group(2).lower()
            interfaces[name] = {
                "is_up": state == "up",
                "is_enabled": state == "up",
                "description": "",
                "last_flapped": -1.0,
                "mtu": -1,
                "speed": -1.0,
                "mac_address": "",
            }
        return interfaces

    def get_interfaces_ip(self) -> dict:
        """Return IP addresses per interface, keyed by interface name."""
        output = self.device.send_command("show ip interface brief")
        if not output:
            return {}

        try:
            parsed = parse_output(
                platform="extreme_slxos",
                command="show ip interface brief",
                data=output,
            )
        except Exception:
            logger.warning("slxos: ntc-template failed for 'show ip interface brief'; returning {}")
            return {}

        interfaces_ip: dict = {}
        for row in parsed:
            intf = row.get("interface", "").strip()
            ip_addr = row.get("ip_address", "").strip()
            if not intf or not ip_addr or ip_addr == "unassigned":
                continue
            # ip_addr may include prefix length e.g. "192.168.1.1/24"
            if "/" in ip_addr:
                ip, prefix_str = ip_addr.split("/", 1)
                try:
                    prefix_len = int(prefix_str)
                except ValueError:
                    logger.warning("slxos: unparseable prefix %r on %s; skipping", ip_addr, intf)
                    continue
            else:
                ip = ip_addr
                prefix_len = 32
            interfaces_ip.setdefault(intf, {}).setdefault("ipv4", {})[ip] = {
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

        if retrieve in ("all", "running"):
            config["running"] = self.device.send_command("show running-config")

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
        for m in _VLAN_ROW_RE.finditer(output):
            vlan_id = m.group(1)
            name = m.group(2)
            port_str = m.group(3) or ""
            ports = [tok.strip() for tok in _VLAN_PORT_RE.findall(port_str)]
            vlans[vlan_id] = {"name": name, "interfaces": ports}
        return vlans

    # ---------------------------------------------------------------------- #
    # Helpers
    # ---------------------------------------------------------------------- #

    def _parse_interface_list(self, output: str) -> list:
        """Extract interface names from 'show ip interface brief' via ntc-template."""
        if not output:
            return []
        try:
            parsed = parse_output(
                platform="extreme_slxos",
                command="show ip interface brief",
                data=output,
            )
            return [row["interface"].strip() for row in parsed if row.get("interface")]
        except Exception:
            logger.warning(
                "slxos: ntc-template failed for 'show ip interface brief' (interface list); "
                "falling back to regex"
            )
            return [m.group(1).strip() for m in _INTF_BRIEF_RE.finditer(output)]
