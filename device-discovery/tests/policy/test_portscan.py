#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""NetBox Labs - Port scanning helpers tests."""

from unittest.mock import MagicMock

import device_discovery.policy.portscan as portscan


def test_expand_hostnames_range_sorted():
    """Ensure IP range expansion returns sorted, inclusive hosts."""
    hosts, parsed_as_range = portscan.expand_hostnames("10.0.0.3-10.0.0.1")

    assert parsed_as_range is True
    assert hosts == ["10.0.0.1", "10.0.0.2", "10.0.0.3"]


def test_expand_hostnames_cidr_and_single_host():
    """Ensure CIDR ranges and /32 addresses are expanded correctly."""
    hosts, parsed_as_range = portscan.expand_hostnames("192.0.2.0/30")
    assert parsed_as_range is True
    assert hosts == ["192.0.2.1", "192.0.2.2"]

    hosts, parsed_as_range = portscan.expand_hostnames("192.0.2.10/32")
    assert parsed_as_range is True
    assert hosts == ["192.0.2.10"]


def test_expand_hostnames_invalid_range_returns_original():
    """Invalid ranges fall back to the original hostname."""
    hosts, parsed_as_range = portscan.expand_hostnames("router-alpha-beta")

    assert parsed_as_range is False
    assert hosts == ["router-alpha-beta"]


def test_has_reachable_port_returns_true_for_any_reachable(monkeypatch):
    """Should return True when any probed port is reachable."""
    calls: list[tuple[str, int, float]] = []

    def fake_probe(hostname, port, timeout):
        calls.append((hostname, port, timeout))
        return port == 443

    monkeypatch.setattr(portscan, "_probe_port", fake_probe)

    reachable = portscan.has_reachable_port("example.com", [22, 443, 443], 1.0)

    assert reachable is True
    probed_ports = {port for _, port, _ in calls}
    assert probed_ports == {22, 443}


def test_has_reachable_port_handles_exceptions(monkeypatch):
    """Exceptions during probing are ignored and treated as unreachable."""

    def flaky_probe(hostname, port, timeout):
        if port == 22:
            raise OSError("connection refused")
        return False

    monkeypatch.setattr(portscan, "_probe_port", flaky_probe)

    reachable = portscan.has_reachable_port("example.com", [22, 80], 0.1)

    assert reachable is False


def test_has_reachable_port_with_no_ports(monkeypatch):
    """No ports configured should skip probing and return False."""
    mock_probe = MagicMock()
    monkeypatch.setattr(portscan, "_probe_port", mock_probe)

    reachable = portscan.has_reachable_port("example.com", [], 1.0)

    assert reachable is False
    mock_probe.assert_not_called()
