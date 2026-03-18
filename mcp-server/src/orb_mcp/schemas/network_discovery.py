from __future__ import annotations

from pydantic import BaseModel, Field, field_validator

_VALID_SCAN_TYPES = frozenset(
    ["udp", "connect", "syn", "ack", "window", "null", "fin", "xmas", "maimon", "sctp_init", "sctp_cookie_echo", "ip_protocol"]
)


class NetworkDiscoveryDefaults(BaseModel):
    comments: str | None = None
    description: str | None = None
    tags: list[str] | None = None
    network_mask: int | None = Field(default=None, ge=0, le=32)


class NetworkDiscoveryScope(BaseModel):
    targets: list[str] = Field(min_length=1)
    fast_mode: bool | None = None
    timing: int | None = Field(default=None, ge=0, le=5)
    ports: list[str | int] | None = None
    exclude_ports: list[str | int] | None = None
    ping_scan: bool | None = None
    top_ports: int | None = Field(default=None, ge=1)
    max_retries: int | None = Field(default=None, ge=0)
    scan_types: list[str] | None = None
    dns_servers: list[str] | None = None
    os_detection: bool | None = None
    use_target_masks: bool | None = None
    icmp_echo: bool | None = None
    icmp_timestamp: bool | None = None
    icmp_netmask: bool | None = None
    skip_host: bool | None = None

    @field_validator("scan_types")
    @classmethod
    def validate_scan_types(cls, v: list[str] | None) -> list[str] | None:
        if v is None:
            return v
        invalid = [s for s in v if s not in _VALID_SCAN_TYPES]
        if invalid:
            raise ValueError(f"Invalid scan types: {invalid}. Must be one of: {sorted(_VALID_SCAN_TYPES)}")
        return v


class NetworkDiscoveryPolicyConfig(BaseModel):
    schedule: str | None = None
    timeout: int | None = Field(default=None, ge=1)  # minutes
    defaults: NetworkDiscoveryDefaults | None = None

    @field_validator("schedule")
    @classmethod
    def validate_cron(cls, v: str | None) -> str | None:
        if v is None:
            return v
        try:
            from croniter import croniter
            croniter(v)
        except Exception as e:
            raise ValueError(f"Invalid cron expression {v!r}: {e}") from e
        return v


class NetworkDiscoveryPolicy(BaseModel):
    config: NetworkDiscoveryPolicyConfig | None = None
    scope: NetworkDiscoveryScope


class NetworkDiscoveryPolicies(BaseModel):
    policies: dict[str, NetworkDiscoveryPolicy]
