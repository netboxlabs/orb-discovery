"""Unit tests for custom_napalm.exes.ExesDriver."""

from pathlib import Path

from custom_napalm.exes import ExesDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestExesDriver(BaseDriverTest):
    """Unit tests for ExesDriver using file-based CLI mocks."""

    driver_cls = ExesDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
