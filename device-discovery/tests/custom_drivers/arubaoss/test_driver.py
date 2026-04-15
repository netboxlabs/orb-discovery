"""Unit tests for custom_napalm.arubaoss.ArubaOSSDriver."""

from pathlib import Path

from custom_napalm.arubaoss import ArubaOSSDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestArubaOSSDriver(BaseDriverTest):
    """Unit tests for ArubaOSSDriver using file-based CLI mocks."""

    driver_cls = ArubaOSSDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
