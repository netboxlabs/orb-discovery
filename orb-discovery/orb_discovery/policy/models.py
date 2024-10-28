from typing import Any

from pydantic import BaseModel, Field, ValidationError

class ParseException(Exception):
    """Custom exception for parsing errors."""

    pass


class Napalm(BaseModel):
    """Model for NAPALM configuration."""

    driver: str | None = Field(default=None, description="Driver name, optional")
    hostname: str
    username: str
    password: str
    timeout: int = 60
    optional_args: dict[str, Any] | None = Field(
        default=None, description="Optional arguments"
    )


class DiscoveryConfig(BaseModel):
    """Model for discovery configuration."""

    netbox: dict[str, str]


class Policy(BaseModel):
    """Model for a policy configuration."""

    config: DiscoveryConfig
    data: list[Napalm]