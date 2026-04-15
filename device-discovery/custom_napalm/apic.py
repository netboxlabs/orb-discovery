# Copyright 2026 NetBox Labs Inc
"""
Custom Cisco APIC NAPALM driver — SSH CLI via Netmiko + ntc-templates/regex.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Connection uses the ``cisco_apic`` Netmiko device type.  ntc-templates provides
the ``cisco_apic`` platform for ``get_vlans`` (``fabric show vlan extended``).
All other commands are parsed with compiled regular expressions, as no
ntc-templates exist for those commands under the cisco_apic platform.
"""

import logging
import re

import napalm.base as _napalm_base
from napalm.base import models
from napalm.base.netmiko_helpers import netmiko_args
from ntc_templates.parse import parse_output

logger = logging.getLogger(__name__)

_PLATFORM = "cisco_apic"

# ---------------------------------------------------------------------------
# Config sanitization — Cisco APIC sensitive fields
# ---------------------------------------------------------------------------

# "password <v>" / "secret <v>" as the first non-whitespace token (indented account lines)
_PASSWORD_RE = re.compile(r"^(\s*(?:password|secret))\s+\S+.*", re.IGNORECASE | re.MULTILINE)

# "enable password/secret [type] <v>" and "username <u> [privilege N] password/secret [type] <v>"
# Uses \S+.* to capture the type indicator AND hash (e.g. "5 $1$abc$hash") in one pass.
_INLINE_PASSWORD_RE = re.compile(
    r"(\b(?:enable\s+(?:password|secret)|username\s+\S+(?:\s+\S+)*?\s+(?:password|secret)))\s+\S+.*",
    re.IGNORECASE | re.MULTILINE,
)

# RADIUS / TACACS+ "... key [<type>] <value>" — ".*" consumes optional type indicator + secret
_KEY_LINE_RE = re.compile(
    r"^(\s*(?:radius-server|tacacs-server)\b.*\bkey)\s+\S+.*",
    re.IGNORECASE | re.MULTILINE,
)

# SNMP community — optional type indicator digit before the actual community string
# e.g. "snmp-server community public ro" or "snmp-server community 7 ABC123 ro"
_COMMUNITY_RE = re.compile(
    r"^(\s*snmp-server\s+community)(?:\s+\d+)?\s+\S+",
    re.IGNORECASE | re.MULTILINE,
)

# Standalone "key <value>" — excludes key-chain identifiers ("key chain X", "key 1")
_BARE_KEY_RE = re.compile(
    r"^(\s*key)\s+(?!chain\b)(?!\d+\b)\S+",
    re.IGNORECASE | re.MULTILINE,
)

# "key-string <v>" inside a Cisco key-chain block
_KEY_STRING_RE = re.compile(r"^(\s*key-string)\s+\S+", re.IGNORECASE | re.MULTILINE)


def _sanitize_config(text: str) -> str:
    text = _PASSWORD_RE.sub(r"\1 <redacted>", text)
    text = _INLINE_PASSWORD_RE.sub(r"\1 <redacted>", text)
    text = _KEY_LINE_RE.sub(r"\1 <redacted>", text)
    text = _COMMUNITY_RE.sub(r"\1 <redacted>", text)
    text = _BARE_KEY_RE.sub(r"\1 <redacted>", text)
    text = _KEY_STRING_RE.sub(r"\1 <redacted>", text)
    return text


# ---------------------------------------------------------------------------
# Uptime helpers
# ---------------------------------------------------------------------------

_DAY_SECONDS = 86400
_HOUR_SECONDS = 3600
_MINUTE_SECONDS = 60

_UPTIME_RE = re.compile(
    r"(?:(?P<days>\d+)\s+days?(?:,\s*)?)?"
    r"(?:(?P<hours>\d+)\s+hours?(?:,\s*)?)?"
    r"(?:(?P<minutes>\d+)\s+minutes?(?:,\s*)?)?"
    r"(?:(?P<seconds>\d+)\s+seconds?)?",
    re.IGNORECASE,
)


def _parse_uptime(uptime_str: str) -> float:
    """Convert an APIC uptime string to total seconds."""
    m = _UPTIME_RE.search(uptime_str)
    total = 0.0
    if m.group("days"):
        total += int(m.group("days")) * _DAY_SECONDS
    if m.group("hours"):
        total += int(m.group("hours")) * _HOUR_SECONDS
    if m.group("minutes"):
        total += int(m.group("minutes")) * _MINUTE_SECONDS
    if m.group("seconds"):
        total += int(m.group("seconds"))
    return total


# ---------------------------------------------------------------------------
# Interface parsing helpers
# ---------------------------------------------------------------------------

