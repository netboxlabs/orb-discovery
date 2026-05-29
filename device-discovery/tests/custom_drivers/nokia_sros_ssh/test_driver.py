"""Unit tests for custom_napalm.nokia_sros_ssh.SROSSSHDriver."""

from pathlib import Path

from custom_napalm.nokia_sros_ssh import SROSSSHDriver, _parse_port_hw_mac_addresses
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestSROSSSHDriver(BaseDriverTest):
    """Unit tests for SROSSSHDriver using file-based CLI mocks."""

    driver_cls = SROSSSHDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"


# ---------------------------------------------------------------------------
# _parse_port_hw_mac_addresses — parser unit tests
# ---------------------------------------------------------------------------


_SHOW_PORT_DETAIL_TWO_PORTS = """\
===============================================================================
Ethernet Interface
===============================================================================
Interface          : 1/1/1                      Oper Speed       : 1 Gbps
Admin State        : up                         Oper Duplex      : full
Configured Mac     : 90:ec:00:00:00:01
Hardware Mac       : 90:ec:00:00:00:01
===============================================================================
===============================================================================
Ethernet Interface
===============================================================================
Interface          : 1/1/2                      Oper Speed       : 1 Gbps
Admin State        : up                         Oper Duplex      : full
Configured Mac     : 90:ec:00:00:00:02
Hardware Mac       : 90:ec:00:00:00:02
===============================================================================
"""


def test_parse_port_hw_mac_addresses_typical_multi_block():
    """Two consecutive port blocks → port_id → normalised MAC dict."""
    result = _parse_port_hw_mac_addresses(_SHOW_PORT_DETAIL_TWO_PORTS)
    assert result == {
        "1/1/1": "90:EC:00:00:00:01",
        "1/1/2": "90:EC:00:00:00:02",
    }


def test_parse_port_hw_mac_addresses_empty_input_returns_empty():
    """Empty / None input never raises — empty dict result."""
    assert _parse_port_hw_mac_addresses("") == {}
    assert _parse_port_hw_mac_addresses(None) == {}  # type: ignore[arg-type]


def test_parse_port_hw_mac_addresses_skips_block_without_hw_mac():
    """A block with Interface but no Hardware Mac row is silently skipped."""
    text = """\
===============================================================================
Ethernet Interface
===============================================================================
Interface          : 1/1/9                      Oper Speed       : N/A
Admin State        : down
Configured Mac     : N/A
===============================================================================
===============================================================================
Ethernet Interface
===============================================================================
Interface          : 1/1/10                     Oper Speed       : 10 Gbps
Hardware Mac       : 90:ec:00:00:00:0a
===============================================================================
"""
    # 1/1/9 lacks a Hardware Mac row → skipped. 1/1/10 still resolves.
    assert _parse_port_hw_mac_addresses(text) == {"1/1/10": "90:EC:00:00:00:0A"}


def test_parse_port_hw_mac_addresses_prefers_hardware_over_configured():
    """When both ``Configured Mac`` and ``Hardware Mac`` are present, only Hardware is taken."""
    text = """\
===============================================================================
Ethernet Interface
===============================================================================
Interface          : 1/1/1
Configured Mac     : aa:bb:cc:dd:ee:ff
Hardware Mac       : 90:ec:00:00:00:01
===============================================================================
"""
    assert _parse_port_hw_mac_addresses(text) == {"1/1/1": "90:EC:00:00:00:01"}


def test_parse_port_hw_mac_addresses_md_cli_hardware_address_label():
    """MD-CLI (SR-OS 19+) uses ``Hardware Address`` instead of ``Hardware Mac``."""
    text = """\
===============================================================================
Ethernet Interface
===============================================================================
Interface         : 1/1/c2/1                   Oper Speed         : 10 Gbps
Configured Address: aa:bb:cc:dd:ee:42
Hardware Address  : 90:ec:00:00:00:42
===============================================================================
"""
    assert _parse_port_hw_mac_addresses(text) == {"1/1/c2/1": "90:EC:00:00:00:42"}


def test_parse_port_hw_mac_addresses_accepts_non_padded_mac():
    """napalm.mac() accepts ``aa:bb:cc:dd:ee:1`` and pads to ``AA:BB:CC:DD:EE:01`` — regex must allow shorter form too."""
    text = """\
===============================================================================
Interface          : 1/1/1
Hardware Mac       : aa:bb:cc:dd:ee:1
===============================================================================
"""
    assert _parse_port_hw_mac_addresses(text) == {"1/1/1": "AA:BB:CC:DD:EE:01"}


def test_driver_exposes_get_modules():
    """SSH driver MUST expose a callable get_modules method."""
    assert hasattr(SROSSSHDriver, "get_modules")
    assert callable(SROSSSHDriver.get_modules)


def test_get_modules_parity_with_netconf():
    """Run both drivers against the sr12_full fixtures; assert envelopes match."""
    from custom_napalm.nokia_sros import SROSDriver
    from tests.custom_drivers.mock_device import FakeNetconfConn

    netconf_mock = Path(__file__).parents[1] / "nokia_sros" / "mock_data" / "test_get_modules" / "sr12_full"
    netconf_drv = object.__new__(SROSDriver)
    netconf_drv.hostname = netconf_drv.username = netconf_drv.password = "test"
    netconf_drv.timeout = 60
    netconf_drv.conn = FakeNetconfConn(netconf_mock)
    netconf_envelope = netconf_drv.get_modules()

    ssh_mock = Path(__file__).parent / "mock_data" / "test_get_modules" / "sr12_full"
    ssh_drv = object.__new__(SROSSSHDriver)
    ssh_drv.hostname = ssh_drv.username = ssh_drv.password = "test"
    ssh_drv.timeout = 60
    ssh_drv.device = FakeCLIDevice(ssh_mock)
    ssh_envelope = ssh_drv.get_modules()

    assert netconf_envelope == ssh_envelope
