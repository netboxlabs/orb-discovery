"""Unit tests for custom_napalm.viptela_ssh.ViptelaSshDriver."""

from pathlib import Path

from custom_napalm.viptela_ssh import ViptelaSshDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestViptelaSshDriver(BaseDriverTest):
    """Unit tests for ViptelaSshDriver using file-based CLI mocks."""

    driver_cls = ViptelaSshDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
