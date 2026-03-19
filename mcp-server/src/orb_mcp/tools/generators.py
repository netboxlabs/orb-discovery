from __future__ import annotations

import uuid
from typing import Any

import yaml

from orb_mcp.schemas.probe_telemetry import ProbeConfig
from orb_mcp.schemas.snmp_telemetry import SNMPTelemetryTarget


def _make_policy_name(prefix: str, policy_name: str | None) -> str:
    """Return policy_name if provided, otherwise generate one from prefix + short UUID."""
    if policy_name:
        return policy_name
    return f"{prefix}-{uuid.uuid4().hex[:8]}"


async def generate_snmp_discovery_policy(
    targets: list[dict[str, Any]],
    authentication: dict[str, Any],
    policy_name: str | None = None,
    schedule: str | None = None,
    timeout: int = 120,
    snmp_timeout: int = 5,
    snmp_probe_timeout: int = 1,
    retries: int = 3,
    site: str | None = None,
    location: str | None = None,
    role: str | None = None,
    tags: list[str] | None = None,
    lookup_extensions_dir: str | None = None,
    interface_patterns: list[dict[str, str]] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the snmp-discovery agent.

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    Each entry in `targets` is a dict with at minimum a `host` key (IP, CIDR, or range).
    Optional target keys: `port` (default 161), `authentication` (per-target override),
    `override_defaults` (per-target defaults override with keys: site, location, role, tags).

    `authentication` must include `protocol_version` (SNMPv1, SNMPv2c, or SNMPv3).
    For SNMPv2c/v1: also include `community`.
    For SNMPv3: also include `username`, `security_level`, and optionally auth/priv fields.

    `interface_patterns` is a list of dicts with `match` (regex) and `type` (NetBox interface type).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.snmp_discovery import (
        Defaults,
        SNMPDiscoveryPolicies,
        SNMPDiscoveryPolicy,
        SNMPDiscoveryPolicyConfig,
        SNMPDiscoveryScope,
    )

    name = _make_policy_name("snmp-discovery", policy_name)

    defaults = None
    if any(v is not None for v in [site, location, role, tags, interface_patterns]):
        defaults = Defaults(
            site=site,
            location=location,
            role=role,
            tags=tags,
            interface_patterns=interface_patterns,
        )

    config = SNMPDiscoveryPolicyConfig(
        schedule=schedule,
        timeout=timeout,
        snmp_timeout=snmp_timeout,
        snmp_probe_timeout=snmp_probe_timeout,
        retries=retries,
        defaults=defaults,
        lookup_extensions_dir=lookup_extensions_dir,
    )
    scope = SNMPDiscoveryScope.model_validate(
        {"targets": targets, "authentication": authentication}
    )
    policy = SNMPDiscoveryPolicy(config=config, scope=scope)
    policies = SNMPDiscoveryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_probe_telemetry_policy(
    probes: list[ProbeConfig],
    policy_name: str | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the probe-telemetry agent.

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    Each probe in `probes` must have:
    - `name`: unique string identifier
    - `type`: one of "http", "ping", "dns", "tcp"
    - `targets`: list of ProbeTarget objects with `host` and optional `id` in the format 'dcim.device:<netbox_id>' (e.g. 'dcim.device:42') — used as the Alloy enrichment join key
    - A type-specific config block matching the `type` field
    - Optional: `interval` (e.g. "30s"), `timeout` (e.g. "10s")

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.probe_telemetry import ProbeTelemetryPolicies, ProbeTelemetryPolicy

    name = _make_policy_name("probe-telemetry", policy_name)

    policy = ProbeTelemetryPolicy.model_validate({"probes": probes})
    policies = ProbeTelemetryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_device_discovery_policy(
    scope: list[dict[str, Any]],
    policy_name: str | None = None,
    schedule: str | None = None,
    site: str | None = None,
    role: str | None = None,
    location: str | None = None,
    tenant: str | None = None,
    tags: list[str] | None = None,
    description: str | None = None,
    comments: str | None = None,
    if_type: str | None = None,
    interface_patterns: list[dict[str, str]] | None = None,
    options: dict[str, Any] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the device_discovery backend (NAPALM-based).

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    Each entry in `scope` is a dict with:
    - `hostname` (required): IP, range (192.168.0.1-10), or subnet (192.168.0.0/24)
    - `username` (required): device login username
    - `password` (required): device login password (supports ${ENV_VAR} syntax)
    - `driver` (optional): NAPALM driver name (e.g. "ios", "eos", "nxos", "junos")
    - `optional_args` (optional): dict of NAPALM optional args (e.g. `ssh_config_file` for jumphost)
    - `override_defaults` (optional): per-device defaults override dict

    `options` dict supports: `platform_omit_version` (bool), `port_scan_ports` (list[int]),
    `port_scan_timeout` (float), `capture_running_config` (bool), `capture_startup_config` (bool).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.device_discovery import (
        DeviceDiscoveryDefaults,
        DeviceDiscoveryPolicies,
        DeviceDiscoveryPolicy,
        DeviceDiscoveryPolicyConfig,
    )

    name = _make_policy_name("device-discovery", policy_name)

    defaults = None
    if any(v is not None for v in [site, role, location, tenant, tags, description, comments, if_type, interface_patterns]):
        defaults = DeviceDiscoveryDefaults(
            site=site, role=role, location=location, tenant=tenant,
            tags=tags, description=description, comments=comments,
            if_type=if_type, interface_patterns=interface_patterns,
        )

    config = DeviceDiscoveryPolicyConfig.model_validate({
        "schedule": schedule,
        "defaults": defaults.model_dump(exclude_none=True) if defaults else None,
        "options": options,
    })
    policy = DeviceDiscoveryPolicy.model_validate({"config": config.model_dump(exclude_none=True), "scope": scope})
    policies = DeviceDiscoveryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_network_discovery_policy(
    targets: list[str],
    policy_name: str | None = None,
    schedule: str | None = None,
    timeout: int | None = None,
    description: str | None = None,
    tags: list[str] | None = None,
    comments: str | None = None,
    network_mask: int | None = None,
    fast_mode: bool | None = None,
    timing: int | None = None,
    ports: list[str | int] | None = None,
    exclude_ports: list[str | int] | None = None,
    scan_types: list[str] | None = None,
    skip_host: bool | None = None,
    top_ports: int | None = None,
    max_retries: int | None = None,
    os_detection: bool | None = None,
    use_target_masks: bool | None = None,
    ping_scan: bool | None = None,
    icmp_echo: bool | None = None,
    icmp_timestamp: bool | None = None,
    icmp_netmask: bool | None = None,
    dns_servers: list[str] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the network_discovery backend (NMAP-based).

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    `targets`: IPs (192.168.1.1), ranges (192.168.1.10-20), subnets (192.168.1.0/24), or hostnames.
    `timeout`: NMAP scan timeout in minutes (default: 2).
    `scan_types`: e.g. ["connect", "syn", "udp"]. Use ["connect"] + skip_host=True for rootless podman.
    `timing`: NMAP timing template 0-5 (higher = faster). Default T3.
    `ports`: e.g. [22, 161, 443, "500-600"]. Mixed list of ints and range strings.

    ⚠️ NOTE: Default NMAP behavior requires root/CAP_NET_RAW. For rootless podman, use
    scan_types=["connect"] and skip_host=True.

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.network_discovery import (
        NetworkDiscoveryDefaults,
        NetworkDiscoveryPolicies,
        NetworkDiscoveryPolicy,
        NetworkDiscoveryPolicyConfig,
        NetworkDiscoveryScope,
    )

    name = _make_policy_name("network-discovery", policy_name)

    defaults = None
    if any(v is not None for v in [description, tags, comments, network_mask]):
        defaults = NetworkDiscoveryDefaults(
            description=description, tags=tags, comments=comments, network_mask=network_mask
        )

    config = NetworkDiscoveryPolicyConfig(
        schedule=schedule,
        timeout=timeout,
        defaults=defaults,
    )
    scope = NetworkDiscoveryScope.model_validate({
        "targets": targets,
        "fast_mode": fast_mode,
        "timing": timing,
        "ports": ports,
        "exclude_ports": exclude_ports,
        "scan_types": scan_types,
        "skip_host": skip_host,
        "top_ports": top_ports,
        "max_retries": max_retries,
        "os_detection": os_detection,
        "use_target_masks": use_target_masks,
        "ping_scan": ping_scan,
        "icmp_echo": icmp_echo,
        "icmp_timestamp": icmp_timestamp,
        "icmp_netmask": icmp_netmask,
        "dns_servers": dns_servers,
    })
    policy = NetworkDiscoveryPolicy(config=config, scope=scope)
    policies = NetworkDiscoveryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_worker_policy(
    package: str,
    scope: dict[str, Any] | list[Any],
    policy_name: str | None = None,
    schedule: str | None = None,
    extra_config: dict[str, Any] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the worker backend (custom Python worker).

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    `package` (required): Python package name implementing the Backend class.
    `scope`: user-defined scope (any dict or list structure passed to the worker).
    `schedule`: optional cron expression for recurring execution.
    `extra_config`: optional additional config fields passed alongside `package` and `schedule`.
      Use this for worker-specific configuration (e.g. {"custom_config": "value"}).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.worker import WorkerPolicies, WorkerPolicy, WorkerPolicyConfig

    name = _make_policy_name("worker", policy_name)

    config_data: dict[str, Any] = {"package": package}
    if schedule is not None:
        config_data["schedule"] = schedule
    if extra_config:
        config_data.update(extra_config)

    config = WorkerPolicyConfig.model_validate(config_data)
    policy = WorkerPolicy(config=config, scope=scope)
    policies = WorkerPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_flow_telemetry_policy(
    port: int,
    rollups: list[dict],
    policy_name: str | None = None,
    host: str | None = None,
    id: str | None = None,
    protocol: str | None = None,
    workers: int | None = None,
    queue_size: int | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the flow-telemetry agent.

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    `port` (required): UDP port to listen on for incoming flow datagrams.
    `host` (optional): IP address to bind the UDP listener to. Defaults to 0.0.0.0.
    `id` (optional): identifier attached to all exported metrics as an OTLP attribute.
      Use it to distinguish between multiple flow-telemetry instances (e.g. by site or role).
    `protocol` (optional): flow decoder — 'auto', 'netflow5', 'netflow9', 'ipfix', or 'sflow'.
      Defaults to 'auto'.
    `workers` (optional): number of UDP receiver goroutines. Defaults to 2.
    `queue_size` (optional): UDP receive queue depth. Defaults to 10000.
    `rollups` (required): list of aggregation rules. Each entry must have:
      - `method`: 'sum', 'max', or 'min'
      - `name`: metric name suffix — exported as flow.<name>
      - `metrics`: list of flow fields to aggregate ('bytes', 'packets')
      - `dimensions` (optional): list of grouping keys
          Valid dimensions: src_addr, dst_addr, src_port, dst_port, proto,
          sampler_addr, in_if, out_if, src_as, dst_as

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.flow_telemetry import (
        FlowPolicyConfig,
        FlowScope,
        FlowTelemetryPolicies,
        FlowTelemetryPolicy,
    )

    name = _make_policy_name("flow-telemetry", policy_name)

    scope = FlowScope.model_validate({"port": port, "host": host, "id": id})
    config = FlowPolicyConfig.model_validate({
        "protocol": protocol,
        "workers": workers,
        "queue_size": queue_size,
        "rollups": rollups,
    })
    policy = FlowTelemetryPolicy(scope=scope, config=config)
    policies = FlowTelemetryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_snmp_telemetry_policy(
    targets: list[SNMPTelemetryTarget],
    authentication: dict[str, Any],
    policy_name: str | None = None,
    metrics_interval: int | None = None,
    snmp_timeout: int = 5,
    retries: int = 3,
    profiles_dir: str | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the snmp-telemetry agent.

    `policy_name` is optional — if omitted, a unique name is generated automatically.

    Each entry in `targets` is an SNMPTelemetryTarget with `host` (IP, CIDR, or range),
    optional `port` (default 161), optional `id` in the format 'dcim.device:<netbox_id>'
    (e.g. 'dcim.device:42') — used as the Alloy enrichment join key, and optional
    per-target `authentication` override.

    `authentication` must include `protocol_version` (SNMPv1, SNMPv2c, or SNMPv3).
    For SNMPv2c/v1: also include `community`.
    For SNMPv3: also include `username`, `security_level`, and optionally auth/priv fields.

    `metrics_interval`: seconds between metric collections (required, must be >= 1).
    `profiles_dir`: path to ktranslate SNMP profiles directory (default: /usr/local/share/snmp-profiles).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.snmp_telemetry import (
        SNMPTelemetryPolicies,
        SNMPTelemetryPolicy,
        SNMPTelemetryPolicyConfig,
        SNMPTelemetryScope,
    )

    name = _make_policy_name("snmp-telemetry", policy_name)

    config = SNMPTelemetryPolicyConfig(
        metrics_interval=metrics_interval,
        snmp_timeout=snmp_timeout,
        retries=retries,
        profiles_dir=profiles_dir,
    )
    scope = SNMPTelemetryScope.model_validate(
        {"targets": targets, "authentication": authentication}
    )
    policy = SNMPTelemetryPolicy(config=config, scope=scope)
    policies = SNMPTelemetryPolicies(policies={name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)
