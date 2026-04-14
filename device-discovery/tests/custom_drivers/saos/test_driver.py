"""Unit tests for custom_napalm.saos.SAOSDriver."""

from pathlib import Path

from custom_napalm.saos import SAOSDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestSAOSDriver(BaseDriverTest):
    """Unit tests for SAOSDriver using file-based CLI mocks."""

    driver_cls = SAOSDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
