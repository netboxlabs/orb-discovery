from mcp.server.fastmcp import FastMCP

from orb_mcp.tools.agent import delete_policy, get_agent_status, list_policies, submit_policy
from orb_mcp.tools.generators import (
    generate_device_discovery_policy,
    generate_network_discovery_policy,
    generate_probe_telemetry_policy,
    generate_snmp_discovery_policy,
    generate_snmp_telemetry_policy,
    generate_worker_policy,
)
from orb_mcp.tools.validator import validate_policy

mcp = FastMCP(
    name="orb-discovery-mcp",
    instructions=(
        "Tools for generating, validating, and managing policies for orb-discovery agents. "
        "Use the generate_* tools to produce valid YAML, validate_policy to check it, "
        "and submit_policy / list_policies / delete_policy / get_agent_status to interact "
        "with running snmp-discovery, snmp-telemetry, or probe-telemetry agent instances."
    ),
)

mcp.tool()(generate_snmp_discovery_policy)
mcp.tool()(generate_snmp_telemetry_policy)
mcp.tool()(generate_probe_telemetry_policy)
mcp.tool()(generate_device_discovery_policy)
mcp.tool()(generate_network_discovery_policy)
mcp.tool()(generate_worker_policy)
mcp.tool()(validate_policy)
mcp.tool()(submit_policy)
mcp.tool()(list_policies)
mcp.tool()(delete_policy)
mcp.tool()(get_agent_status)


def main() -> None:
    mcp.run()


if __name__ == "__main__":
    main()
