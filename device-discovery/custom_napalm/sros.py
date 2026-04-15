# Copyright 2026 NetBox Labs Inc
# Based on napalm-sros (Apache-2.0): https://github.com/napalm-automation-community/napalm-sros
"""
Custom Nokia SR-OS NETCONF NAPALM driver.

Implements only the methods used by device-discovery:
  get_facts, get_interfaces, get_interfaces_ip, get_config, get_vlans.

Uses ncclient for NETCONF/YANG transport and lxml for structured XML parsing
against Nokia's YANG models (urn:nokia.com:sros:ns:yang:sr:*).

Modernisations over the community napalm-sros driver:
  - logging.exception() replaces print() + traceback
  - lxml.etree used directly (not ncclient.xml_ shims)
  - Inline NETCONF filters (no separate nc_filters module)
  - Multiple IPv4 secondary addresses handled via loop
  - No config-write methods (read-only subset)
  - No extra paramiko SSH channel
  - Type hints throughout
"""

import logging
import re
from datetime import datetime, timezone

import napalm.base as _napalm_base
from lxml import etree
from napalm.base import models
from napalm.base.helpers import convert

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Nokia YANG namespaces
# ---------------------------------------------------------------------------

_NS_STATE = "urn:nokia.com:sros:ns:yang:sr:state"
_NS_CONF = "urn:nokia.com:sros:ns:yang:sr:conf"
_NS_NC = "urn:ietf:params:xml:ns:netconf:base:1.0"

_NSMAP: dict[str, str] = {
    "state_ns": _NS_STATE,
    "configure_ns": _NS_CONF,
}

# ---------------------------------------------------------------------------
# NETCONF filters
# ---------------------------------------------------------------------------

_FILTER_FACTS = f"""
<filter xmlns="{_NS_NC}">
    <state xmlns="{_NS_STATE}">
        <chassis>
            <hardware-data>
                <serial-number/>
            </hardware-data>
        </chassis>
        <system>
            <oper-name/>
            <up-time/>
            <platform/>
            <version>
                <version-number/>
            </version>
        </system>
        <router>
            <interface>
                <interface-name/>
            </interface>
        </router>
    </state>
</filter>
"""

# R19 uses <if-oper-status> instead of <oper-state> for router interfaces.
# The tag is injected at runtime via .format().
_FILTER_INTERFACES_TMPL = """\
<filter xmlns="{ns_nc}">
    <state xmlns="{ns_state}">
        <port>
            <port-id/>
            <oper-state/>
            <hardware-mac-address/>
            <ethernet>
                <oper-speed/>
            </ethernet>
        </port>
        <router>
            <interface>
                <interface-name/>
                <oper-ip-mtu/>
                {oper_state_tag}
                <last-oper-change/>
            </interface>
        </router>
        <chassis>
            <hardware-data>
                <base-mac-address/>
            </hardware-data>
        </chassis>
    </state>
    <configure xmlns="{ns_conf}">
        <port>
            <port-id/>
            <description/>
            <admin-state/>
            <ethernet>
                <mtu/>
            </ethernet>
        </port>
        <router>
            <interface>
                <interface-name/>
                <admin-state/>
                <description/>
                <mac/>
                <port/>
                <loopback/>
            </interface>
        </router>
    </configure>
</filter>"""

_FILTER_INTERFACES_IP = f"""
<filter xmlns="{_NS_NC}">
    <configure xmlns="{_NS_CONF}">
        <router>
            <interface>
                <interface-name/>
                <ipv4>
                    <primary>
                        <address/>
                        <prefix-length/>
                    </primary>
                    <secondary>
                        <address/>
                        <prefix-length/>
                    </secondary>
                </ipv4>
                <ipv6>
                    <address>
                        <ipv6-address/>
                        <prefix-length/>
                    </address>
                </ipv6>
            </interface>
        </router>
        <service>
            <vprn>
                <service-name/>
                <interface>
                    <interface-name/>
                    <ipv4>
                        <primary>
                            <address/>
                            <prefix-length/>
                        </primary>
                        <secondary>
                            <address/>
                            <prefix-length/>
                        </secondary>
                    </ipv4>
                    <ipv6>
                        <address>
                            <ipv6-address/>
                            <prefix-length/>
                        </address>
                    </ipv6>
                </interface>
            </vprn>
        </service>
    </configure>
</filter>
"""