# Opening line: "Interface eth2-1 is up, line protocol is up"
# group(2) captures the full admin-state token, including the optional "administratively " prefix,
# so callers can distinguish "administratively down" (is_enabled=False) from plain "down"
# (link-down only, is_enabled=True).
_INTF_HEADER_RE = re.compile(
    r"^(?:Interface\s+)?(\S+)\s+is\s+((?:administratively\s+)?(?:up|down)).*?line\s+protocol\s+is\s+(up|down)",
    re.IGNORECASE | re.MULTILINE,
)

# MAC address
_MAC_RE = re.compile(r"address\s+is\s+([0-9a-f]{2}(?:[:.][0-9a-f]{2}){5})", re.IGNORECASE)

# IPv4 address in CIDR notation
_IPV4_RE = re.compile(r"Internet\s+address\s+is\s+(\d+\.\d+\.\d+\.\d+/\d+)", re.IGNORECASE)

# IPv6 address: "IPv6 address: 2001:db8::1/64"
_IPV6_RE = re.compile(r"IPv6\s+address:\s+([0-9a-fA-F:]+/\d+)", re.IGNORECASE)

# MTU
_MTU_RE = re.compile(r"MTU\s+(\d+)\s+bytes", re.IGNORECASE)

# Speed: "Speed: 1000 Mbps" or "1000 Gbps"
_SPEED_RE = re.compile(r"Speed[:\s]+(\d+(?:\.\d+)?)\s*([MmGgKk]bps?)", re.IGNORECASE)

# Interface description
_DESC_RE = re.compile(r"Description:\s+(.+)", re.IGNORECASE)


def _split_cidr(cidr: str) -> tuple[str, int]:
    """Split 'addr/prefix' into (addr, prefix_length). Returns ('', -1) on failure."""
    if not cidr or "/" not in cidr:
        return "", -1
    addr, _, prefix_str = cidr.partition("/")
    if not addr:
        return "", -1
    try:
        return addr, int(prefix_str)
    except ValueError:
        return addr, -1


def _speed_mbps(value: str, unit: str) -> float:
    """Convert speed value + unit string to Mbps float; -1.0 if unparseable."""
    try:
        v = float(value)
    except ValueError:
        return -1.0
    unit_lower = unit.lower()
    if unit_lower.startswith("g"):
        return v * 1000.0
    if unit_lower.startswith("k"):
        return v / 1000.0
    return v


def _parse_interfaces(raw: str) -> list[dict]:
    """
    Parse ``show interface`` output into a list of per-interface dicts.

    Each dict contains: name, is_up, is_enabled, description, mac_address,
    mtu, speed, ipv4 (list of CIDR strings), ipv6 (list of CIDR strings).
    Stanzas are separated by blank lines; each must start with an interface
    header line matching ``_INTF_HEADER_RE``.
    """
    interfaces = []
    stanzas = re.split(r"\n\s*\n", raw.strip())

    for stanza in stanzas:
        if not stanza.strip():
            continue
        header = _INTF_HEADER_RE.search(stanza)
        if not header:
            continue
        name = header.group(1)
        # group(2): full admin-state token — "up", "down", or "administratively down"
        # group(3): line protocol state — "up" or "down"
        admin_token = header.group(2).lower()
        admin_up = "administratively" not in admin_token  # False only for admin-shutdown ports
        proto_up = header.group(3).lower() == "up"

        mac_m = _MAC_RE.search(stanza)
        mac = mac_m.group(1) if mac_m else ""

        mtu_m = _MTU_RE.search(stanza)
        mtu = int(mtu_m.group(1)) if mtu_m else -1

        speed_m = _SPEED_RE.search(stanza)
        speed = _speed_mbps(speed_m.group(1), speed_m.group(2)) if speed_m else -1.0

        desc_m = _DESC_RE.search(stanza)
        description = desc_m.group(1).strip() if desc_m else ""

        ipv4 = [m.group(1) for m in _IPV4_RE.finditer(stanza)]
        ipv6 = [m.group(1) for m in _IPV6_RE.finditer(stanza)]

        interfaces.append(
            {
                "name": name,
                "is_up": proto_up,
                "is_enabled": admin_up,
                "description": description,
                "mac_address": mac,
                "mtu": mtu,
                "speed": speed,
                "ipv4": ipv4,
                "ipv6": ipv6,
            }
        )

    return interfaces


# ---------------------------------------------------------------------------
# Facts parsing helpers
# ---------------------------------------------------------------------------

_HOSTNAME_RE = re.compile(r"^(?:hostname|Hostname)\s*:\s*(\S+)", re.IGNORECASE | re.MULTILINE)
_VERSION_RE = re.compile(
    r"(?:Software\s+Version|System\s+Version|Firmware\s+Version|^Version)\s*:\s*(\S+)",
    re.IGNORECASE | re.MULTILINE,
)
_MODEL_RE = re.compile(r"^(?:Model|APIC\s+Model|Platform)\s*:\s*(\S+)", re.IGNORECASE | re.MULTILINE)
_SERIAL_RE = re.compile(r"^(?:Serial\s+Number|Serial)\s*:\s*(\S+)", re.IGNORECASE | re.MULTILINE)
_UPTIME_LINE_RE = re.compile(r"^(?:System\s+uptime|Uptime)\s*:\s*(.+)", re.IGNORECASE | re.MULTILINE)

