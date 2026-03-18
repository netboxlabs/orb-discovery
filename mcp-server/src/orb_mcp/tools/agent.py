from __future__ import annotations

from typing import Any, Literal

import httpx

from orb_mcp.client import AgentClient

AgentType = Literal[
    "snmp-discovery",
    "snmp-telemetry",
    "probe-telemetry",
    "device-discovery",
    "network-discovery",
    "worker",
]

_DEFAULT_PORTS: dict[str, int] = {
    "snmp-discovery": 8070,
    "snmp-telemetry": 8074,
    "probe-telemetry": 8075,
    "device-discovery": 8072,
    "network-discovery": 8073,
    "worker": 8071,
}


def _base_url(agent_type: AgentType, host: str, port: int | None) -> str:
    effective_port = port if port is not None else _DEFAULT_PORTS[agent_type]
    return f"http://{host}:{effective_port}"


def _response_dict(response: httpx.Response) -> dict[str, Any]:
    try:
        body = response.json()
    except Exception:
        body = response.text
    return {"status_code": response.status_code, "response": body}


async def submit_policy(
    yaml_content: str,
    agent_type: AgentType,
    agent_host: str = "localhost",
    agent_port: int | None = None,
) -> dict[str, Any]:
    """
    Submit a policy YAML document to a running orb-discovery agent (POST /api/v1/policies).

    `yaml_content`: the complete YAML policy document (e.g. output from generate_* tools).
    `agent_type`: 'snmp-discovery', 'snmp-telemetry', or 'probe-telemetry'.
    `agent_host`: hostname or IP of the running agent (default: 'localhost').
    `agent_port`: port number (defaults: snmp-discovery=8070, snmp-telemetry=8074, probe-telemetry=8075).

    Returns the HTTP status code and the agent's JSON response.
    Status 201 = policy started. Status 409 = policy name already exists (delete it first).
    """
    try:
        async with AgentClient(_base_url(agent_type, agent_host, agent_port)) as client:
            response = await client.post_policy(yaml_content)
            return _response_dict(response)
    except httpx.ConnectError as e:
        return {"status_code": None, "error": f"Could not connect to {agent_type} at {agent_host}: {e}"}


async def list_policies(
    agent_type: AgentType,
    agent_host: str = "localhost",
    agent_port: int | None = None,
) -> dict[str, Any]:
    """
    Retrieve current policy statuses from a running orb-discovery agent (GET /api/v1/status).

    `agent_type`: 'snmp-discovery', 'snmp-telemetry', or 'probe-telemetry'.
    `agent_host`: hostname or IP of the running agent (default: 'localhost').
    `agent_port`: port number (defaults: snmp-discovery=8070, snmp-telemetry=8074, probe-telemetry=8075).

    Returns the agent's status response including all active policies and their run history.
    """
    try:
        async with AgentClient(_base_url(agent_type, agent_host, agent_port)) as client:
            response = await client.get_status()
            return _response_dict(response)
    except httpx.ConnectError as e:
        return {"status_code": None, "error": f"Could not connect to {agent_type} at {agent_host}: {e}"}


async def delete_policy(
    policy_name: str,
    agent_type: AgentType,
    agent_host: str = "localhost",
    agent_port: int | None = None,
) -> dict[str, Any]:
    """
    Delete a named policy from a running orb-discovery agent (DELETE /api/v1/policies/:name).

    `policy_name`: the name of the policy to delete (as it was submitted).
    `agent_type`: 'snmp-discovery', 'snmp-telemetry', or 'probe-telemetry'.
    `agent_host`: hostname or IP of the running agent (default: 'localhost').
    `agent_port`: port number (defaults: snmp-discovery=8070, snmp-telemetry=8074, probe-telemetry=8075).

    Returns the HTTP status code and the agent's JSON response.
    Status 200 = policy stopped and removed. Status 404 = policy not found.
    """
    try:
        async with AgentClient(_base_url(agent_type, agent_host, agent_port)) as client:
            response = await client.delete_policy(policy_name)
            return _response_dict(response)
    except httpx.ConnectError as e:
        return {"status_code": None, "error": f"Could not connect to {agent_type} at {agent_host}: {e}"}


async def get_agent_status(
    agent_type: AgentType,
    agent_host: str = "localhost",
    agent_port: int | None = None,
) -> dict[str, Any]:
    """
    Get the health status of a running orb-discovery agent (GET /api/v1/status).

    `agent_type`: 'snmp-discovery', 'snmp-telemetry', or 'probe-telemetry'.
    `agent_host`: hostname or IP of the running agent (default: 'localhost').
    `agent_port`: port number (defaults: snmp-discovery=8070, snmp-telemetry=8074, probe-telemetry=8075).

    Returns service uptime, version, and per-policy run history. Useful for health-checking
    before submitting new policies.
    """
    try:
        async with AgentClient(_base_url(agent_type, agent_host, agent_port)) as client:
            response = await client.get_status()
            return _response_dict(response)
    except httpx.ConnectError as e:
        return {"status_code": None, "error": f"Could not connect to {agent_type} at {agent_host}: {e}"}