# ---------------------------------------------------------------------------
# Config sanitization — Nokia SR-OS YANG XML sensitive element content
# ---------------------------------------------------------------------------
# Pattern: (<tag>)[^<]*(</tag>)  →  \1<redacted>\2
# Covers both plain-text and encrypted values inside XML elements.

_AUTH_KEY_RE = re.compile(r"(<authentication-key>)[^<]*(</authentication-key>)", re.IGNORECASE)
_HMAC_MD5_RE = re.compile(r"(<hmac-md5-key>)[^<]*(</hmac-md5-key>)", re.IGNORECASE)
_DES_KEY_RE = re.compile(r"(<des-key>)[^<]*(</des-key>)", re.IGNORECASE)
_AES_KEY_RE = re.compile(r"(<aes-key>)[^<]*(</aes-key>)", re.IGNORECASE)
_PASSWORD_RE = re.compile(r"(<password>)[^<]*(</password>)", re.IGNORECASE)
_SECRET_RE = re.compile(r"(<secret>)[^<]*(</secret>)", re.IGNORECASE)
_COMMUNITY_STR_RE = re.compile(r"(<community-string>)[^<]*(</community-string>)", re.IGNORECASE)
_PRIVATE_KEY_RE = re.compile(r"(<private-key>)[^<]*(</private-key>)", re.IGNORECASE)
_PSK_RE = re.compile(r"(<pre-shared-key>)[^<]*(</pre-shared-key>)", re.IGNORECASE)


def _sanitize_config(text: str) -> str:
    """Redact Nokia SR-OS YANG XML secret values, replacing content with <redacted>."""
    text = _AUTH_KEY_RE.sub(r"\1<redacted>\2", text)
    text = _HMAC_MD5_RE.sub(r"\1<redacted>\2", text)
    text = _DES_KEY_RE.sub(r"\1<redacted>\2", text)
    text = _AES_KEY_RE.sub(r"\1<redacted>\2", text)
    text = _PASSWORD_RE.sub(r"\1<redacted>\2", text)
    text = _SECRET_RE.sub(r"\1<redacted>\2", text)
    text = _COMMUNITY_STR_RE.sub(r"\1<redacted>\2", text)
    text = _PRIVATE_KEY_RE.sub(r"\1<redacted>\2", text)
    text = _PSK_RE.sub(r"\1<redacted>\2", text)
    return text


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _parse_xml(data_xml: str | bytes) -> etree._Element:
    """Parse a NETCONF data_xml payload into an lxml Element."""
    if isinstance(data_xml, str):
        data_xml = data_xml.encode("utf-8")
    return etree.fromstring(data_xml)


def _find_txt(xml_tree: etree._Element, xpath: str, default: str = "") -> str:
    """Extract the text of the first XPath match; return *default* on miss."""
    try:
        results = xml_tree.xpath(xpath, namespaces=_NSMAP)
        if results:
            node = results[0]
            if hasattr(node, "text") and node.text is not None:
                return node.text.strip()
            if isinstance(node, str):
                return node.strip()
    except Exception:
        logger.debug("XPath '%s' returned nothing", xpath)
    return default


def _xpath_one(xml_tree: etree._Element, xpath: str) -> etree._Element | None:
    """Return the first XPath match as an Element, or None."""
    try:
        results = xml_tree.xpath(xpath, namespaces=_NSMAP)
        return results[0] if results else None
    except Exception:
        return None


def _port_ref_from_cfg(cfg_block: etree._Element | None) -> str:
    """Extract the bare port-id from a configure/router/interface block (strips .1q tag)."""
    if cfg_block is None:
        return ""
    port_ref = _find_txt(cfg_block, "configure_ns:port")
    return port_ref.split(":")[0] if ":" in port_ref else port_ref


