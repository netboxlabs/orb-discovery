#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Translate Unit Tests."""

import pytest

from device_discovery.interface import (
    match_interface_type,
    translate_interface,
    translate_interface_ips,
)
from device_discovery.policy.models import (
    Defaults,
    DeviceParameters,
    InterfacePattern,
    IpamParameters,
    ObjectParameters,
    Options,
    TenantParameters,
    VlanParameters,
)
from device_discovery.translate import (
    translate_data,
    translate_device,
    translate_vlan,
)


@pytest.fixture
def sample_device_info():
    """Sample device information for testing."""
    return {
        "hostname": "router1",
        "model": "ISR4451",
        "vendor": "Cisco",
        "serial_number": "123456789",
        "os_version": "v15.2",
        "platform": "ios",
        "interface_list": ["GigabitEthernet0/0", "GigabitEthernet0/0/1"],
    }


@pytest.fixture
def sample_interface_info():
    """Sample interface information for testing."""
    return {
        "GigabitEthernet0/0": {
            "is_enabled": True,
            "mtu": 1500,
            "mac_address": "00:1C:58:29:4A:71",
            "speed": 1000,
            "description": "Uplink Interface",
        },
        "GigabitEthernet0/0/1": {
            "is_enabled": True,
            "mtu": 1500,
            "mac_address": "00:1C:58:29:4A:72",
            "speed": 10000,
            "description": "Uplink Interface",
        },
    }


@pytest.fixture
def sample_interface_overflows_info():
    """Sample interface information for testing."""
    return {
        "GigabitEthernet0/0": {
            "is_enabled": True,
            "mtu": 150000000000,
            "mac_address": "00:1C:58:29:4A:71",
            "speed": 10000000000,
            "description": "Uplink Interface",
        }
    }


@pytest.fixture
def sample_interfaces_ip():
    """Sample interface IPs for testing."""
    return {"GigabitEthernet0/0/1": {"ipv4": {"192.0.2.1": {"prefix_length": 24}}}}


@pytest.fixture
def sample_defaults():
    """Sample defaults for testing."""
    return Defaults(
        site="New York",
        tags=["tag1", "tag2"],
        if_type="other",
        location="local",
        tenant="test",
        device=DeviceParameters(comments="testing", tags=["devtag"]),
        interface=ObjectParameters(description="testing", tags=["inttag"]),
        ipaddress=IpamParameters(description="ip test", tags=["iptag"]),
        prefix=IpamParameters(description="prefix test", tags=["prefixtag"]),
        vlan=VlanParameters(comments="test"),
    )


@pytest.fixture
def sample_tenant_parameters():
    """Sample tenant parameters for testing."""
    return TenantParameters(
        name="Tenant With Group",
        group="Tenant Group",
        description="Tenant description",
        comments="Tenant comments",
        tags=["tenant-tag"],
    )


@pytest.fixture
def sample_override_defaults(sample_defaults):
    """Sample defaults with device overrides."""
    sample_defaults.device.model = "Catalyst"
    return sample_defaults


def test_translate_device(sample_device_info, sample_defaults):
    """Ensure device translation is correct."""
    device = translate_device(sample_device_info, sample_defaults)
    assert device.name == "router1"
    assert device.device_type.model == "ISR4451"
    assert device.platform.name == "ios"
    assert device.serial == "123456789"
    assert device.site.name == "New York"
    assert device.comments == "testing"
    assert device.role.name == "undefined"
    assert device.location.name == "local"
    assert device.location.site.name == "New York"
    assert device.tenant.name == "test"
    assert len(device.tags) == 3


def test_translate_device_with_overrides(sample_device_info, sample_override_defaults):
    """Ensure device translation respects model overrides."""
    device = translate_device(sample_device_info, sample_override_defaults)
    assert device.device_type.model == "Catalyst"
    assert device.device_type.manufacturer.name == "Cisco"


def test_translate_device_with_tenant_parameters(
    sample_device_info, sample_defaults, sample_tenant_parameters
):
    """Ensure tenant parameters translate into Tenant entities."""
    sample_defaults.tenant = sample_tenant_parameters
    device = translate_device(sample_device_info, sample_defaults)

    assert device.tenant.name == "Tenant With Group"
    assert device.tenant.group.name == "Tenant Group"
    assert device.tenant.description == "Tenant description"
    assert device.tenant.comments == "Tenant comments"
    assert len(device.tenant.tags) == 1


