"""Custom extensions for the NAPALM IOS driver."""

from napalm.ios.ios import IOSDriver as NapalmIOSDriver
from ntc_templates.parse import parse_output

class IOSDriver(NapalmIOSDriver):
    """Extend the base IOS driver with a site-code helper."""

    def get_site_code(self) -> dict:
        """
        Return site code information from the running configuration if present.

        Uses a simple CLI grep to extract a configured site code line.
        """
        output = self._send_command("show inventory")
        parsed_output = parse_output(platform="cisco_ios", command="show inventory", data=output)
        return {"site_code": parsed_output} if parsed_output else {}
