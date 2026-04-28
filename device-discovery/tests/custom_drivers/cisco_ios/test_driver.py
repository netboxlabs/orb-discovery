"""Unit tests for custom_napalm.ios.IOSDriver."""

from pathlib import Path

from custom_napalm.ios import IOSDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestIOSDriver(BaseDriverTest):
    """Unit tests for our IOSDriver using file-based CLI mocks."""

    driver_cls = IOSDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
