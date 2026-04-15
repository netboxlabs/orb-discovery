# Copyright 2026 NetBox Labs Inc
"""
Custom Cisco Small Business S300 NAPALM driver.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses Netmiko (cisco_s300) and ntc-templates for structured CLI parsing.
"""

import logging
import re

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.netmiko_helpers import netmiko_args
from ntc_templates.parse import parse_output

logger = logging.getLogger(__name__)

# Config sanitization — S300 sensitive CLI fields:
#   username <name> privilege <n> password [<enc-type>] <hash>
#   enable password [level <n>] [<enc-type>] <hash>
#   snmp-server community <community-string> [ro|rw] ...
_USERNAME_PASSWORD_RE = re.compile(
    r"(username\s+\S+\s+privilege\s+\d+\s+password)\s+.*",
    re.IGNORECASE,
)
_ENABLE_PASSWORD_RE = re.compile(
    r"(enable\s+password(?:\s+level\s+\d+)?)\s+.*",
    re.IGNORECASE,
)
_SNMP_COMMUNITY_RE = re.compile(
    r"(snmp-server\s+community)\s+\S+",
    re.IGNORECASE,
)


def _sanitize_config(text: str) -> str:
    text = _USERNAME_PASSWORD_RE.sub(r"\1 <redacted>", text)
    text = _ENABLE_PASSWORD_RE.sub(r"\1 <redacted>", text)
    text = _SNMP_COMMUNITY_RE.sub(r"\1 <redacted>", text)
    return text


def _parse_uptime(uptime_str: str) -> float:
    """Convert S300 uptime string 'DD,HH:MM:SS' to total seconds."""
    m = re.match(r"(\d+),(\d+):(\d+):(\d+)", uptime_str.strip())
    if not m:
        return 0.0
    days, hours, minutes, secs = int(m.group(1)), int(m.group(2)), int(m.group(3)), int(m.group(4))
    return float(days * 86400 + hours * 3600 + minutes * 60 + secs)


def _expand_interface_range(range_str: str) -> list[str]:
    """
    Expand a comma-separated interface list with optional ranges into individual names.

    Examples:
        "fa1-2,fa4-8,gi1" -> ["fa1", "fa2", "fa4", "fa5", "fa6", "fa7", "fa8", "gi1"]
        "gi1" -> ["gi1"]
        "" -> []

    """
    if not range_str:
        return []
    result = []
    for token in range_str.strip().rstrip(",").split(","):
        token = token.strip()
        if not token:
            continue
        # Match "prefix<start>-<end>" e.g. "fa1-2", "fa4-8"
        m = re.match(r"([a-zA-Z]+)(\d+)-(\d+)$", token)
        if m:
            prefix = m.group(1)
            start, end = int(m.group(2)), int(m.group(3))
            result.extend(f"{prefix}{i}" for i in range(start, end + 1))
        else:
            result.append(token)
    return result


class S300Driver(_napalm_base.NetworkDriver):
    """Cisco Small Business S300 NAPALM driver (read-only subset for device-discovery)."""

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
            "cisco_s300", netmiko_optional_args=self.netmiko_optional_args
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
        sys_out = self.device.send_command("show system")
        parsed_sys = parse_output(platform="cisco_s300", command="show system", data=sys_out)

        hostname = "Unknown"
        model = "Unknown"
        uptime = 0.0

        if parsed_sys:
            row = parsed_sys[0]
            hostname = row.get("hostname", "Unknown") or "Unknown"
            model = row.get("description", "Unknown") or "Unknown"
            uptime = _parse_uptime(row.get("up_time", ""))

        ver_out = self.device.send_command("show version")
        parsed_ver = parse_output(platform="cisco_s300", command="show version", data=ver_out)
        os_version = "Unknown"
        if parsed_ver:
            os_version = parsed_ver[0].get("sw_version", "Unknown") or "Unknown"

        id_out = self.device.send_command("show system id")
        parsed_id = parse_output(platform="cisco_s300", command="show system id", data=id_out)
        serial_number = "Unknown"
        if parsed_id:
            serial_number = parsed_id[0].get("serial_number", "Unknown") or "Unknown"

        status_out = self.device.send_command("show interfaces status")
        parsed_status = parse_output(
            platform="cisco_s300", command="show interfaces status", data=status_out
        )
        interface_list = [
            row["port"]
            for row in parsed_status
            if row.get("port") and row.get("linkstate", "").lower() != "not present"
        ]

        return {
            "hostname": hostname,
            "vendor": "Cisco",
            "model": model,
            "os_version": os_version,
            "serial_number": serial_number,
            "uptime": uptime,
            "fqdn": "Unknown",
            "interface_list": interface_list,
        }

    def get_interfaces(self) -> dict:
        """Return interface details keyed by interface name."""
        status_out = self.device.send_command("show interfaces status")
        parsed_status = parse_output(
            platform="cisco_s300", command="show interfaces status", data=status_out
        )

        desc_out = self.device.send_command("show interfaces description")
        parsed_desc = parse_output(
            platform="cisco_s300", command="show interfaces description", data=desc_out
        )
        desc_map = {row["interface"]: row.get("description", "") for row in parsed_desc}

        interfaces = {}
        for row in parsed_status:
            port = row.get("port", "")
            if not port:
                continue

            linkstate = row.get("linkstate", "").lower()
            speed_raw = row.get("speed", "")
            try:
                speed = float(speed_raw) if speed_raw and speed_raw != "--" else -1.0
            except ValueError:
                speed = -1.0

            interfaces[port] = {
                "is_up": linkstate == "up",
                "is_enabled": linkstate != "not present",
                "description": desc_map.get(port, ""),
                "last_flapped": -1.0,
                "mtu": -1,
                "speed": speed,
                "mac_address": "",
            }

        return interfaces

    def get_interfaces_ip(self) -> dict:
        """Return IP addresses per interface."""
        ip_out = self.device.send_command("show ip interface")
        parsed = parse_output(platform="cisco_s300", command="show ip interface", data=ip_out)

        interfaces_ip: dict = {}
        for row in parsed:
            ip_with_prefix = row.get("ip", "")
            intf = row.get("interface", "")
            if not ip_with_prefix or not intf or "/" not in ip_with_prefix:
                continue
            try:
                ip, prefix_str = ip_with_prefix.split("/", 1)
                prefix_length = int(prefix_str)
            except (ValueError, AttributeError):
                continue
            interfaces_ip.setdefault(intf, {}).setdefault("ipv4", {})[ip] = {
                "prefix_length": prefix_length
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
        vlan_out = self.device.send_command("show vlan")
        parsed = parse_output(platform="cisco_s300", command="show vlan", data=vlan_out)

        vlans: dict = {}
        for row in parsed:
            vlan_id = row.get("vlan_id", "")
            if not vlan_id:
                continue
            vlan_name = row.get("vlan_name", "") or vlan_id
            raw_interfaces = row.get("interfaces", "") or ""
            interfaces = _expand_interface_range(raw_interfaces)
            entry = vlans.setdefault(vlan_id, {"name": vlan_name, "interfaces": []})
            for intf in interfaces:
                if intf not in entry["interfaces"]:
                    entry["interfaces"].append(intf)

        return vlans
