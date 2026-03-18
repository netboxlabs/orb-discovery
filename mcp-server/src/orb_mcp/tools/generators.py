from __future__ import annotations

from typing import Any

import yaml


async def generate_snmp_discovery_policy(
    policy_name: str,
    targets: list[dict[str, Any]],
    authentication: dict[str, Any],
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
    policies = SNMPDiscoveryPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_probe_telemetry_policy(
    policy_name: str,
    probes: list[dict[str, Any]],
    site: str | None = None,
    role: str | None = None,
    location: str | None = None,
    tenant: str | None = None,
    tags: list[str] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the probe-telemetry agent.

    `probes` is a list of probe dicts. Each probe must have:
    - `name`: unique string identifier
    - `type`: one of "http", "ping", "dns", "tcp"
    - `targets`: list of dicts with `host` key
    - A type-specific config block matching the `type` field:
      - `http`: dict with `scheme`, `path`, `port`, `method`, optional `headers`
      - `ping`: dict with `packets_per_probe`, `packets_interval_msec`, `payload_size`
      - `dns`: dict with `domain`, `query_type` (A/AAAA/MX), `min_answers`
      - `tcp`: dict with `port`, `tls_handshake`
    - Optional: `interval` (e.g. "30s"), `timeout` (e.g. "10s"), `override_defaults`

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.probe_telemetry import ProbeDefaults, ProbeTelemetryPolicies, ProbeTelemetryPolicy

    defaults = None
    if any(v is not None for v in [site, role, location, tenant, tags]):
        defaults = ProbeDefaults(site=site, role=role, location=location, tenant=tenant, tags=tags)

    policy = ProbeTelemetryPolicy.model_validate(
        {"defaults": defaults.model_dump(exclude_none=True) if defaults else None, "probes": probes}
    )
    policies = ProbeTelemetryPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_device_discovery_policy(
    policy_name: str,
    scope: list[dict[str, Any]],
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
    policies = DeviceDiscoveryPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_network_discovery_policy(
    policy_name: str,
    targets: list[str],
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
    policies = NetworkDiscoveryPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_worker_policy(
    policy_name: str,
    package: str,
    scope: dict[str, Any] | list[Any],
    schedule: str | None = None,
    extra_config: dict[str, Any] | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the worker backend (custom Python worker).

    `package` (required): Python package name implementing the Backend class.
    `scope`: user-defined scope (any dict or list structure passed to the worker).
    `schedule`: optional cron expression for recurring execution.
    `extra_config`: optional additional config fields passed alongside `package` and `schedule`.
      Use this for worker-specific configuration (e.g. {"custom_config": "value"}).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.worker import WorkerPolicies, WorkerPolicy, WorkerPolicyConfig

    config_data: dict[str, Any] = {"package": package}
    if schedule is not None:
        config_data["schedule"] = schedule
    if extra_config:
        config_data.update(extra_config)

    config = WorkerPolicyConfig.model_validate(config_data)
    policy = WorkerPolicy(config=config, scope=scope)
    policies = WorkerPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)


async def generate_snmp_telemetry_policy(
    policy_name: str,
    targets: list[dict[str, Any]],
    authentication: dict[str, Any],
    schedule: str | None = None,
    metrics_interval: int | None = None,
    snmp_timeout: int = 5,
    retries: int = 3,
    profiles_dir: str | None = None,
    lookup_extensions_dir: str | None = None,
) -> str:
    """
    Generate a valid YAML policy document for the snmp-telemetry agent.

    Each entry in `targets` is a dict with at minimum a `host` key.
    Optional target keys: `port` (default 161), `authentication` (per-target override).

    `authentication` must include `protocol_version` (SNMPv1, SNMPv2c, or SNMPv3).
    For SNMPv2c/v1: also include `community`.
    For SNMPv3: also include `username`, `security_level`, and optionally auth/priv fields.

    `metrics_interval`: seconds between metric collections. Omit or set to None to disable.
    `profiles_dir`: path to ktranslate SNMP profiles directory (default: /usr/local/share/snmp-profiles).

    Returns the YAML string ready to POST to the agent's /api/v1/policies endpoint.
    """
    from orb_mcp.schemas.snmp_telemetry import (
        SNMPTelemetryPolicies,
        SNMPTelemetryPolicy,
        SNMPTelemetryPolicyConfig,
        SNMPTelemetryScope,
    )

    config = SNMPTelemetryPolicyConfig(
        schedule=schedule,
        metrics_interval=metrics_interval,
        snmp_timeout=snmp_timeout,
        retries=retries,
        profiles_dir=profiles_dir,
        lookup_extensions_dir=lookup_extensions_dir,
    )
    scope = SNMPTelemetryScope.model_validate(
        {"targets": targets, "authentication": authentication}
    )
    policy = SNMPTelemetryPolicy(config=config, scope=scope)
    policies = SNMPTelemetryPolicies(policies={policy_name: policy})
    data = policies.model_dump(exclude_none=True)
    return yaml.dump(data, default_flow_style=False, sort_keys=False, allow_unicode=True)
