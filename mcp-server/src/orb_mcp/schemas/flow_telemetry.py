from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field, field_validator


_VALID_METRICS = {"bytes", "packets"}
_VALID_DIMENSIONS = {
    "src_addr", "dst_addr", "src_port", "dst_port",
    "proto", "sampler_addr", "in_if", "out_if", "src_as", "dst_as",
}
_VALID_PROTOCOLS = {"auto", "netflow5", "netflow9", "ipfix", "sflow", ""}


class FlowScope(BaseModel):
    port: int = Field(ge=1, le=65535, description="UDP port to listen on.")
    host: str | None = Field(default=None, description="IP address to bind the UDP listener to. Defaults to 0.0.0.0.")
    id: str | None = Field(default=None, description="NetBox device identifier. Emitted as the netbox_id= label on all exported metrics. Used as the join key for Prometheus/Alloy relabeling rules that attach site, role, and other NetBox enrichment labels.")


class Rollup(BaseModel):
    method: Literal["sum", "max", "min"]
    name: str = Field(min_length=1, description="Metric name suffix — exported as flow.<name>.")
    metrics: list[str] = Field(min_length=1, description="Flow fields to aggregate: 'bytes', 'packets'.")
    dimensions: list[str] = Field(default_factory=list, description="Flow fields to group by.")

    @field_validator("metrics")
    @classmethod
    def validate_metrics(cls, v: list[str]) -> list[str]:
        for m in v:
            if m not in _VALID_METRICS:
                raise ValueError(f"Unsupported metric {m!r}. Must be one of: {sorted(_VALID_METRICS)}")
        return v

    @field_validator("dimensions")
    @classmethod
    def validate_dimensions(cls, v: list[str]) -> list[str]:
        for d in v:
            if d not in _VALID_DIMENSIONS:
                raise ValueError(f"Unsupported dimension {d!r}. Must be one of: {sorted(_VALID_DIMENSIONS)}")
        return v


class FlowPolicyConfig(BaseModel):
    protocol: str | None = Field(default=None, description="Flow decoder: auto, netflow5, netflow9, ipfix, sflow. Defaults to auto.")
    workers: int | None = Field(default=None, ge=1, description="Number of UDP receiver goroutines. Defaults to 2.")
    queue_size: int | None = Field(default=None, ge=1, description="UDP receive queue depth. Defaults to 10000.")
    rollups: list[Rollup] = Field(min_length=1, description="Aggregation rules. At least one required.")

    @field_validator("protocol")
    @classmethod
    def validate_protocol(cls, v: str | None) -> str | None:
        if v is not None and v not in _VALID_PROTOCOLS:
            raise ValueError(f"Unsupported protocol {v!r}. Must be one of: auto, netflow5, netflow9, ipfix, sflow.")
        return v


class FlowTelemetryPolicy(BaseModel):
    scope: FlowScope
    config: FlowPolicyConfig


class FlowTelemetryPolicies(BaseModel):
    policies: dict[str, FlowTelemetryPolicy]