def _resolve_if_mac(
    result: etree._Element,
    cfg_block: etree._Element | None,
    if_name: str,
    chassis_mac: str,
) -> str:
    """Resolve MAC for a logical router interface: cfg override → port MAC → chassis MAC."""
    if cfg_block is not None:
        cfg_mac = _find_txt(cfg_block, "configure_ns:mac")
        if cfg_mac:
            return cfg_mac
    if_port = _port_ref_from_cfg(cfg_block)
    if if_port:
        port_state = _xpath_one(result, f'state_ns:state/state_ns:port[state_ns:port-id="{if_port}"]')
        if port_state is not None:
            port_mac = _find_txt(port_state, "state_ns:hardware-mac-address")
            if port_mac:
                return port_mac
    if if_name == "system":
        return chassis_mac
    return ""


def _resolve_if_speed(result: etree._Element, cfg_block: etree._Element | None) -> float:
    """Resolve speed for a logical router interface from its associated port."""
    if_port = _port_ref_from_cfg(cfg_block)
    if not if_port:
        return -1.0
    port_state = _xpath_one(result, f'state_ns:state/state_ns:port[state_ns:port-id="{if_port}"]')
    if port_state is None:
        return -1.0
    return convert(float, _find_txt(port_state, "state_ns:ethernet/state_ns:oper-speed"), default=-1.0)


def _parse_last_flapped(flap_str: str) -> float:
    """Convert Nokia ISO 8601 last-oper-change timestamp to a UTC epoch float."""
    if not flap_str:
        return -1.0
    try:
        dt = datetime.strptime(flap_str, "%Y-%m-%dT%H:%M:%S.%fZ").replace(tzinfo=timezone.utc)
        return dt.timestamp()
    except ValueError:
        logger.debug("Cannot parse last-oper-change: %s", flap_str)
        return -1.0


