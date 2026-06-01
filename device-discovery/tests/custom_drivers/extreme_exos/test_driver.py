"""Unit tests for custom_napalm.extreme_exos.ExosDriver."""

from pathlib import Path

from custom_napalm.extreme_exos import ExosDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestExosDriver(BaseDriverTest):
    """Unit tests for ExosDriver using file-based CLI mocks."""

    driver_cls = ExosDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"


def test_get_facts_captures_multi_token_model(tmp_path: Path) -> None:
    """get_facts() must capture multi-token System Type values like 'BlackDiamond X8'."""
    mock_dir = tmp_path / "test_get_facts" / "bdx8"
    mock_dir.mkdir(parents=True)
    (mock_dir / "show_version.txt").write_text(
        "Switch      : BD-X8 (800533-00-01) Rev 1.0 Boot PROM Version v1.0.0.1\n"
        "System MAC  : 00:04:96:01:02:03\n"
        "System Type : BlackDiamond X8\n"
        "SysName     : bdx8-lab\n"
        "SysSerial   : 1234N-12345\n"
        "Recovery Mode: None\n"
        "MSM/MM      : MSM-A (Master) - Up 1 day\n"
        "PowerSupply : Internal-PS\n"
        "\nImage   : Version 30.7.1.4 build by release-manager\n"
    )
    drv = object.__new__(ExosDriver)
    drv.hostname = drv.username = drv.password = "test"
    drv.timeout = 60
    drv.device = FakeCLIDevice(mock_dir)
    facts = drv.get_facts()
    assert facts["model"] == "BlackDiamond X8"
