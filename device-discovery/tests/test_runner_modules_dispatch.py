"""
Test the runner's _collect_modules dispatch + VC-of-modular gate.

Calls ``PolicyRunner._collect_modules`` directly (rather than duplicating
the snippet) so the production logic is what's pinned. Covers:

- ``options.discover_modules == "off"`` → no driver call, no data mutation.
- driver lacks ``get_modules`` → no-op.
- happy path: callable, payload stored on data["modules"].
- VC-of-modular gate: when chassis_members payload is populated, skip
  the driver call entirely and log a WARNING. This is the runner-level
  defer that supersedes the (now-removed) translate-layer guard.
- driver raises → WARNING + data["modules"] = None, no propagation.
"""

import logging
from unittest.mock import MagicMock

from device_discovery.policy.models import Config, Defaults, Options
from device_discovery.policy.runner import PolicyRunner


def _runner(discover_modules: str = "off") -> PolicyRunner:
    """Build a minimal PolicyRunner with discover_modules pre-configured."""
    runner = PolicyRunner()
    runner.name = "test-policy"
    runner.config = Config(
        defaults=Defaults(),
        options=Options(discover_modules=discover_modules),  # type: ignore[arg-type]
    )
    return runner


def _mock_device(with_modules: bool):
    """Build a mock NAPALM device, optionally exposing get_modules()."""
    dev = MagicMock()
    if with_modules:
        dev.get_modules = MagicMock(return_value={
            "bays": [
                {
                    "name": "1",
                    "position": "1",
                    "module": {
                        "model": "C9400-LC-48U",
                        "serial": "FOC1",
                        "description": "",
                        "type": "linecard",
                        "sub_bays": [],
                    },
                },
            ],
            "interfaces_by_bay": {"1": ["Te1/0/1"]},
        })
    else:
        del dev.get_modules
    return dev


def test_collect_modules_skips_when_off() -> None:
    """discover_modules='off' → no driver call, no data mutation."""
    runner = _runner("off")
    dev = _mock_device(with_modules=True)
    data: dict = {}
    runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" not in data
    assert not dev.get_modules.called


def test_collect_modules_skips_when_driver_lacks_method() -> None:
    """When the driver does not expose get_modules, dispatch is a no-op."""
    runner = _runner("linecards")
    dev = _mock_device(with_modules=False)
    data: dict = {}
    runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" not in data


def test_collect_modules_calls_when_linecards() -> None:
    """discover_modules='linecards' → driver call, payload stored."""
    runner = _runner("linecards")
    dev = _mock_device(with_modules=True)
    data: dict = {}
    runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" in data
    assert data["modules"]["bays"][0]["module"]["serial"] == "FOC1"


def test_collect_modules_skips_for_virtual_chassis(caplog, monkeypatch) -> None:
    """
    When chassis_members is populated, skip get_modules() and log WARNING.

    Also pins the modules_dropped counter bump with reason=vc_of_modular —
    this is the production telemetry signal operators alert on, so a
    silent regression of the .add() call would be invisible from logs.
    """
    import device_discovery.policy.runner as runner_mod

    counter_calls: list[tuple[int, dict]] = []

    class _FakeCounter:
        def add(self, value, attrs):
            counter_calls.append((value, dict(attrs)))

    monkeypatch.setattr(
        runner_mod, "get_metric",
        lambda name: _FakeCounter() if name == "modules_dropped" else None,
    )

    runner = _runner("linecards")
    dev = _mock_device(with_modules=True)
    data: dict = {
        "chassis_members": {
            "members": [{"id": 1}, {"id": 2}],
            "domain": None,
        },
    }
    with caplog.at_level(logging.WARNING, logger="device_discovery.policy.runner"):
        runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" not in data
    assert not dev.get_modules.called
    assert any(
        "module discovery" in r.message and "virtual chassis" in r.message
        for r in caplog.records
    )
    assert counter_calls == [(1, {"reason": "vc_of_modular"})]


def test_collect_modules_swallows_driver_exception(caplog) -> None:
    """Driver raising → WARNING logged, data['modules']=None, no propagation."""
    runner = _runner("linecards")
    dev = MagicMock()
    dev.get_modules = MagicMock(side_effect=RuntimeError("boom"))
    data: dict = {}
    with caplog.at_level(logging.WARNING, logger="device_discovery.policy.runner"):
        runner._collect_modules(runner.config, dev, data, "host")
    assert data["modules"] is None
    assert any(
        "Error getting modules" in r.message and "boom" in r.message
        for r in caplog.records
    )
