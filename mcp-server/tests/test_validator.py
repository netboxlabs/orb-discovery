import pytest

from orb_mcp.tools.validator import validate_policy

VALID_SNMP_DISCOVERY_YAML = """
policies:
  my_policy:
    config:
      timeout: 120
      snmp_timeout: 5
      snmp_probe_timeout: 1
      retries: 3
    scope:
      targets:
        - host: 192.168.1.1
      authentication:
        protocol_version: SNMPv2c
        community: public
"""

VALID_PROBE_TELEMETRY_YAML = """
policies:
  web_check:
    defaults:
      site: dc-01
    probes:
      - name: health
        type: http
        targets:
          - host: example.com
        http:
          scheme: HTTPS
          path: /health
          port: 443
          method: GET
"""

VALID_SNMP_TELEMETRY_YAML = """
policies:
  metrics_policy:
    config:
      metrics_interval: 60
      snmp_timeout: 5
      retries: 3
    scope:
      targets:
        - host: 192.168.1.1
      authentication:
        protocol_version: SNMPv2c
        community: public
"""


async def test_validate_snmp_discovery_valid():
    result = await validate_policy(VALID_SNMP_DISCOVERY_YAML, "snmp-discovery")
    assert result["valid"] is True
    assert "my_policy" in result["policies"]


async def test_validate_probe_telemetry_valid():
    result = await validate_policy(VALID_PROBE_TELEMETRY_YAML, "probe-telemetry")
    assert result["valid"] is True
    assert "web_check" in result["policies"]


async def test_validate_snmp_telemetry_valid():
    result = await validate_policy(VALID_SNMP_TELEMETRY_YAML, "snmp-telemetry")
    assert result["valid"] is True
    assert "metrics_policy" in result["policies"]


async def test_validate_missing_required_field():
    # Missing scope.targets
    bad_yaml = """
policies:
  bad_policy:
    config:
      timeout: 120
    scope:
      authentication:
        protocol_version: SNMPv2c
        community: public
"""
    result = await validate_policy(bad_yaml, "snmp-discovery")
    assert result["valid"] is False
    assert len(result["errors"]) > 0


async def test_validate_invalid_auth():
    # SNMPv2c missing community
    bad_yaml = """
policies:
  bad:
    scope:
      targets:
        - host: 1.2.3.4
      authentication:
        protocol_version: SNMPv2c
"""
    result = await validate_policy(bad_yaml, "snmp-discovery")
    assert result["valid"] is False


async def test_validate_malformed_yaml():
    result = await validate_policy("{: [broken yaml", "snmp-discovery")
    assert result["valid"] is False
    assert result["errors"][0]["field"] == "yaml"


async def test_validate_wrong_root_type():
    result = await validate_policy("- item1\n- item2\n", "snmp-discovery")
    assert result["valid"] is False


async def test_validate_probe_missing_type_conf():
    bad_yaml = """
policies:
  bad:
    probes:
      - name: p
        type: http
        targets:
          - host: example.com
"""
    result = await validate_policy(bad_yaml, "probe-telemetry")
    assert result["valid"] is False
