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
    """
    Build a mock NAPALM device, optionally exposing get_modules().

    Uses ``spec=`` so attribute access for anything outside the allowed
    list raises AttributeError instead of auto-creating a child mock.
    That makes the ``with_modules=False`` case test the production
    contract — ``getattr(device, "get_modules", None)`` must return
    ``None`` for drivers that don't implement the extension.
    """
    base_attrs = ["get_facts", "get_interfaces", "get_interfaces_ip", "get_vlans"]
    if with_modules:
        dev = MagicMock(spec=[*base_attrs, "get_modules"])
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
        dev = MagicMock(spec=base_attrs)
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


def test_collect_modules_runs_for_single_member_chassis_payload() -> None:
    """
    A 1-member chassis_members payload is NOT a virtual chassis.

    translate_chassis.validate_chassis_payload only treats N>=2 as a
    real VC. The runner's VC gate must align so a standalone modular
    chassis whose driver returns a single-member chassis_members
    payload still gets its modules discovered.
    """
    runner = _runner("linecards")
    dev = _mock_device(with_modules=True)
    data: dict = {
        "chassis_members": {
            "members": [{"id": 1, "serial": "ABC123"}],
            "domain": None,
        },
    }
    runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" in data  # get_modules was called
    assert dev.get_modules.called


def test_collect_modules_runs_for_empty_members_chassis_payload() -> None:
    """An empty members list is not a VC and must not suppress module discovery."""
    runner = _runner("linecards")
    dev = _mock_device(with_modules=True)
    data: dict = {"chassis_members": {"members": [], "domain": None}}
    runner._collect_modules(runner.config, dev, data, "host")
    assert "modules" in data
    assert dev.get_modules.called


def test_collect_modules_vc_gate_tolerates_non_dict_payload(caplog) -> None:
    """
    A driver returning a non-dict chassis_members payload must not crash.

    Behavior post-fix: non-dict payloads are NOT a VC (members are
    treated as empty), so module discovery proceeds normally rather
    than being suppressed. The fix gates on isinstance(dict) before
    reading members and only short-circuits when len(members) >= 2.
    """
    runner = _runner("linecards")
    dev = _mock_device(with_modules=True)
    # A buggy driver might return a list — must not crash the runner.
    data: dict = {"chassis_members": ["not", "a", "dict"]}
    with caplog.at_level(logging.WARNING, logger="device_discovery.policy.runner"):
        runner._collect_modules(runner.config, dev, data, "host")
    # Not a VC → module discovery proceeds, no WARNING.
    assert "modules" in data
    assert dev.get_modules.called
    assert not any(
        "virtual chassis" in r.message for r in caplog.records
    )


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
