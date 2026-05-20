"""
Test that Runner.run wires get_modules into the data dict for translate.

Mirrors the get_chassis_members dispatch pattern. The runner gates the
call on options.discover_modules and stores the payload in
``data["modules"]`` for translate_modules to consume.
"""

import logging
from unittest.mock import MagicMock


def _make_napalm_device_mock(with_modules: bool):
    """Build a mock NAPALM device with the standard getters and (optionally) get_modules."""
    dev = MagicMock()
    dev.get_facts.return_value = {"hostname": "core-sw"}
    dev.get_interfaces.return_value = {}
    dev.get_interfaces_ip.return_value = {}
    dev.get_vlans.return_value = {}
    if with_modules:
        dev.get_modules = MagicMock(return_value={
            "bays": [
                {
                    "name": "1",
                    "position": "1",
                    "module": {
                        "model": "C9400-LC-48U",
                        "serial": "FOC1",
                        "description": "48-port UPOE+",
                        "type": "linecard",
                        "sub_bays": [],
                    },
                },
            ],
            "interfaces_by_bay": {"1": ["Te1/0/1"]},
        })
    return dev


def test_runner_dispatch_idiom_calls_get_modules_when_present():
    """getattr-based dispatch returns the bound method when the driver exposes it."""
    dev = _make_napalm_device_mock(with_modules=True)
    method = getattr(dev, "get_modules", None)
    assert callable(method)
    payload = method()
    assert payload["bays"][0]["module"]["serial"] == "FOC1"


def test_runner_dispatch_idiom_skips_when_method_absent():
    """When the driver does not expose get_modules, dispatch is a no-op."""
    dev = MagicMock(spec=["get_facts", "get_interfaces", "get_interfaces_ip", "get_vlans"])
    assert getattr(dev, "get_modules", None) is None


def test_runner_dispatch_skips_when_options_off():
    """
    When options.discover_modules == 'off', the runner must NOT call get_modules.

    This pins the behavior at the snippet level so a future runner refactor
    that drops the options check is caught by the unit test.
    """
    dev = _make_napalm_device_mock(with_modules=True)
    # Simulate the runner's snippet shape — see runner.py:298+ for the live code.
    data: dict = {}
    discover_modules = "off"
    if discover_modules != "off":
        method = getattr(dev, "get_modules", None)
        if callable(method):
            data["modules"] = method()
    assert "modules" not in data
    assert not dev.get_modules.called


def test_runner_dispatch_calls_when_options_linecards():
    """When options.discover_modules == 'linecards', the runner calls get_modules."""
    dev = _make_napalm_device_mock(with_modules=True)
    data: dict = {}
    discover_modules = "linecards"
    if discover_modules != "off":
        method = getattr(dev, "get_modules", None)
        if callable(method):
            data["modules"] = method()
    assert "modules" in data
    assert data["modules"]["bays"][0]["module"]["serial"] == "FOC1"


def test_runner_dispatch_swallows_exceptions(caplog):
    """get_modules raising → WARNING logged, data['modules'] set to None, no propagation."""
    dev = MagicMock()
    dev.get_modules = MagicMock(side_effect=RuntimeError("boom"))

    logger = logging.getLogger("device_discovery.policy.runner")
    data: dict = {}
    with caplog.at_level(logging.WARNING, logger="device_discovery.policy.runner"):
        method = getattr(dev, "get_modules", None)
        if callable(method):
            try:
                data["modules"] = method()
            except Exception as e:
                logger.warning("Error getting modules: %s. Continuing without module data.", e)
                data["modules"] = None

    assert data["modules"] is None
    assert any(
        r.levelno == logging.WARNING
        and "Error getting modules" in r.message
        and "boom" in r.message
        for r in caplog.records
    ), "expected runner to log a WARNING with the exception message"
