"""Unit tests for custom_napalm.nokia_sros.SROSDriver."""

from pathlib import Path

from custom_napalm.nokia_sros import SROSDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeNetconfConn


class TestSROSDriver(BaseDriverTest):
    """Unit tests for SROSDriver using file-based NETCONF XML mocks."""

    driver_cls = SROSDriver
    fake_device_cls = FakeNetconfConn
    mock_data_root = Path(__file__).parent / "mock_data"

    def _build_driver(self, mock_dir: Path) -> SROSDriver:
        """Instantiate SROSDriver with a fake NETCONF connection (no real device needed)."""
        driver = object.__new__(SROSDriver)
        driver.hostname = "test-host"
        driver.username = "test-user"
        driver.password = "test-pass"
        driver.timeout = 60
        driver.R19 = False
        driver.conn = FakeNetconfConn(mock_dir)
        return driver


def test_driver_exposes_get_modules():
    """Driver MUST expose a callable get_modules method."""
    from custom_napalm.nokia_sros import SROSDriver
    assert hasattr(SROSDriver, "get_modules")
    assert callable(SROSDriver.get_modules)


# ---------------------------------------------------------------------------
# Transceiver attachment — port-id form coverage
# ---------------------------------------------------------------------------


def _build_mda_bay_map():
    """Construct a single MDA bay map for transceiver-attachment unit tests."""
    from custom_napalm._modules import ModuleBay, ModuleEntry
    parent = ModuleBay(
        name="1", position="1",
        module=ModuleEntry(model="IOM", serial="SN-IOM-1", type="linecard"),
    )
    mda = ModuleBay(
        name="1/1", position="1/1",
        module=ModuleEntry(model="MDA", serial="SN-MDA-1", type="linecard"),
    )
    parent.module.sub_bays.append(mda)
    return {"1/1": mda}, parent


def test_attach_transceiver_classic_3_segment_port_id():
    """Classic slot/mda/port (1/1/1) emits card-slot, MDA-path, and per-port keys."""
    from custom_napalm.nokia_sros import _nokia_sros_attach_transceiver_sub_bays
    mda_map, _ = _build_mda_bay_map()
    rows = [{"port_id": "1/1/1", "model": "SFP-10G-LR", "sn": "OPT1"}]
    ifaces = _nokia_sros_attach_transceiver_sub_bays(rows, mda_map)
    assert ifaces == {"1": ["1/1/1"], "1/1": ["1/1/1"], "1/1/1": ["1/1/1"]}
    assert mda_map["1/1"].module.sub_bays[0].name == "1/1/1"


def test_attach_transceiver_fp4_c_cage_port_id():
    """FP4 connector-cage (1/1/c2/1) emits the same three keys as classic form."""
    from custom_napalm.nokia_sros import _nokia_sros_attach_transceiver_sub_bays
    mda_map, _ = _build_mda_bay_map()
    rows = [{"port_id": "1/1/c2/1", "model": "QSFP28-SR4", "sn": "OPT2"}]
    ifaces = _nokia_sros_attach_transceiver_sub_bays(rows, mda_map)
    assert ifaces == {"1": ["1/1/c2/1"], "1/1": ["1/1/c2/1"], "1/1/c2/1": ["1/1/c2/1"]}
    assert mda_map["1/1"].module.sub_bays[0].name == "1/1/c2/1"


def test_attach_transceiver_card_slot_key_aggregates_across_mdas():
    """Multiple transceivers under the same card aggregate into one card-slot key."""
    from custom_napalm._modules import ModuleBay, ModuleEntry
    from custom_napalm.nokia_sros import _nokia_sros_attach_transceiver_sub_bays
    parent = ModuleBay(name="1", position="1",
                       module=ModuleEntry(model="IOM", serial="SN-1", type="linecard"))
    mda1 = ModuleBay(name="1/1", position="1/1",
                     module=ModuleEntry(model="MDA", serial="SN-MDA1", type="linecard"))
    mda2 = ModuleBay(name="1/2", position="1/2",
                     module=ModuleEntry(model="MDA", serial="SN-MDA2", type="linecard"))
    parent.module.sub_bays.extend([mda1, mda2])
    mda_map = {"1/1": mda1, "1/2": mda2}
    rows = [
        {"port_id": "1/1/1", "model": "SFP-10G-LR", "sn": "OPT-A"},
        {"port_id": "1/2/1", "model": "QSFP-100G-SR4", "sn": "OPT-B"},
    ]
    ifaces = _nokia_sros_attach_transceiver_sub_bays(rows, mda_map)
    # Card-slot key "1" aggregates ports from both MDAs — critical for
    # linecards-mode routing which never descends into sub-bays.
    assert ifaces["1"] == ["1/1/1", "1/2/1"]
    assert ifaces["1/1"] == ["1/1/1"]
    assert ifaces["1/2"] == ["1/2/1"]


def test_attach_transceiver_unknown_mda_path_dropped():
    """A port-id whose slot/mda doesn't match any known MDA is silently dropped."""
    from custom_napalm.nokia_sros import _nokia_sros_attach_transceiver_sub_bays
    mda_map, _ = _build_mda_bay_map()
    rows = [{"port_id": "9/9/1", "model": "SFP-X", "sn": "OPT3"}]
    ifaces = _nokia_sros_attach_transceiver_sub_bays(rows, mda_map)
    assert ifaces == {}
    assert mda_map["1/1"].module.sub_bays == []
