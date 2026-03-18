from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, field_validator, model_validator

_PROTOCOL_VERSION_ALIASES: dict[str, str] = {
    "1": "SNMPv1",
    "v1": "SNMPv1",
    "snmpv1": "SNMPv1",
    "2": "SNMPv2c",
    "v2": "SNMPv2c",
    "2c": "SNMPv2c",
    "v2c": "SNMPv2c",
    "snmpv2c": "SNMPv2c",
    "3": "SNMPv3",
    "v3": "SNMPv3",
    "snmpv3": "SNMPv3",
}


class Authentication(BaseModel):
    """SNMP authentication credentials. Mirrors the Go Authentication struct (flat, all fields)."""

    protocol_version: Literal["SNMPv1", "SNMPv2c", "SNMPv3"]

    @field_validator("protocol_version", mode="before")
    @classmethod
    def normalize_protocol_version(cls, v: object) -> object:
        if isinstance(v, str):
            normalized = _PROTOCOL_VERSION_ALIASES.get(v.lower())
            if normalized:
                return normalized
        return v
    community: str | None = None
    security_level: str | None = None  # noAuthNoPriv, authNoPriv, authPriv
    username: str | None = None
    auth_protocol: str | None = None  # MD5, SHA, SHA224, SHA256, SHA384, SHA512
    auth_passphrase: str | None = None
    priv_protocol: str | None = None  # DES, AES, AES192, AES256
    priv_passphrase: str | None = None

    @model_validator(mode="after")
    def validate_auth_fields(self) -> "Authentication":
        if self.protocol_version in ("SNMPv1", "SNMPv2c"):
            if not self.community:
                raise ValueError(f"{self.protocol_version} requires 'community'")
        elif self.protocol_version == "SNMPv3":
            if not self.username:
                raise ValueError("SNMPv3 requires 'username'")
            level = self.security_level or "noAuthNoPriv"
            if level in ("authNoPriv", "authPriv"):
                if not self.auth_protocol or not self.auth_passphrase:
                    raise ValueError(
                        f"security_level '{level}' requires 'auth_protocol' and 'auth_passphrase'"
                    )
            if level == "authPriv":
                if not self.priv_protocol or not self.priv_passphrase:
                    raise ValueError(
                        "security_level 'authPriv' requires 'priv_protocol' and 'priv_passphrase'"
                    )
        return self
