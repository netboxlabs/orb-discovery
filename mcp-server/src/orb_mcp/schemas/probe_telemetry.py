from __future__ import annotations

import re
from typing import Literal

from pydantic import BaseModel, Field, field_validator, model_validator


class ProbeTarget(BaseModel):
    host: str
    id: str | None = Field(default=None, description="NetBox device ID — emitted as the id= label on all metrics for this target. Use as the join key for Prometheus/Alloy relabeling rules that attach site, role, and other enrichment labels.")


class HTTPConf(BaseModel):
    scheme: str = "HTTP"   # HTTP or HTTPS
    path: str = "/"
    port: int = Field(default=80, ge=1, le=65535)
    method: str = "GET"
    headers: dict[str, str] | None = None


class PingConf(BaseModel):
    packets_per_probe: int = Field(default=3, ge=1)
    packets_interval_msec: int = Field(default=100, ge=1)
    payload_size: int = Field(default=56, ge=0)


class DNSConf(BaseModel):
    domain: str
    query_type: str = "A"   # A, AAAA, MX, NS, TXT, CNAME
    min_answers: int = Field(default=1, ge=0)


class TCPConf(BaseModel):
    port: int = Field(ge=1, le=65535)
    tls_handshake: bool = False


_DURATION_RE = re.compile(r"^\d+(\.\d+)?(s|m|h)$")


class ProbeConfig(BaseModel):
    name: str
    type: Literal["http", "ping", "dns", "tcp"]
    targets: list[ProbeTarget] = Field(min_length=1)
    interval: str = "30s"
    timeout: str = "10s"
    http: HTTPConf | None = None
    ping: PingConf | None = None
    dns: DNSConf | None = None
    tcp: TCPConf | None = None

    @field_validator("interval", "timeout")
    @classmethod
    def validate_duration(cls, v: str) -> str:
        if not _DURATION_RE.match(v):
            raise ValueError(f"Invalid duration {v!r}. Use Go-style durations: '30s', '5m', '1h'")
        return v

    @model_validator(mode="after")
    def validate_type_conf(self) -> "ProbeConfig":
        conf_map = {"http": self.http, "dns": self.dns, "ping": self.ping, "tcp": self.tcp}
        if conf_map[self.type] is None:
            raise ValueError(f"Probe type '{self.type}' requires a '{self.type}' configuration block")
        return self


class ProbeTelemetryPolicy(BaseModel):
    probes: list[ProbeConfig] = Field(min_length=1)


class ProbeTelemetryPolicies(BaseModel):
    policies: dict[str, ProbeTelemetryPolicy]
