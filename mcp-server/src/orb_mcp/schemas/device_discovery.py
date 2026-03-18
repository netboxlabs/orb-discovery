from __future__ import annotations

import re
from typing import Any

from pydantic import BaseModel, Field, field_validator


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
    model: str | None = None
    manufacturer: str | None = None
    platform: str | None = None
    description: str | None = None
    comments: str | None = None
    tags: list[str] | None = None


class InterfaceDefaults(BaseModel):
    description: str | None = None
    tags: list[str] | None = None


class IPAddressDefaults(BaseModel):
    role: str | None = None
    tenant: str | None = None
    vrf: str | None = None
    description: str | None = None
    comments: str | None = None
    tags: list[str] | None = None


class PrefixDefaults(BaseModel):
    role: str | None = None
    tenant: str | None = None
    vrf: str | None = None
    description: str | None = None
    comments: str | None = None
    tags: list[str] | None = None


class VLANDefaults(BaseModel):
    group: str | None = None
    tenant: str | None = None
    role: str | None = None
    description: str | None = None
    comments: str | None = None
    tags: list[str] | None = None


class TenantDefaults(BaseModel):
    name: str | None = None
    group: str | None = None
    description: str | None = None
    tags: list[str] | None = None


class DeviceDiscoveryDefaults(BaseModel):
    site: str | None = None
    role: str | None = None
    if_type: str | None = None
    interface_patterns: list[InterfacePattern] | None = None
    location: str | None = None
    # tenant can be a plain string or a nested map
    tenant: str | TenantDefaults | None = None
    description: str | None = None
    comments: str | None = None
    tags: list[str] | None = None
    device: DeviceDefaults | None = None
    interface: InterfaceDefaults | None = None
    ipaddress: IPAddressDefaults | None = None  # Note: "ipaddress" (not "ip_address")
    prefix: PrefixDefaults | None = None
    vlan: VLANDefaults | None = None


class DeviceDiscoveryOptions(BaseModel):
    platform_omit_version: bool | None = None
    port_scan_ports: list[int] | None = None
    port_scan_timeout: float | None = None
    capture_running_config: bool | None = None
    capture_startup_config: bool | None = None


class DeviceDiscoveryTarget(BaseModel):
    hostname: str
    username: str
    password: str
    driver: str | None = None
    optional_args: dict[str, Any] | None = None
    override_defaults: DeviceDiscoveryDefaults | None = None


class DeviceDiscoveryPolicyConfig(BaseModel):
    schedule: str | None = None
    defaults: DeviceDiscoveryDefaults | None = None
    options: DeviceDiscoveryOptions | None = None

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


class DeviceDiscoveryPolicy(BaseModel):
    config: DeviceDiscoveryPolicyConfig | None = None
    scope: list[DeviceDiscoveryTarget] = Field(min_length=1)


class DeviceDiscoveryPolicies(BaseModel):
    policies: dict[str, DeviceDiscoveryPolicy]
