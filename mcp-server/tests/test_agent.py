import httpx
import pytest
import respx

from orb_mcp.tools.agent import delete_policy, get_agent_status, list_policies, submit_policy

SAMPLE_YAML = """
policies:
  test:
    config:
      timeout: 120
    scope:
      targets:
        - host: 192.168.1.1
      authentication:
        protocol_version: SNMPv2c
        community: public
"""


@respx.mock
async def test_submit_policy_success():
    respx.post("http://localhost:8070/api/v1/policies").mock(
        return_value=httpx.Response(201, json={"detail": "policies [test] were started"})
    )
    result = await submit_policy(SAMPLE_YAML, agent_type="snmp-discovery")
    assert result["status_code"] == 201
    assert "test" in result["response"]["detail"]


@respx.mock
async def test_submit_policy_conflict():
    respx.post("http://localhost:8070/api/v1/policies").mock(
        return_value=httpx.Response(409, json={"detail": "policy 'test' already exists"})
    )
    result = await submit_policy(SAMPLE_YAML, agent_type="snmp-discovery")
    assert result["status_code"] == 409


@respx.mock
async def test_submit_policy_custom_host_port():
    respx.post("http://10.0.0.1:9000/api/v1/policies").mock(
        return_value=httpx.Response(201, json={"detail": "policies [test] were started"})
    )
    result = await submit_policy(
        SAMPLE_YAML, agent_type="snmp-discovery", agent_host="10.0.0.1", agent_port=9000
    )
    assert result["status_code"] == 201


@respx.mock
async def test_list_policies():
    respx.get("http://localhost:8070/api/v1/status").mock(
        return_value=httpx.Response(
            200,
            json={
                "start_time": "2024-01-01T00:00:00Z",
                "up_time_seconds": 3600,
                "version": "1.0.0",
            },
        )
    )
    result = await list_policies(agent_type="snmp-discovery")
    assert result["status_code"] == 200
    assert "up_time_seconds" in result["response"]


@respx.mock
async def test_delete_policy_success():
    respx.delete("http://localhost:8070/api/v1/policies/test").mock(
        return_value=httpx.Response(200, json={"detail": "policy 'test' was stopped"})
    )
    result = await delete_policy("test", agent_type="snmp-discovery")
    assert result["status_code"] == 200


@respx.mock
async def test_delete_policy_not_found():
    respx.delete("http://localhost:8070/api/v1/policies/missing").mock(
        return_value=httpx.Response(404, json={"detail": "policy 'missing' not found"})
    )
    result = await delete_policy("missing", agent_type="snmp-discovery")
    assert result["status_code"] == 404


@respx.mock
async def test_get_agent_status():
    respx.get("http://localhost:8075/api/v1/status").mock(
        return_value=httpx.Response(200, json={"version": "1.0.0", "up_time_seconds": 100})
    )
    result = await get_agent_status(agent_type="probe-telemetry")
    assert result["status_code"] == 200
    assert result["response"]["version"] == "1.0.0"


async def test_connect_error():
    # No respx mock — real connection attempt to localhost will fail
    result = await submit_policy(SAMPLE_YAML, agent_type="snmp-discovery", agent_port=19999)
    assert result["status_code"] is None
    assert "error" in result
