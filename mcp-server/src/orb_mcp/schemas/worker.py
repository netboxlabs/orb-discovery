from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field, field_validator


class WorkerPolicyConfig(BaseModel):
    """
    Worker policy config. `package` is required (Python package implementing the Backend class).
    Any additional custom config fields are allowed alongside the standard fields.
    """

    model_config = ConfigDict(extra="allow")

    package: str = Field(min_length=1)
    schedule: str | None = None

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


class WorkerPolicy(BaseModel):
    config: WorkerPolicyConfig
    scope: dict[str, Any] | list[Any] | None = None


class WorkerPolicies(BaseModel):
    policies: dict[str, WorkerPolicy]