# Tabular format emitted by some APIC versions for "show version":
#   Role         Pod  Node  Name    Version
#   -----------  ---  ----  ------  --------
#   controller   1    1     apic1   6.0(3f)
# group(1) = node name, group(2) = version string
_TABULAR_CTRL_RE = re.compile(
    r"^controller\s+\d+\s+\d+\s+(\S+)\s+(\S+)",
    re.IGNORECASE | re.MULTILINE,
)

_SERIAL_PLACEHOLDERS = frozenset({"none", "n/a", "na", "null", "unknown", "-"})


def _extract(text: str, pattern: re.Pattern) -> str:
    """Return first capture group from *pattern* match, or empty string."""
    m = pattern.search(text)
    return m.group(1).strip() if m else ""


class APICDriver(_napalm_base.NetworkDriver):
    """Cisco APIC NAPALM SSH driver (read-only subset for device-discovery)."""

    def __init__(self, hostname, username, password, timeout=60, optional_args=None):
        """Initialise driver state; no connection is opened yet."""
        self.hostname = hostname
        self.username = username
        self.password = password
        self.timeout = timeout
        self.device = None

        if optional_args is None:
            optional_args = {}
        self.netmiko_optional_args = netmiko_args(optional_args)
        self.netmiko_optional_args.setdefault("port", 22)
        self._intf_cache: list[dict] | None = None

    def open(self):
        """Open an SSH connection to the APIC via Netmiko."""
        self._intf_cache = None  # invalidate any stale cache from a previous session
        self.device = self._netmiko_open(
            _PLATFORM, netmiko_optional_args=self.netmiko_optional_args
        )

    def close(self):
        """Close the SSH connection."""
        self._netmiko_close()

    def is_alive(self):
        """Return whether the SSH channel is still active."""
        if self.device is None:
            return {"is_alive": False}
        try:
            self.device.write_channel(chr(0))
            return {"is_alive": self.device.remote_conn.transport.is_active()}
        except (OSError, EOFError, AttributeError):
            return {"is_alive": False}

    # -----------------------------------------------------------------------
    # Private helpers
    # -----------------------------------------------------------------------

    def _send(self, command: str) -> str:
        return self.device.send_command(command)

    def _parsed_interfaces(self) -> list[dict]:
        """Run ``show interface`` and return parsed interface list (cached per session)."""
        if getattr(self, "_intf_cache", None) is None:
            raw = self._send("show interface")
            self._intf_cache = _parse_interfaces(raw) if raw else []
        return self._intf_cache

    # -----------------------------------------------------------------------
    # NAPALM getters
    # -----------------------------------------------------------------------

    def get_facts(self) -> dict:
        """
        Return general device facts from ``show version`` and ``show interface``.

        Hostname, OS version, model, and serial number are regex-parsed from
        ``show version``.  Two output formats are handled:

        *Key-value* (older APIC firmware or per-controller context)::

            Hostname: apic1
            Software Version: 6.0(3f)
            Model: APIC-M2

        *Tabular* (some APIC versions, fabric-wide table)::

            Role        Pod  Node  Name   Version
            ----------  ---  ----  -----  -------
            controller  1    1     apic1  6.0(3f)

        Key-value patterns are tried first; the tabular fallback fills in
        any fields that remain ``"Unknown"`` after key-value parsing.
        The interface list is derived from ``show interface``.
        """
        hostname = self.hostname
        vendor = "Cisco"
        model = "Unknown"
        os_version = "Unknown"
        serial_number = "Unknown"
        uptime = 0.0

        ver_raw = self._send("show version")
        if ver_raw:
            # --- Key-value format (primary) ---
            hostname_val = _extract(ver_raw, _HOSTNAME_RE)
            if hostname_val:
                hostname = hostname_val

            version_val = _extract(ver_raw, _VERSION_RE)
            if version_val:
                os_version = version_val

            model_val = _extract(ver_raw, _MODEL_RE)
            if model_val:
                model = model_val

            serial_val = _extract(ver_raw, _SERIAL_RE)
            if serial_val and serial_val.lower() not in _SERIAL_PLACEHOLDERS:
                serial_number = serial_val

            uptime_line = _extract(ver_raw, _UPTIME_LINE_RE)
            if uptime_line:
                uptime = _parse_uptime(uptime_line)

            # --- Tabular format fallback ---
            # When key-value parsing yields nothing, try the controller row.
            if os_version == "Unknown":
                tab_m = _TABULAR_CTRL_RE.search(ver_raw)
                if tab_m:
                    if hostname == self.hostname:  # not yet overridden by key-value
                        hostname = tab_m.group(1)
                    os_version = tab_m.group(2)

        parsed_intfs = self._parsed_interfaces()
        interface_list = sorted({r["name"] for r in parsed_intfs if r.get("name")})

        return {
            "hostname": hostname,
            "vendor": vendor,
            "model": model,
            "os_version": os_version,
            "serial_number": serial_number,
            "uptime": uptime,
            "fqdn": "Unknown",
            "interface_list": interface_list,
        }

    def get_interfaces(self) -> dict:
        """
        Return interface details from ``show interface``.

        Each entry includes operational/admin state, description, MAC address,
        MTU, and speed.  IP information is returned by ``get_interfaces_ip``.
        """
        parsed = self._parsed_interfaces()
        result: dict = {}
        for row in parsed:
            name = row["name"]
            if not name or name in result:
                continue
            result[name] = {
                "is_up": row["is_up"],
                "is_enabled": row["is_enabled"],
                "description": row["description"],
                "last_flapped": -1.0,
                "mtu": row["mtu"],
                "speed": row["speed"],
                "mac_address": row["mac_address"],
            }
        return result

    def get_interfaces_ip(self) -> dict:
        """
        Return IP addresses per interface from ``show interface``.

        Both IPv4 and IPv6 addresses in CIDR notation are included.
        """
        parsed = self._parsed_interfaces()
        result: dict = {}
        for row in parsed:
            name = row["name"]
            if not name or name in result:
                continue
            intf_ips: dict = {}

            for cidr in row.get("ipv4", []):
                addr, prefix = _split_cidr(cidr)
                if addr and prefix >= 0:
                    intf_ips.setdefault("ipv4", {})[addr] = {"prefix_length": prefix}

            for cidr in row.get("ipv6", []):
                addr, prefix = _split_cidr(cidr)
                if addr and prefix >= 0:
                    intf_ips.setdefault("ipv6", {})[addr] = {"prefix_length": prefix}

            if intf_ips:
                result[name] = intf_ips

        return result

    def get_config(
        self,
        retrieve: str = "all",
        full: bool = False,
        sanitized: bool = False,
        format: str = "text",
    ) -> models.ConfigDict:
        """
        Return device configuration from ``show running-config``.

        APIC does not expose a separate candidate or startup config via SSH CLI;
        those keys are always returned as empty strings.
        """
        config: models.ConfigDict = {"running": "", "candidate": "", "startup": ""}
        retrieve = retrieve.lower()

        if retrieve in ("all", "running"):
            config["running"] = self._send("show running-config")

        if sanitized:
            for key in ("running", "candidate", "startup"):
                if config[key]:
                    config[key] = _sanitize_config(config[key])

        return config

    def get_vlans(self) -> dict:
        """
        Return VLAN information from ``fabric show vlan extended`` via ntc-templates.

        The ``cisco_apic`` ntc-templates platform parses per-node VLAN entries
        including VLAN ID, name, encapsulation, and port membership.  Entries
        are aggregated across all fabric nodes; duplicate VLAN IDs are merged
        with interfaces deduplicated.

        Returns an empty dict when the command produces no output or the
        ntc-templates parser returns no rows.
        """
        raw = self._send("fabric show vlan extended")
        if not raw:
            return {}

        try:
            rows = parse_output(
                platform=_PLATFORM,
                command="fabric show vlan extended",
                data=raw,
            )
        except Exception:
            logger.debug("Failed to parse 'fabric show vlan extended' output", exc_info=True)
            return {}

        # Build intermediate result with sets for O(1) port deduplication.
        # Each value is {"name": str, "ports": set[str]}.
        intermediate: dict = {}
        for row in rows:
            vlan_id = row.get("vlan_id", "").strip()
            if not vlan_id:
                continue

            raw_name = row.get("vlan_name")
            vlan_name = (raw_name[0].strip() if isinstance(raw_name, list) and raw_name else str(raw_name or "").strip()) or vlan_id

            entry = intermediate.setdefault(vlan_id, {"name": vlan_id, "ports": set()})

            # Update the name when a better (non-placeholder) name is found in a later row.
            if vlan_name and vlan_name != vlan_id:
                entry["name"] = vlan_name

            # VLAN_PORTS is a List in the template; each element may be a
            # comma-separated string — normalise to a flat deduplicated set.
            raw_ports: list = row.get("vlan_ports") or []
            for port_token in raw_ports:
                for port in re.split(r"[,\s]+", port_token):
                    port = port.strip().rstrip(",")
                    if port:
                        entry["ports"].add(port)

        return {
            vid: {"name": e["name"], "interfaces": sorted(e["ports"])}
            for vid, e in intermediate.items()
        }
