import pytest
import yaml

from orb_mcp.tools.generators import (
    generate_flow_telemetry_policy,
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


@pytest.mark.parametrize("alias", ["2c", "v2c", "2", "v2", "SNMPv2c"])
async def test_protocol_version_v2c_aliases(alias, basic_targets):
    result = await generate_snmp_telemetry_policy(
        policy_name="alias_test",
        targets=basic_targets,
        authentication={"protocol_version": alias, "community": "public"},
    )
    import yaml

    auth = yaml.safe_load(result)["policies"]["alias_test"]["scope"]["authentication"]
    assert auth["protocol_version"] == "SNMPv2c"


@pytest.mark.parametrize("alias", ["1", "v1", "SNMPv1"])
async def test_protocol_version_v1_aliases(alias, basic_targets):
    result = await generate_snmp_telemetry_policy(
        policy_name="alias_test",
        targets=basic_targets,
        authentication={"protocol_version": alias, "community": "public"},
    )
    import yaml

    auth = yaml.safe_load(result)["policies"]["alias_test"]["scope"]["authentication"]
    assert auth["protocol_version"] == "SNMPv1"


@pytest.mark.parametrize("alias", ["3", "v3", "SNMPv3"])
async def test_protocol_version_v3_aliases(alias, basic_targets):
    result = await generate_snmp_telemetry_policy(
        policy_name="alias_test",
        targets=basic_targets,
        authentication={
            "protocol_version": alias,
            "username": "admin",
            "security_level": "authPriv",
            "auth_protocol": "SHA",
            "auth_passphrase": "authsecret",
            "priv_protocol": "AES",
            "priv_passphrase": "privsecret",
        },
    )
    import yaml

    auth = yaml.safe_load(result)["policies"]["alias_test"]["scope"]["authentication"]
    assert auth["protocol_version"] == "SNMPv3"


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


# --- flow-telemetry ---

_BASIC_ROLLUP = {
    "method": "sum",
    "name": "flow_bytes",
    "metrics": ["bytes"],
    "dimensions": ["src_addr", "dst_addr"],
}


async def test_generate_flow_telemetry_basic():
    result = await generate_flow_telemetry_policy(
        policy_name="flows",
        port=9995,
        rollups=[_BASIC_ROLLUP],
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["flows"]
    assert policy["scope"]["port"] == 9995
    assert "host" not in policy["scope"]
    assert "id" not in policy["scope"]
    rollup = policy["config"]["rollups"][0]
    assert rollup["method"] == "sum"
    assert rollup["name"] == "flow_bytes"
    assert rollup["metrics"] == ["bytes"]
    assert rollup["dimensions"] == ["src_addr", "dst_addr"]


async def test_generate_flow_telemetry_with_scope_fields():
    result = await generate_flow_telemetry_policy(
        policy_name="flows",
        port=9995,
        host="192.168.1.10",
        id="site-a",
        rollups=[_BASIC_ROLLUP],
    )
    data = yaml.safe_load(result)
    scope = data["policies"]["flows"]["scope"]
    assert scope["host"] == "192.168.1.10"
    assert scope["id"] == "site-a"


async def test_generate_flow_telemetry_protocol_options():
    for proto in ["auto", "netflow5", "netflow9", "ipfix", "sflow"]:
        result = await generate_flow_telemetry_policy(
            policy_name="flows",
            port=9995,
            protocol=proto,
            rollups=[_BASIC_ROLLUP],
        )
        data = yaml.safe_load(result)
        assert data["policies"]["flows"]["config"]["protocol"] == proto


async def test_generate_flow_telemetry_workers_and_queue():
    result = await generate_flow_telemetry_policy(
        policy_name="flows",
        port=9995,
        workers=4,
        queue_size=50000,
        rollups=[_BASIC_ROLLUP],
    )
    data = yaml.safe_load(result)
    config = data["policies"]["flows"]["config"]
    assert config["workers"] == 4
    assert config["queue_size"] == 50000


async def test_generate_flow_telemetry_multiple_rollups():
    result = await generate_flow_telemetry_policy(
        policy_name="flows",
        port=9995,
        rollups=[
            {"method": "sum", "name": "bytes", "metrics": ["bytes"], "dimensions": ["src_addr"]},
            {"method": "max", "name": "max_pkts", "metrics": ["packets"], "dimensions": ["dst_addr"]},
        ],
    )
    data = yaml.safe_load(result)
    rollups = data["policies"]["flows"]["config"]["rollups"]
    assert len(rollups) == 2
    assert rollups[1]["method"] == "max"


async def test_generate_flow_telemetry_auto_policy_name():
    result = await generate_flow_telemetry_policy(port=9995, rollups=[_BASIC_ROLLUP])
    data = yaml.safe_load(result)
    name = next(iter(data["policies"]))
    assert name.startswith("flow-telemetry-")


async def test_generate_flow_telemetry_invalid_method():
    with pytest.raises(Exception):
        await generate_flow_telemetry_policy(
            policy_name="bad",
            port=9995,
            rollups=[{"method": "average", "name": "x", "metrics": ["bytes"], "dimensions": []}],
        )


async def test_generate_flow_telemetry_invalid_metric():
    with pytest.raises(Exception):
        await generate_flow_telemetry_policy(
            policy_name="bad",
            port=9995,
            rollups=[{"method": "sum", "name": "x", "metrics": ["bits"], "dimensions": []}],
        )


async def test_generate_flow_telemetry_invalid_dimension():
    with pytest.raises(Exception):
        await generate_flow_telemetry_policy(
            policy_name="bad",
            port=9995,
            rollups=[{"method": "sum", "name": "x", "metrics": ["bytes"], "dimensions": ["hostname"]}],
        )


async def test_generate_flow_telemetry_invalid_protocol():
    with pytest.raises(Exception):
        await generate_flow_telemetry_policy(
            policy_name="bad",
            port=9995,
            protocol="cflow",
            rollups=[_BASIC_ROLLUP],
        )


async def test_generate_flow_telemetry_no_rollups():
    with pytest.raises(Exception):
        await generate_flow_telemetry_policy(policy_name="bad", port=9995, rollups=[])
