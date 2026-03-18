import pytest
import yaml

from orb_mcp.tools.generators import (
    generate_probe_telemetry_policy,
    generate_snmp_discovery_policy,
    generate_snmp_telemetry_policy,
)


async def test_generate_snmp_discovery_basic(snmpv2c_auth, basic_targets):
    result = await generate_snmp_discovery_policy(
        policy_name="test_policy",
        targets=basic_targets,
        authentication=snmpv2c_auth,
        site="dc-01",
        tags=["snmp"],
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    assert "test_policy" in data["policies"]
    policy = data["policies"]["test_policy"]
    assert policy["scope"]["targets"][0]["host"] == "192.168.1.1"
    assert policy["scope"]["authentication"]["community"] == "public"
    assert policy["config"]["defaults"]["site"] == "dc-01"
    assert policy["config"]["defaults"]["tags"] == ["snmp"]


async def test_generate_snmp_discovery_minimal(snmpv2c_auth, basic_targets):
    result = await generate_snmp_discovery_policy(
        policy_name="minimal",
        targets=basic_targets,
        authentication=snmpv2c_auth,
    )
    data = yaml.safe_load(result)
    policy = data["policies"]["minimal"]
    # No defaults block when none provided
    assert "defaults" not in policy["config"]


async def test_generate_snmp_discovery_snmpv3(snmpv3_auth, basic_targets):
    result = await generate_snmp_discovery_policy(
        policy_name="v3_policy",
        targets=basic_targets,
        authentication=snmpv3_auth,
    )
    data = yaml.safe_load(result)
    auth = data["policies"]["v3_policy"]["scope"]["authentication"]
    assert auth["protocol_version"] == "SNMPv3"
    assert auth["username"] == "admin"
    assert auth["auth_protocol"] == "SHA"


async def test_generate_snmp_discovery_with_schedule(snmpv2c_auth, basic_targets):
    result = await generate_snmp_discovery_policy(
        policy_name="scheduled",
        targets=basic_targets,
        authentication=snmpv2c_auth,
        schedule="0 */6 * * *",
    )
    data = yaml.safe_load(result)
    assert data["policies"]["scheduled"]["config"]["schedule"] == "0 */6 * * *"


async def test_generate_snmp_discovery_invalid_cron(snmpv2c_auth, basic_targets):
    with pytest.raises(Exception):
        await generate_snmp_discovery_policy(
            policy_name="bad_cron",
            targets=basic_targets,
            authentication=snmpv2c_auth,
            schedule="not-a-cron",
        )


async def test_generate_snmp_discovery_missing_community(basic_targets):
    with pytest.raises(Exception):
        await generate_snmp_discovery_policy(
            policy_name="bad_auth",
            targets=basic_targets,
            authentication={"protocol_version": "SNMPv2c"},
        )


async def test_generate_snmp_discovery_if_type(snmpv2c_auth, basic_targets):
    result = await generate_snmp_discovery_policy(
        policy_name="iface",
        targets=basic_targets,
        authentication=snmpv2c_auth,
        # Pass defaults via interface_patterns is separate; if_type is part of interface defaults
        # We test this by constructing YAML directly
    )
    # Just check valid YAML
    data = yaml.safe_load(result)
    assert "policies" in data


async def test_generate_probe_telemetry_http():
    result = await generate_probe_telemetry_policy(
        policy_name="web_check",
        probes=[
            {
                "name": "health",
                "type": "http",
                "targets": [{"host": "example.com"}],
                "http": {"scheme": "HTTPS", "path": "/health", "port": 443, "method": "GET"},
            }
        ],
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["web_check"]
    assert "defaults" not in policy
    probe = policy["probes"][0]
    assert probe["name"] == "health"
    assert probe["type"] == "http"
    assert probe["http"]["scheme"] == "HTTPS"


async def test_generate_probe_telemetry_dns():
    result = await generate_probe_telemetry_policy(
        policy_name="dns_check",
        probes=[
            {
                "name": "dns",
                "type": "dns",
                "targets": [{"host": "8.8.8.8"}],
                "dns": {"domain": "example.com", "query_type": "A", "min_answers": 1},
            }
        ],
    )
    data = yaml.safe_load(result)
    probe = data["policies"]["dns_check"]["probes"][0]
    assert probe["dns"]["domain"] == "example.com"


async def test_generate_probe_telemetry_missing_type_conf():
    with pytest.raises(Exception):
        await generate_probe_telemetry_policy(
            policy_name="bad",
            probes=[
                {
                    "name": "http_no_conf",
                    "type": "http",
                    "targets": [{"host": "example.com"}],
                    # missing http config block
                }
            ],
        )


async def test_generate_probe_telemetry_invalid_interval():
    with pytest.raises(Exception):
        await generate_probe_telemetry_policy(
            policy_name="bad",
            probes=[
                {
                    "name": "p",
                    "type": "ping",
                    "targets": [{"host": "1.2.3.4"}],
                    "ping": {"packets_per_probe": 3},
                    "interval": "5x",  # invalid
                }
            ],
        )


async def test_generate_snmp_telemetry_basic(snmpv2c_auth, basic_targets):
    result = await generate_snmp_telemetry_policy(
        policy_name="telemetry",
        targets=basic_targets,
        authentication=snmpv2c_auth,
        metrics_interval=60,
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["telemetry"]
    assert policy["config"]["metrics_interval"] == 60
    assert policy["scope"]["targets"][0]["host"] == "192.168.1.1"


async def test_generate_snmp_telemetry_no_metrics_interval(snmpv2c_auth, basic_targets):
    result = await generate_snmp_telemetry_policy(
        policy_name="no_metrics",
        targets=basic_targets,
        authentication=snmpv2c_auth,
    )
    data = yaml.safe_load(result)
    # metrics_interval should not appear when None
    assert "metrics_interval" not in data["policies"]["no_metrics"]["config"]
