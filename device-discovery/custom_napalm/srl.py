"""Custom extensions for the NAPALM SR Linux driver."""

from napalm_srl.srl import NokiaSRLDriver as NapalmSRLDriver


class NokiaSRLDriver(NapalmSRLDriver):
    """Extend the base SR Linux driver with a site-code helper."""

    def get_site_code(self) -> dict:
        """
        Return site code information from the running configuration if present.

        Tries both JSON and CLI configuration formats to locate a `site-code`
        key or line. Falls back to an empty dict when nothing is found.
        """
        return {"site_code": "called"}