"""Custom extensions for the NAPALM IOS driver."""

from napalm.ios.ios import IOSDriver as NapalmIOSDriver
from ntc_templates.parse import parse_output


class IOSDriver(NapalmIOSDriver):
    """Extend the base IOS driver with an inventory helper."""

    def get_inventory(self) -> dict:
        """
        Return parsed hardware inventory from the device.

        Uses `show inventory` parsed via ntc-templates. Falls back to an
        empty dict when the command or parser is unavailable.
        """
        try:
            output = self._send_command("show inventory")
            parsed_output = parse_output(
                platform="cisco_ios", command="show inventory", data=output
            )
            return {"items": parsed_output} if parsed_output else {}
        except Exception:
            return {}
