"""Unit tests for custom_napalm.panos_ssh.PANOSSHDriver."""

from pathlib import Path

from custom_napalm.panos_ssh import PANOSSHDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestPANOSSHDriver(BaseDriverTest):
    driver_cls = PANOSSHDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
