"""Unit tests for custom_napalm.brocade_fastiron.BrocadeFastIronDriver."""

from pathlib import Path

from custom_napalm.brocade_fastiron import BrocadeFastIronDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestBrocadeFastIronDriver(BaseDriverTest):
    """Unit tests for BrocadeFastIronDriver using file-based CLI mocks."""

    driver_cls = BrocadeFastIronDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"
