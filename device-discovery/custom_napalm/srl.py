"""Custom extensions for the NAPALM SR Linux driver."""

from napalm_srl.srl import NokiaSRLDriver as SRLDriver


class NokiaSRLDriver(SRLDriver):
    """Extend the base SR Linux driver with an inventory helper."""

    def get_inventory(self) -> dict:
        """Return custom inventory information if present."""
        return {"items": []}  # Placeholder for actual implementation
