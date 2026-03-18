from __future__ import annotations

import importlib
from typing import Any, Literal

import yaml
from pydantic import ValidationError

AgentType = Literal[
    "snmp-discovery",
    "snmp-telemetry",
    "probe-telemetry",
    "device-discovery",
    "network-discovery",
    "worker",
]

_SCHEMA_MAP: dict[str, tuple[str, str]] = {
    "snmp-discovery": ("orb_mcp.schemas.snmp_discovery", "SNMPDiscoveryPolicies"),
    "snmp-telemetry": ("orb_mcp.schemas.snmp_telemetry", "SNMPTelemetryPolicies"),
    "probe-telemetry": ("orb_mcp.schemas.probe_telemetry", "ProbeTelemetryPolicies"),
    "device-discovery": ("orb_mcp.schemas.device_discovery", "DeviceDiscoveryPolicies"),
    "network-discovery": ("orb_mcp.schemas.network_discovery", "NetworkDiscoveryPolicies"),
    "worker": ("orb_mcp.schemas.worker", "WorkerPolicies"),
}


async def validate_policy(
    yaml_content: str,
    agent_type: AgentType,
) -> dict[str, Any]:
    """
    Validate a policy YAML string against the schema for the specified agent type.

    `agent_type` must be one of: 'snmp-discovery', 'snmp-telemetry', 'probe-telemetry'.

    Returns a dict with:
    - `valid: true` and `policies` (list of policy names) and `data` (parsed structure) on success
    - `valid: false` and `errors` (list of field-level error dicts) on failure

    Each error dict has keys: `field`, `type`, `error`.
    """
    try:
        raw = yaml.safe_load(yaml_content)
    except yaml.YAMLError as e:
        return {"valid": False, "agent_type": agent_type, "errors": [{"field": "yaml", "type": "yaml_parse_error", "error": str(e)}]}

    if not isinstance(raw, dict):
        return {"valid": False, "agent_type": agent_type, "errors": [{"field": "root", "type": "invalid_type", "error": "Expected a YAML mapping at the root level"}]}

    module_path, class_name = _SCHEMA_MAP[agent_type]
    module = importlib.import_module(module_path)
    schema_class = getattr(module, class_name)

    try:
        parsed = schema_class.model_validate(raw)
        return {
            "valid": True,
            "agent_type": agent_type,
            "policies": list(parsed.policies.keys()),
            "data": parsed.model_dump(exclude_none=True),
        }
    except ValidationError as e:
        errors = [
            {
                "field": ".".join(str(loc) for loc in err["loc"]),
                "type": err["type"],
                "error": err["msg"],
            }
            for err in e.errors()
        ]
        return {"valid": False, "agent_type": agent_type, "errors": errors}
