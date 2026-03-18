from __future__ import annotations

import re

from pydantic import BaseModel, Field, field_validator

from orb_mcp.schemas.common import Authentication


class IPAddressDefaults(BaseModel):
    description: str | None = None
    tags: list[str] | None = None
    comments: str | None = None
    role: str | None = None
    tenant: str | None = None
    vrf: str | None = None


class InterfaceDefaults(BaseModel):
    description: str | None = None
    tags: list[str] | None = None
    if_type: str | None = None  # YAML key: if_type (Go: yaml:"if_type,omitempty")


class InterfacePattern(BaseModel):
    match: str  # regex
    type: str   # NetBox interface type

    @field_validator("match")
    @classmethod
    def validate_regex(cls, v: str) -> str:
        try:
            re.compile(v)
        except re.error as e:
            raise ValueError(f"Invalid regex pattern: {e}") from e
        return v


class DeviceDefaults(BaseModel):
    description: str | None = None
    tags: list[str] | None = None
    comments: str | None = None


class Defaults(BaseModel):
    tags: list[str] | None = None
    site: str | None = None
    location: str | None = None
    role: str | None = None
    ip_address: IPAddressDefaults | None = None
    interface: InterfaceDefaults | None = None
    device: DeviceDefaults | None = None
    interface_patterns: list[InterfacePattern] | None = None


class SNMPDiscoveryTarget(BaseModel):
    host: str
    port: int = Field(default=161, ge=1, le=65535)
    authentication: Authentication | None = None
    override_defaults: Defaults | None = None


class SNMPDiscoveryScope(BaseModel):
    targets: list[SNMPDiscoveryTarget] = Field(min_length=1)
    authentication: Authentication | None = None


class SNMPDiscoveryPolicyConfig(BaseModel):
    schedule: str | None = None
    timeout: int = 120
    snmp_timeout: int = 5
    snmp_probe_timeout: int = 1
    retries: int = 3
    defaults: Defaults | None = None
    lookup_extensions_dir: str | None = None

    @field_validator("schedule")
    @classmethod
    def validate_cron(cls, v: str | None) -> str | None:
        if v is None:
            return v
        try:
            from croniter import CroniterBadCronError, croniter
            croniter(v)
        except Exception as e:
            raise ValueError(f"Invalid cron expression {v!r}: {e}") from e
        return v


class SNMPDiscoveryPolicy(BaseModel):
    config: SNMPDiscoveryPolicyConfig = Field(default_factory=SNMPDiscoveryPolicyConfig)
    scope: SNMPDiscoveryScope


class SNMPDiscoveryPolicies(BaseModel):
    policies: dict[str, SNMPDiscoveryPolicy]