def test_translate_device_serial_list(sample_device_info, sample_defaults):
    """Ensure device translation handles list serial numbers."""
    sample_device_info["serial_number"] = ["123456789", "987654321"]
    device = translate_device(sample_device_info, sample_defaults)
    assert device.serial == "123456789"
    sample_device_info["serial_number"] = []
    device = translate_device(sample_device_info, sample_defaults)
    assert device.serial == ""


def test_translate_interface(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Ensure interface translation is correct."""
    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )
    assert interface.device.name == "router1"
    assert interface.name == "GigabitEthernet0/0"
    assert interface.enabled is True
    assert interface.mtu == 1500
    assert interface.primary_mac_address.mac_address == "00:1C:58:29:4A:71"
    assert interface.speed == 1000000
    assert interface.description == "Uplink Interface"
    assert len(interface.tags) == 3


def test_translate_interface_with_overflow_data(
    sample_device_info, sample_interface_overflows_info, sample_defaults
):
    """Ensure interface translation is correct."""
    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_overflows_info["GigabitEthernet0/0"],
        sample_defaults,
    )
    assert interface.device.name == "router1"
    assert interface.name == "GigabitEthernet0/0"
    assert interface.enabled is True
    assert interface.mtu == 0
    assert interface.primary_mac_address.mac_address == "00:1C:58:29:4A:71"
    assert interface.speed == 0
    assert interface.description == "Uplink Interface"
    assert len(interface.tags) == 3


def test_translate_interface_ips(
    sample_device_info, sample_interface_info, sample_interfaces_ip, sample_defaults
):
    """Ensure interface IPs translation is correct."""
    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )
    ip_entities = list(
        translate_interface_ips(interface, sample_interfaces_ip, sample_defaults)
    )

    assert len(ip_entities) == 0

    interface = translate_interface(
        device,
        "GigabitEthernet0/0/1",
        sample_interface_info["GigabitEthernet0/0/1"],
        sample_defaults,
    )
    ip_entities = list(
        translate_interface_ips(interface, sample_interfaces_ip, sample_defaults)
    )

    assert len(ip_entities) == 2
    assert ip_entities[0].prefix.prefix == "192.0.2.0/24"
    assert ip_entities[1].ip_address.address == "192.0.2.1/24"
    assert ip_entities[0].prefix.description == "prefix test"
    assert ip_entities[1].ip_address.description == "ip test"
    assert len(ip_entities[0].prefix.tags) == 3
    assert len(ip_entities[1].ip_address.tags) == 3


def test_translate_interface_ips_with_tenant_parameters(
    sample_device_info,
    sample_interface_info,
    sample_interfaces_ip,
    sample_defaults,
    sample_tenant_parameters,
):
    """Ensure interface IP translation supports tenant parameters."""
    sample_defaults.ipaddress.tenant = sample_tenant_parameters
    sample_defaults.prefix.tenant = TenantParameters(
        name="Prefix Tenant", group="Prefix Group"
    )
    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0/1",
        sample_interface_info["GigabitEthernet0/0/1"],
        sample_defaults,
    )
    ip_entities = list(
        translate_interface_ips(interface, sample_interfaces_ip, sample_defaults)
    )

    assert len(ip_entities) == 2
    assert ip_entities[0].prefix.tenant.name == "Prefix Tenant"
    assert ip_entities[0].prefix.tenant.group.name == "Prefix Group"
    assert ip_entities[1].ip_address.tenant.name == "Tenant With Group"
    assert ip_entities[1].ip_address.tenant.group.name == "Tenant Group"


def test_translate_data(
    sample_device_info, sample_interface_info, sample_interfaces_ip, sample_defaults
):
    """Ensure data translation is correct."""
    data = {
        "device": sample_device_info,
        "interface": sample_interface_info,
        "interface_ip": sample_interfaces_ip,
        "driver": "ios",
    }
    entities = list(translate_data(data))
    assert len(entities) == 5
    assert entities[0].device.name == "router1"
    assert entities[0].device.site.name == "undefined"
    assert entities[0].device.platform.name == "IOS v15.2"
    assert entities[1].interface.name == "GigabitEthernet0/0"
    assert entities[2].interface.name == "GigabitEthernet0/0/1"
    assert entities[3].prefix.prefix == "192.0.2.0/24"
    assert entities[4].ip_address.address == "192.0.2.1/24"

    data["defaults"] = sample_defaults
    data["options"] = Options(platform_omit_version=True)

    entities = list(translate_data(data))
    assert entities[0].device.site.name == "New York"
    assert entities[0].device.platform.name == "ios"

    data["defaults"].role = "switch"
    data["defaults"].device.platform = "custom"
    data["options"].platform_omit_version = False

    entities = list(translate_data(data))
    assert entities[0].device.platform.name == "custom"
    assert entities[0].device.role.name == "switch"


def test_translate_data_truncates_platform(sample_device_info, sample_defaults):
    """Ensure overly long platform strings are truncated to 100 characters."""
    long_os_version = "v" * 150
    device_info = sample_device_info.copy()
    device_info["os_version"] = long_os_version
    data = {
        "device": device_info,
        "interface": {},
        "interface_ip": {},
        "driver": "ios",
        "defaults": sample_defaults,
    }

    entities = list(translate_data(data))

    assert len(entities) == 1
    assert entities[0].device.platform.name == long_os_version[:100]
    assert len(entities[0].device.platform.name) == 100


def test_translate_data_creates_missing_interface(sample_device_info, sample_defaults):
    """Ensure translate_data creates interfaces referenced only by IP data."""
    interfaces = {
        "GigabitEthernet0/0": {
            "is_enabled": True,
            "mtu": 1500,
            "mac_address": "00:1C:58:29:4A:71",
            "speed": 1000,
            "description": "Uplink Interface",
        }
    }
    interfaces_ip = {
        "Loopback0": {"ipv4": {"198.51.100.1": {"prefix_length": 32}}},
    }
    data = {
        "device": sample_device_info,
        "interface": interfaces,
        "interface_ip": interfaces_ip,
        "driver": "ios",
    }

    entities = list(translate_data(data))

    loopback_interface = next(
        entity.interface
        for entity in entities
        if entity.WhichOneof("entity") == "interface"
        and entity.interface.name == "Loopback0"
    )
    loopback_ip = next(
        entity.ip_address
        for entity in entities
        if entity.WhichOneof("entity") == "ip_address"
    )

    assert len(entities) == 5
    assert (
        sum(1 for entity in entities if entity.WhichOneof("entity") == "interface") == 2
    )
    assert loopback_interface.name == "Loopback0"
    assert loopback_ip.address == "198.51.100.1/32"
    assert loopback_ip.assigned_object_interface.name == "Loopback0"


def test_translate_data_creates_missing_subinterface_with_parent(
    sample_device_info, sample_defaults
):
    """Ensure translate_data creates subinterfaces and assigns parent relationships."""
    interfaces = {
        "ethernet-1/1": {
            "is_enabled": True,
            "mtu": 1500,
            "mac_address": "00:1C:58:29:4A:71",
            "speed": 1000,
            "description": "Parent Interface",
        },
        "ethernet-1/10": {
            "is_enabled": True,
            "mtu": 1500,
            "mac_address": "00:1C:58:29:4A:72",
            "speed": 1000,
            "description": "Interface",
        },
    }
    interfaces_ip = {
        "ethernet-1/1.0": {"ipv4": {"10.0.0.1": {"prefix_length": 30}}},
    }
    data = {
        "device": sample_device_info,
        "interface": interfaces,
        "interface_ip": interfaces_ip,
        "driver": "ios",
    }

    entities = list(translate_data(data))

    subinterface = next(
        entity.interface
        for entity in entities
        if entity.WhichOneof("entity") == "interface"
        and entity.interface.name == "ethernet-1/1.0"
    )
    parent_interface = next(
        entity.interface
        for entity in entities
        if entity.WhichOneof("entity") == "interface"
        and entity.interface.name == "ethernet-1/1"
    )
    ip_entity = next(
        entity.ip_address
        for entity in entities
        if entity.WhichOneof("entity") == "ip_address"
    )

    assert subinterface.parent.name == "ethernet-1/1"
    assert subinterface.parent.name == parent_interface.name
    assert subinterface.type == "virtual"
    assert parent_interface.type == "other"
    assert ip_entity.address == "10.0.0.1/30"
    assert ip_entity.assigned_object_interface.name == "ethernet-1/1.0"


def test_translate_data_handles_none_defaults_and_options(
    sample_device_info, sample_interface_info, sample_interfaces_ip
):
    """Ensure translation works when defaults and options are None."""
    data = {
        "device": sample_device_info,
        "interface": sample_interface_info,
        "interface_ip": sample_interfaces_ip,
        "driver": "ios",
        "defaults": None,
        "options": None,
    }

    entities = list(translate_data(data))

    assert len(entities) == 5
    assert entities[0].device.site.name == "undefined"
    assert entities[0].device.platform.name == "IOS v15.2"


def test_translate_vlan(sample_defaults):
    """Ensure VLAN translation is correct."""
    vid = "1"
    vlan_name = "Test  VLAN   "
    vlan = translate_vlan(vid, vlan_name, sample_defaults)

    assert vlan.vid == 1
    assert vlan.name == "Test VLAN"
    assert len(vlan.tags) == 2
    assert vlan.comments == "test"

    vid = "2"
    vlan_name = "Test - VLAN   "
    vlan = translate_vlan(vid, vlan_name, sample_defaults)
    assert vlan.vid == 2
    assert vlan.name == "Test - VLAN"

    vlan_name = "info-vlan "
    vlan = translate_vlan(vid, vlan_name, sample_defaults)
    assert vlan.vid == 2
    assert vlan.name == "info-vlan"

    vid = "NA"
    vlan = translate_vlan(vid, vlan_name, sample_defaults)
    assert vlan is None


def test_translate_vlan_with_none_id(sample_defaults):
    """Ensure translate_vlan returns None for invalid None VLAN IDs."""
    vlan = translate_vlan(None, "Invalid VLAN", sample_defaults)
    assert vlan is None


def test_translate_vlan_with_defaults(sample_defaults):
    """Ensure VLAN translation includes default values."""
    sample_defaults.vlan = VlanParameters(
        tags=["vlantag"],
        comments="Default VLAN comment",
        description="Default VLAN description",
        group="Default Group",
        tenant="Default Tenant",
        role="Default Role",
    )
    vid = "200"
    vlan_name = "Default VLAN"
    vlan = translate_vlan(vid, vlan_name, sample_defaults)

    assert vlan.vid == 200
    assert vlan.name == "Default VLAN"
    assert vlan.comments == "Default VLAN comment"
    assert vlan.description == "Default VLAN description"
    assert vlan.group.name == "Default Group"
    assert vlan.tenant.name == "Default Tenant"
    assert vlan.role.name == "Default Role"
    assert len(vlan.tags) == 3


def test_translate_vlan_with_tenant_parameters(
    sample_defaults, sample_tenant_parameters
):
    """Ensure VLAN translation supports tenant parameter objects."""
    sample_defaults.vlan = VlanParameters(
        tenant=sample_tenant_parameters, description="Tenant VLAN"
    )
    vlan = translate_vlan("201", "Tenant VLAN", sample_defaults)

    assert vlan.tenant.name == "Tenant With Group"
    assert vlan.tenant.group.name == "Tenant Group"
    assert vlan.description == "Tenant VLAN"


# Unit Tests for Pattern Matching Function


def test_match_interface_type_no_patterns():
    """Test pattern matching with no patterns configured."""
    result = match_interface_type("GigabitEthernet0/0", None)
    assert result is None

    result = match_interface_type("GigabitEthernet0/0", [])
    assert result is None


def test_match_interface_type_single_match():
    """Test pattern matching with single pattern."""
    patterns = [InterfacePattern(match="Gi.*", type="1000base-t")]
    result = match_interface_type("GigabitEthernet0/0", patterns)
    assert result == "1000base-t"

    result = match_interface_type("Gi0/0/0", patterns)
    assert result == "1000base-t"


def test_match_interface_type_no_match():
    """Test pattern matching when no pattern matches."""
    patterns = [InterfacePattern(match="Te.*", type="10gbase-x-sfpp")]
    result = match_interface_type("GigabitEthernet0/0", patterns)
    assert result is None


def test_match_interface_type_most_specific_wins():
    """Test that most specific (longest) match wins."""
    patterns = [
        InterfacePattern(match="^Gi", type="short-match"),
        InterfacePattern(match="^GigabitEthernet", type="long-match"),
        InterfacePattern(match="^Ten", type="10gbase-x-sfpp"),
    ]
    # "GigabitEthernet" matches longer (15 chars) than "Gi" (2 chars)
    result = match_interface_type("GigabitEthernet0/0", patterns)
    assert result == "long-match"

    # Only "Ten" matches at the start
    result = match_interface_type("TenGigabitEthernet1/0/1", patterns)
    assert result == "10gbase-x-sfpp"


def test_match_interface_type_first_pattern_wins_on_tie():
    """Test that first pattern wins when match lengths are equal."""
    patterns = [
        InterfacePattern(match="Ethernet.*", type="first-type"),
        InterfacePattern(match="Eth.*", type="second-type"),
    ]
    # Both could match "Ethernet0/0" but with different lengths
    # "Ethernet.*" is more specific
    result = match_interface_type("Ethernet0/0", patterns)
    assert result == "first-type"


def test_match_interface_type_multiple_patterns():
    """Test pattern matching with multiple different patterns."""
    patterns = [
        InterfacePattern(match="Gi.*", type="1000base-t"),
        InterfacePattern(match="Te.*", type="10gbase-x-sfpp"),
        InterfacePattern(match="Fa.*", type="100base-tx"),
        InterfacePattern(match="ethernet-.*", type="1000base-t"),
    ]
    assert match_interface_type("GigabitEthernet0/0", patterns) == "1000base-t"
    assert match_interface_type("TenGigabitEthernet1/0/1", patterns) == "10gbase-x-sfpp"
    assert match_interface_type("FastEthernet0/0", patterns) == "100base-tx"
    assert match_interface_type("ethernet-1/1", patterns) == "1000base-t"
    assert match_interface_type("Loopback0", patterns) is None


def test_match_interface_type_case_sensitive():
    """Test that pattern matching is case-sensitive by default."""
    patterns = [InterfacePattern(match="gi.*", type="1000base-t")]
    result = match_interface_type("GigabitEthernet0/0", patterns)
    assert result is None

    result = match_interface_type("gigabit0/0", patterns)
    assert result == "1000base-t"


def test_match_interface_type_anchored_patterns():
    """Test patterns with anchors (^ and $)."""
    patterns = [
        InterfacePattern(match="^Gi0/0/0$", type="specific-interface"),
        InterfacePattern(match="Gi.*", type="generic-type"),
    ]
    # Exact match should win (it's more specific - matches entire string)
    result = match_interface_type("Gi0/0/0", patterns)
    assert result == "specific-interface"

    # Other interfaces match generic pattern
    result = match_interface_type("Gi0/0/1", patterns)
    assert result == "generic-type"


# Integration Tests for Interface Translation with Patterns


def test_translate_interface_with_pattern_matching(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Test interface translation with pattern-based type assignment."""
    # Configure patterns
    sample_defaults.interface_patterns = [
        InterfacePattern(match="GigabitEthernet.*", type="1000base-t"),
        InterfacePattern(match="Te.*", type="10gbase-x-sfpp"),
    ]

    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )

    assert interface.type == "1000base-t"
    assert interface.name == "GigabitEthernet0/0"


