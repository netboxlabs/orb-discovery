"""Custom NAPALM drivers shipped with device-discovery."""

from custom_napalm.ios import IOSDriver
from custom_napalm.srl import NokiaSRLDriver

__all__ = ["IOSDriver", "NokiaSRLDriver"]