class SROSDriver(_napalm_base.NetworkDriver):
    """Nokia SR-OS NAPALM driver using NETCONF/YANG (read-only subset for device-discovery)."""

    def __init__(self, hostname, username, password, timeout=60, optional_args=None):
        """Initialise driver — no network connection is made here."""
        self.hostname = hostname
        self.username = username
        self.password = password
        self.timeout = timeout
        self.conn = None
        if optional_args is None:
            optional_args = {}
        self.port: int = int(optional_args.get("port", 830))
        # R19 = True for SR-OS < 21.x (older YANG revision 2016-07-06)
        self.R19: bool = False

    def open(self) -> None:
        """Open a NETCONF session to the device."""
        from ncclient import manager  # deferred: not needed until connection time

        self.conn = manager.connect(
            host=self.hostname,
            port=self.port,
            username=self.username,
            password=self.password,
            hostkey_verify=False,  # Nokia devices rarely present verifiable host keys
            timeout=self.timeout,
        )
        # Detect legacy R19 YANG revision (SR-OS < 21.x firmware)
        _rev_re = re.compile(r".*&revision=(.*)")
        revisions = [
            m.group(1)
            for c in self.conn.server_capabilities
            if "nokia-state" in c and (m := _rev_re.match(c))
        ]
        self.R19 = "2016-07-06" in revisions

    def close(self) -> None:
        """Close the NETCONF session."""
        if self.conn is not None:
            self.conn.close_session()
            self.conn = None

    def is_alive(self) -> dict:
        """Return connection liveness."""
        return {"is_alive": self.conn is not None}

    # -----------------------------------------------------------------------
    # NAPALM getters
    # -----------------------------------------------------------------------

    def get_facts(self) -> dict:
        """Return general device facts via NETCONF state tree."""
        try:
            result = _parse_xml(self.conn.get(filter=_FILTER_FACTS).data_xml)
            hostname = _find_txt(result, "state_ns:state/state_ns:system/state_ns:oper-name")
            uptime_ms_str = _find_txt(result, "state_ns:state/state_ns:system/state_ns:up-time")
            # Nokia YANG up-time is milliseconds (integer string)
            uptime = convert(float, uptime_ms_str, default=0.0) / 1000.0 if uptime_ms_str else 0.0
            interface_list = [
                el.text.strip()
                for el in result.xpath(
                    "state_ns:state/state_ns:router/state_ns:interface/state_ns:interface-name",
                    namespaces=_NSMAP,
                )
                if el.text
            ]
            return {
                "hostname": hostname,
                "vendor": "Nokia",
                "model": _find_txt(result, "state_ns:state/state_ns:system/state_ns:platform"),
                "os_version": _find_txt(
                    result,
                    "state_ns:state/state_ns:system/state_ns:version/state_ns:version-number",
                ),
                "serial_number": _find_txt(
                    result,
                    "state_ns:state/state_ns:chassis/state_ns:hardware-data/state_ns:serial-number",
                ),
                "uptime": uptime,
                "fqdn": hostname,
                "interface_list": interface_list,
            }
        except Exception:
            logger.exception("get_facts failed")
            return {}

    def get_interfaces(self) -> dict:
        """
        Return interface details keyed by name (physical ports + logical router interfaces).

        Note: covers the Base router VRF and physical ports only. L3 interfaces inside VPRN
        service instances are accessible via get_interfaces_ip() but are not enumerated here,
        as they lack port-state (speed/MAC) data in the YANG model.
        """
        try:
            oper_state_tag = "<if-oper-status/>" if self.R19 else "<oper-state/>"
            nc_filter = _FILTER_INTERFACES_TMPL.format(
                ns_nc=_NS_NC,
                ns_state=_NS_STATE,
                ns_conf=_NS_CONF,
                oper_state_tag=oper_state_tag,
            )
            result = _parse_xml(
                self.conn.get(filter=nc_filter, with_defaults="report-all").data_xml
            )
            interfaces: dict = {}

            # --- Physical ports ---
            for port in result.xpath("state_ns:state/state_ns:port", namespaces=_NSMAP):
                port_id = _find_txt(port, "state_ns:port-id")
                if not port_id:
                    continue
                cfg_block = _xpath_one(
                    result,
                    f'configure_ns:configure/configure_ns:port[configure_ns:port-id="{port_id}"]',
                )
                interfaces[port_id] = {
                    "is_up": _find_txt(port, "state_ns:oper-state") == "up",
                    "is_enabled": (
                        _find_txt(cfg_block, "configure_ns:admin-state") == "enable"
                        if cfg_block is not None
                        else False
                    ),
                    "description": (
                        _find_txt(cfg_block, "configure_ns:description")
                        if cfg_block is not None
                        else ""
                    ),
                    "last_flapped": -1.0,
                    "speed": convert(
                        float,
                        _find_txt(port, "state_ns:ethernet/state_ns:oper-speed"),
                        default=0.0,
                    ),
                    "mtu": convert(
                        int,
                        _find_txt(cfg_block, "configure_ns:ethernet/configure_ns:mtu")
                        if cfg_block is not None
                        else "",
                        default=0,
                    ),
                    "mac_address": _find_txt(port, "state_ns:hardware-mac-address"),
                }

            # --- Logical router interfaces ---
            chassis_mac = _find_txt(
                result,
                "state_ns:state/state_ns:chassis/state_ns:hardware-data/state_ns:base-mac-address",
            )
            oper_state_xpath = "state_ns:if-oper-status" if self.R19 else "state_ns:oper-state"

            for if_state in result.xpath(
                "state_ns:state/state_ns:router/state_ns:interface", namespaces=_NSMAP
            ):
                if_name = _find_txt(if_state, "state_ns:interface-name")
                if not if_name:
                    continue
                cfg_block = _xpath_one(
                    result,
                    f"configure_ns:configure/configure_ns:router"
                    f'/configure_ns:interface[configure_ns:interface-name="{if_name}"]',
                )
                interfaces[if_name] = {
                    "is_up": _find_txt(if_state, oper_state_xpath) == "up",
                    "is_enabled": _find_txt(cfg_block, "configure_ns:admin-state") == "enable"
                    if cfg_block is not None
                    else False,
                    "description": _find_txt(cfg_block, "configure_ns:description")
                    if cfg_block is not None
                    else "",
                    "last_flapped": _parse_last_flapped(
                        _find_txt(if_state, "state_ns:last-oper-change")
                    ),
                    "speed": _resolve_if_speed(result, cfg_block),
                    "mtu": convert(int, _find_txt(if_state, "state_ns:oper-ip-mtu"), default=0),
                    "mac_address": _resolve_if_mac(result, cfg_block, if_name, chassis_mac),
                }

            return interfaces
        except Exception:
            logger.exception("get_interfaces failed")
            return {}

    def get_interfaces_ip(self) -> dict:
        """Return all configured IP addresses per interface (configure tree)."""
        try:
            result = _parse_xml(
                self.conn.get(filter=_FILTER_INTERFACES_IP, with_defaults="report-all").data_xml
            )
            interfaces_ip: dict = {}

            xpath_filter = (
                "configure_ns:configure/configure_ns:router/configure_ns:interface"
                " | configure_ns:configure/configure_ns:service"
                "/configure_ns:vprn/configure_ns:interface"
            )
            for interface in result.xpath(xpath_filter, namespaces=_NSMAP):
                if_name = _find_txt(interface, "configure_ns:interface-name")
                if not if_name:
                    continue

                entry: dict = {}

                # IPv4 primary
                ipv4_primary = _find_txt(
                    interface,
                    "configure_ns:ipv4/configure_ns:primary/configure_ns:address",
                )
                if ipv4_primary:
                    entry.setdefault("ipv4", {})[ipv4_primary] = {
                        "prefix_length": convert(
                            int,
                            _find_txt(
                                interface,
                                "configure_ns:ipv4/configure_ns:primary/configure_ns:prefix-length",
                            ),
                            default=0,
                        )
                    }

                # IPv4 secondaries (can be multiple)
                for secondary in interface.xpath(
                    "configure_ns:ipv4/configure_ns:secondary", namespaces=_NSMAP
                ):
                    sec_addr = _find_txt(secondary, "configure_ns:address")
                    if sec_addr:
                        entry.setdefault("ipv4", {})[sec_addr] = {
                            "prefix_length": convert(
                                int,
                                _find_txt(secondary, "configure_ns:prefix-length"),
                                default=0,
                            )
                        }

                # IPv6 addresses (can be multiple)
                for ipv6_entry in interface.xpath(
                    "configure_ns:ipv6/configure_ns:address", namespaces=_NSMAP
                ):
                    ipv6_addr = _find_txt(ipv6_entry, "configure_ns:ipv6-address")
                    if ipv6_addr:
                        entry.setdefault("ipv6", {})[ipv6_addr] = {
                            "prefix_length": convert(
                                int,
                                _find_txt(ipv6_entry, "configure_ns:prefix-length"),
                                default=0,
                            )
                        }

                if entry:
                    interfaces_ip[if_name] = entry

            return interfaces_ip
        except Exception:
            logger.exception("get_interfaces_ip failed")
            return {}

    def get_config(
        self,
        retrieve: str = "all",
        full: bool = False,
        sanitized: bool = False,
        format: str = "text",
    ) -> models.ConfigDict:
        """Return Nokia SR-OS running configuration as NETCONF XML."""
        config: models.ConfigDict = {"running": "", "candidate": "", "startup": ""}
        try:
            if retrieve in ("all", "running"):
                data = _parse_xml(self.conn.get_config(source="running").data_xml)
                cfg_nodes = data.xpath("configure_ns:configure", namespaces=_NSMAP)
                if cfg_nodes:
                    config["running"] = etree.tostring(
                        cfg_nodes[0], pretty_print=True
                    ).decode("utf-8")
        except Exception:
            logger.exception("get_config failed")
        if sanitized:
            for key in ("running", "candidate", "startup"):
                if config[key]:
                    config[key] = _sanitize_config(config[key])
        return config

    def get_vlans(self) -> dict:
        """Nokia SR-OS uses a service-based architecture — no traditional VLAN table."""
        return {}
