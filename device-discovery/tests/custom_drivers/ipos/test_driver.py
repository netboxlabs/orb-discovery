from pathlib import Path

from custom_napalm.ipos import IPOSDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestIPOSDriver(BaseDriverTest):
    driver_cls = IPOSDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
