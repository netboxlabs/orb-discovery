"""Unit tests for custom_napalm.paloalto_panos_ssh.PANOSSHDriver."""

from pathlib import Path

from custom_napalm.paloalto_panos_ssh import PANOSSHDriver
from tests.custom_drivers.base_test import BaseDriverTest
from tests.custom_drivers.mock_device import FakeCLIDevice


class TestPANOSSHDriver(BaseDriverTest):
    """Unit tests for PANOSSHDriver using file-based CLI mocks."""

    driver_cls = PANOSSHDriver
    fake_device_cls = FakeCLIDevice
    mock_data_root = Path(__file__).parent / "mock_data"


def test_driver_exposes_get_modules():
    """PANOSSHDriver MUST expose a callable get_modules method."""
    assert hasattr(PANOSSHDriver, "get_modules")
    assert callable(PANOSSHDriver.get_modules)


def _run_both_panos_drivers(scenario: str):
    """Run XML and SSH drivers against the same scenario; return their envelopes."""
    from custom_napalm.paloalto_panos import PANOSDriver
    from tests.custom_drivers.mock_device import FakeXmlDevice

    xml_mock = (
        Path(__file__).parents[1] / "paloalto_panos" / "mock_data" / "test_get_modules" / scenario
    )
    xml_drv = object.__new__(PANOSDriver)
    xml_drv.hostname = xml_drv.username = xml_drv.password = "test"
    xml_drv.timeout = 60
    xml_drv.device = FakeXmlDevice(xml_mock)
    xml_envelope = xml_drv.get_modules()

    ssh_mock = Path(__file__).parent / "mock_data" / "test_get_modules" / scenario
    ssh_drv = object.__new__(PANOSSHDriver)
    ssh_drv.hostname = ssh_drv.username = ssh_drv.password = "test"
    ssh_drv.timeout = 60
    ssh_drv.device = FakeCLIDevice(ssh_mock)
    ssh_envelope = ssh_drv.get_modules()

    return xml_envelope, ssh_envelope


def test_get_modules_parity_pa7080_modular():
    """PA-7080: XML API and SSH envelopes must match."""
    xml, ssh = _run_both_panos_drivers("pa7080_modular")
    assert xml == ssh


def test_get_modules_parity_pa7500_modular():
    """PA-7500: XML API and SSH envelopes must match."""
    xml, ssh = _run_both_panos_drivers("pa7500_modular")
    assert xml == ssh


def test_get_modules_parity_pa5450_modular():
    """PA-5450: XML API and SSH envelopes must match."""
    xml, ssh = _run_both_panos_drivers("pa5450_modular")
    assert xml == ssh


def test_get_modules_parity_pa7050_single_smc():
    """PA-7050 single-SMC scenario: both transports must match."""
    xml, ssh = _run_both_panos_drivers("pa7050_single_smc")
    assert xml == ssh


def test_get_modules_parity_pa3220_fixed():
    """Both transports return None on fixed-config models."""
    xml, ssh = _run_both_panos_drivers("pa3220_fixed")
    assert xml is None
    assert ssh is None


def test_classify_module_type_panos_ssh_sku_substrings():
    """SSH-side classifier mirrors the XML side."""
    from custom_napalm.paloalto_panos_ssh import classify_module_type_panos_ssh
    assert classify_module_type_panos_ssh("PA-7000-100G-NPC-A") == "linecard"
    assert classify_module_type_panos_ssh("PA-7080-SMC") == "supervisor"
    assert classify_module_type_panos_ssh("PA-7050-SMC") == "supervisor"
    assert classify_module_type_panos_ssh("PA-5400-MPC-A") == "supervisor"
    assert classify_module_type_panos_ssh("PA-7500-SFC-A") == "linecard"
    assert classify_module_type_panos_ssh("PA-5400-NC-A") == "linecard"
    assert classify_module_type_panos_ssh("PA-5440-PSU") == "other"


def test_is_modular_panos_ssh_prefixes_and_variants():
    """Bare-prefix and suffixed model strings classify as modular."""
    from custom_napalm.paloalto_panos_ssh import _is_modular_panos_ssh
    assert _is_modular_panos_ssh("PA-7080") is True
    assert _is_modular_panos_ssh("PA-7050B") is True
    assert _is_modular_panos_ssh("PA-5450-AC") is True
    assert _is_modular_panos_ssh("PA-3220") is False
    assert _is_modular_panos_ssh("") is False


def test_panos_sku_classifier_tables_in_sync():
    """XML and SSH SKU classifier tables MUST be tuple-equal (Approach A guard)."""
    from custom_napalm.paloalto_panos import _PANOS_SKU_CLASSIFIER
    from custom_napalm.paloalto_panos_ssh import _PANOS_SSH_SKU_CLASSIFIER
    assert _PANOS_SKU_CLASSIFIER == _PANOS_SSH_SKU_CLASSIFIER


def test_panos_sku_classifier_ordering_invariant():
    """Any classifier token that is a substring of another must appear AFTER the longer one."""
    from custom_napalm.paloalto_panos import _PANOS_SKU_CLASSIFIER
    tokens = [t for t, _ in _PANOS_SKU_CLASSIFIER]
    for i, shorter in enumerate(tokens):
        for j, longer in enumerate(tokens):
            if i == j or shorter not in longer or shorter == longer:
                continue
            # shorter is a strict substring of longer → longer must come first
            assert j < i, (
                f"Ordering violation: {longer!r} (index {j}) contains "
                f"{shorter!r} (index {i}) as substring — the longer/more-specific "
                f"token must be checked first."
            )


def test_panos_modular_prefixes_in_sync():
    """The XML and SSH model-prefix tables must stay tuple-equal (Approach A guard)."""
    from custom_napalm.paloalto_panos import _MODULAR_PANOS_PREFIXES
    from custom_napalm.paloalto_panos_ssh import _MODULAR_PANOS_PREFIXES_SSH
    assert _MODULAR_PANOS_PREFIXES == _MODULAR_PANOS_PREFIXES_SSH