def test_translate_interface_pattern_no_match_uses_default(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Test that default if_type is used when no pattern matches."""
    sample_defaults.interface_patterns = [
        InterfacePattern(match="Te.*", type="10gbase-x-sfpp"),
    ]
    sample_defaults.if_type = "other"

    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )

    # No pattern matches, should use default
    assert interface.type == "other"


def test_translate_interface_subinterface_ignores_patterns(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Test that subinterfaces always get 'virtual' type regardless of patterns."""
    sample_defaults.interface_patterns = [
        InterfacePattern(match=".*", type="should-not-match"),
    ]

    device = translate_device(sample_device_info, sample_defaults)

    # Create parent interface
    parent = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )

    # Create subinterface with parent
    subinterface = translate_interface(
        device,
        "GigabitEthernet0/0.100",
        {},
        sample_defaults,
        parent=parent,
    )

    # Subinterface should be "virtual" despite pattern matching everything
    assert subinterface.type == "virtual"
    assert parent.type == "should-not-match"  # Parent uses pattern


def test_translate_interface_most_specific_pattern_wins(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Test that most specific pattern match wins."""
    sample_defaults.interface_patterns = [
        InterfacePattern(match="Gi", type="generic"),
        InterfacePattern(match="GigabitEthernet0/0", type="specific"),
    ]

    device = translate_device(sample_device_info, sample_defaults)

    # Should match the more specific pattern (15 chars vs 2 chars)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0/1",
        sample_interface_info["GigabitEthernet0/0/1"],
        sample_defaults,
    )
    assert interface.type == "specific"

    # Should match only generic pattern
    interface2 = translate_interface(
        device,
        "GigabitEthernet1/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )
    assert interface2.type == "generic"


def test_translate_interface_backward_compatible(
    sample_device_info, sample_interface_info, sample_defaults
):
    """Test that interface translation works without patterns (backward compatibility)."""
    # No patterns configured (None)
    sample_defaults.interface_patterns = None
    sample_defaults.if_type = "other"

    device = translate_device(sample_device_info, sample_defaults)
    interface = translate_interface(
        device,
        "GigabitEthernet0/0",
        sample_interface_info["GigabitEthernet0/0"],
        sample_defaults,
    )

    # Should use default if_type
    assert interface.type == "other"


def test_translate_data_with_interface_patterns(
    sample_device_info, sample_interface_info, sample_interfaces_ip
):
    """Test full data translation with interface patterns."""
    defaults = Defaults(
        site="New York",
        if_type="other",
        interface_patterns=[
            InterfacePattern(match="GigabitEthernet.*", type="1000base-t"),
        ],
    )

    data = {
        "device": sample_device_info,
        "interface": sample_interface_info,
        "interface_ip": sample_interfaces_ip,
        "driver": "ios",
        "defaults": defaults,
    }

    entities = list(translate_data(data))

    # Find interface entities
    interface_entities = [
        e for e in entities if e.WhichOneof("entity") == "interface"
    ]

    # Both GigabitEthernet interfaces should match the pattern
    for interface_entity in interface_entities:
        if interface_entity.interface.name.startswith("GigabitEthernet"):
            assert interface_entity.interface.type == "1000base-t"


# Model Validation Tests


def test_interface_pattern_valid_regex():
    """Test InterfacePattern accepts valid regex patterns."""
    valid_patterns = [
        "Gi.*",
        "Te.*",
        "^GigabitEthernet0/0$",
        "ethernet-[0-9]+/[0-9]+",
        ".*",
    ]

    for pattern_str in valid_patterns:
        pattern = InterfacePattern(match=pattern_str, type="test-type")
        assert pattern.match == pattern_str


def test_interface_pattern_invalid_regex():
    """Test InterfacePattern rejects invalid regex patterns."""
    invalid_patterns = [
        "Gi[.*",  # Unclosed bracket
        "(?P<incomplete",  # Incomplete group
        "(?P<>invalid)",  # Empty group name
    ]

    for pattern_str in invalid_patterns:
        with pytest.raises(ValueError, match="Invalid regex pattern"):
            InterfacePattern(match=pattern_str, type="test-type")


def test_defaults_with_interface_patterns():
    """Test Defaults model with interface_patterns field."""
    defaults = Defaults(
        site="Test Site",
        if_type="other",
        interface_patterns=[
            InterfacePattern(match="Gi.*", type="1000base-t"),
            InterfacePattern(match="Te.*", type="10gbase-x-sfpp"),
        ],
    )

    assert len(defaults.interface_patterns) == 2
    assert defaults.interface_patterns[0].match == "Gi.*"
    assert defaults.interface_patterns[1].type == "10gbase-x-sfpp"


def test_defaults_without_interface_patterns():
    """Test Defaults model works without interface_patterns (backward compatibility)."""
    defaults = Defaults(site="Test Site", if_type="other")
    assert defaults.interface_patterns is None
