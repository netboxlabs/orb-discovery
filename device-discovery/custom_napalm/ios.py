"""Custom extensions for the NAPALM IOS driver."""

from napalm.ios.ios import IOSDriver as NapalmIOSDriver
from ntc_templates.parse import parse_output


class IOSDriver(NapalmIOSDriver):
    """Extend the base IOS driver with an inventory helper."""

    def get_stack_info(self) -> dict:
        """
        Return switch stack information combining show switch and show inventory.

        Returns dict with:
        - is_stack: bool - Whether device is a stack
        - members: list - Stack member details
        - master_number: int - Stack master member number

        For non-stacked devices, returns {"is_stack": False}.
        """
        try:
            # Get stack topology from show switch
            switch_output = self._send_command("show switch")
            switch_data = parse_output(
                platform="cisco_ios", command="show switch", data=switch_output
            )

            # If no switch data or single member, not a stack
            if not switch_data or len(switch_data) <= 1:
                return {"is_stack": False}

            # Get detailed inventory
            inventory_output = self._send_command("show inventory")
            inventory_data = parse_output(
                platform="cisco_ios", command="show inventory", data=inventory_output
            )

            # Build inventory lookup by switch number
            # Inventory items typically have names like "Switch 1", "Switch 2"
            inventory_by_switch = {}
            if inventory_data:
                for item in inventory_data:
                    name = item.get("name", "")
                    # Try to extract switch number from name
                    if "switch" in name.lower():
                        parts = name.lower().split("switch")
                        if len(parts) > 1:
                            # Extract number after "switch"
                            num_str = "".join(filter(str.isdigit, parts[1].split()[0] if parts[1].strip() else ""))
                            if num_str:
                                switch_num = int(num_str)
                                inventory_by_switch[switch_num] = item

            # Build member list
            members = []
            master_number = None

            for switch_entry in switch_data:
                switch_number = int(switch_entry.get("switch", 0))
                role = switch_entry.get("role", "Member")

                # Track master
                if role.lower() in ("master", "active"):
                    master_number = switch_number

                # Get inventory details for this switch
                inv_item = inventory_by_switch.get(switch_number, {})

                member = {
                    "switch_number": switch_number,
                    "role": role,
                    "priority": switch_entry.get("priority"),
                    "state": switch_entry.get("state", "Unknown"),
                    "mac_address": switch_entry.get("mac_address"),
                    "serial": inv_item.get("sn") or inv_item.get("serial"),
                    "model": inv_item.get("pid") or inv_item.get("model"),
                }
                members.append(member)

            return {
                "is_stack": True,
                "master_number": master_number or 1,
                "members": sorted(members, key=lambda x: x["switch_number"]),
            }

        except Exception:
            # If any error occurs, treat as non-stacked device
            return {"is_stack": False}
