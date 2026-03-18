from __future__ import annotations

from pydantic import BaseModel, Field, field_validator

from orb_mcp.schemas.common import Authentication


class SNMPTelemetryTarget(BaseModel):
    host: str
    port: int = Field(default=161, ge=1, le=65535)
    authentication: Authentication | None = None


class SNMPTelemetryScope(BaseModel):
    targets: list[SNMPTelemetryTarget] = Field(min_length=1)
    authentication: Authentication | None = None


class SNMPTelemetryPolicyConfig(BaseModel):
    schedule: str | None = None
    metrics_interval: int | None = None  # seconds; None = disabled
    profiles_dir: str | None = None
    snmp_timeout: int = 5
    retries: int = 3
    lookup_extensions_dir: str | None = None

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

    @field_validator("metrics_interval")
    @classmethod
    def validate_metrics_interval(cls, v: int | None) -> int | None:
        if v is not None and v < 1:
            raise ValueError("metrics_interval must be >= 1 second")
        return v


class SNMPTelemetryPolicy(BaseModel):
    config: SNMPTelemetryPolicyConfig = Field(default_factory=SNMPTelemetryPolicyConfig)
    scope: SNMPTelemetryScope


class SNMPTelemetryPolicies(BaseModel):
    policies: dict[str, SNMPTelemetryPolicy]
