import pytest
import yaml

from orb_mcp.tools.generators import (
    generate_device_discovery_policy,
    generate_network_discovery_policy,
    generate_worker_policy,
)
from orb_mcp.tools.validator import validate_policy


# ── device_discovery ─────────────────────────────────────────────────────────

async def test_generate_device_discovery_basic():
    result = await generate_device_discovery_policy(
        policy_name="ios_discovery",
        scope=[
            {"hostname": "192.168.0.5", "username": "admin", "password": "${PASS}", "driver": "ios"}
        ],
        site="New York NY",
        role="switch",
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["ios_discovery"]
    assert policy["scope"][0]["hostname"] == "192.168.0.5"
    assert policy["scope"][0]["driver"] == "ios"
    assert policy["config"]["defaults"]["site"] == "New York NY"


async def test_generate_device_discovery_no_config():
    result = await generate_device_discovery_policy(
        policy_name="minimal",
        scope=[{"hostname": "10.0.0.1", "username": "admin", "password": "pass"}],
    )
    data = yaml.safe_load(result)
    # config block should still be present but without a defaults sub-key
    assert "scope" in data["policies"]["minimal"]


async def test_generate_device_discovery_with_options():
    result = await generate_device_discovery_policy(
        policy_name="with_opts",
        scope=[{"hostname": "192.168.1.1", "username": "u", "password": "p"}],
        options={"capture_running_config": True, "platform_omit_version": True},
    )
    data = yaml.safe_load(result)
    assert data["policies"]["with_opts"]["config"]["options"]["capture_running_config"] is True


async def test_generate_device_discovery_with_optional_args():
    result = await generate_device_discovery_policy(
        policy_name="jumphost",
        scope=[
            {
                "hostname": "10.0.1.10",
                "username": "cisco",
                "password": "${CISCO_PASS}",
                "driver": "ios",
                "optional_args": {"ssh_config_file": "/opt/orb/ssh-jumphost.conf"},
            }
        ],
    )
    data = yaml.safe_load(result)
    target = data["policies"]["jumphost"]["scope"][0]
    assert target["optional_args"]["ssh_config_file"] == "/opt/orb/ssh-jumphost.conf"


async def test_generate_device_discovery_with_schedule():
    result = await generate_device_discovery_policy(
        policy_name="scheduled",
        scope=[{"hostname": "10.0.0.1", "username": "u", "password": "p"}],
        schedule="*/10 * * * *",
    )
    data = yaml.safe_load(result)
    assert data["policies"]["scheduled"]["config"]["schedule"] == "*/10 * * * *"


async def test_generate_device_discovery_invalid_cron():
    with pytest.raises(Exception):
        await generate_device_discovery_policy(
            policy_name="bad",
            scope=[{"hostname": "10.0.0.1", "username": "u", "password": "p"}],
            schedule="not-a-cron",
        )


async def test_generate_device_discovery_missing_hostname():
    with pytest.raises(Exception):
        await generate_device_discovery_policy(
            policy_name="bad",
            scope=[{"username": "admin", "password": "pass"}],  # missing hostname
        )


async def test_validate_device_discovery_policy():
    result = await generate_device_discovery_policy(
        policy_name="validate_test",
        scope=[{"hostname": "192.168.0.5", "username": "admin", "password": "pass"}],
        site="dc-01",
    )
    validation = await validate_policy(result, "device-discovery")
    assert validation["valid"] is True
    assert "validate_test" in validation["policies"]


# ── network_discovery ─────────────────────────────────────────────────────────

async def test_generate_network_discovery_basic():
    result = await generate_network_discovery_policy(
        policy_name="net_scan",
        targets=["192.168.1.0/24", "10.0.0.1"],
        description="Network scan",
        tags=["net-discovery"],
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["net_scan"]
    assert "192.168.1.0/24" in policy["scope"]["targets"]
    assert policy["config"]["defaults"]["description"] == "Network scan"


async def test_generate_network_discovery_rootless():
    result = await generate_network_discovery_policy(
        policy_name="rootless",
        targets=["192.168.1.0/24"],
        scan_types=["connect"],
        skip_host=True,
        ports=[22, 80, 443],
        max_retries=3,
    )
    data = yaml.safe_load(result)
    scope = data["policies"]["rootless"]["scope"]
    assert scope["scan_types"] == ["connect"]
    assert scope["skip_host"] is True
    assert 22 in scope["ports"]


async def test_generate_network_discovery_invalid_scan_type():
    with pytest.raises(Exception):
        await generate_network_discovery_policy(
            policy_name="bad",
            targets=["192.168.1.1"],
            scan_types=["invalid_type"],
        )


async def test_generate_network_discovery_invalid_timing():
    with pytest.raises(Exception):
        await generate_network_discovery_policy(
            policy_name="bad",
            targets=["192.168.1.1"],
            timing=6,  # max is 5
        )


async def test_validate_network_discovery_policy():
    result = await generate_network_discovery_policy(
        policy_name="validate_net",
        targets=["192.168.1.0/24"],
        schedule="0 */2 * * *",
        timeout=5,
    )
    validation = await validate_policy(result, "network-discovery")
    assert validation["valid"] is True
    assert "validate_net" in validation["policies"]


# ── worker ────────────────────────────────────────────────────────────────────

async def test_generate_worker_basic():
    result = await generate_worker_policy(
        policy_name="custom_worker",
        package="my_worker",
        scope={"custom_val": "value"},
    )
    data = yaml.safe_load(result)
    assert "policies" in data
    policy = data["policies"]["custom_worker"]
    assert policy["config"]["package"] == "my_worker"
    assert policy["scope"]["custom_val"] == "value"


async def test_generate_worker_with_schedule():
    result = await generate_worker_policy(
        policy_name="scheduled_worker",
        package="nbl_custom",
        scope={"key": "val"},
        schedule="* * * * *",
    )
    data = yaml.safe_load(result)
    assert data["policies"]["scheduled_worker"]["config"]["schedule"] == "* * * * *"


async def test_generate_worker_with_extra_config():
    result = await generate_worker_policy(
        policy_name="worker_with_config",
        package="my_pkg",
        scope=[{"item": 1}],
        extra_config={"custom_config": "custom_value", "timeout": 30},
    )
    data = yaml.safe_load(result)
    config = data["policies"]["worker_with_config"]["config"]
    assert config["custom_config"] == "custom_value"
    assert config["timeout"] == 30


async def test_generate_worker_missing_package():
    with pytest.raises(Exception):
        await generate_worker_policy(
            policy_name="bad",
            package="",  # empty package
            scope={},
        )


async def test_generate_worker_list_scope():
    result = await generate_worker_policy(
        policy_name="list_scope",
        package="pkg",
        scope=[{"host": "1.2.3.4"}, {"host": "5.6.7.8"}],
    )
    data = yaml.safe_load(result)
    assert isinstance(data["policies"]["list_scope"]["scope"], list)


async def test_validate_worker_policy():
    result = await generate_worker_policy(
        policy_name="validate_worker",
        package="test_pkg",
        scope={"data": "value"},
        schedule="0 */2 * * *",
    )
    validation = await validate_policy(result, "worker")
    assert validation["valid"] is True
    assert "validate_worker" in validation["policies"]
