#!/usr/bin/env python
# Copyright 2026 NetBox Labs Inc
"""Unit tests for device_discovery.translate_modules."""

from __future__ import annotations

from unittest.mock import MagicMock

from device_discovery.policy.models import Options
from device_discovery.translate_modules import emit_modules_if_requested


def test_off_mode_returns_empty_map_and_does_not_touch_entities() -> None:
    """When discover_modules == 'off', no payload is read and entities is unchanged."""
    entities: list = []
    fake_device = MagicMock()
    data = {"modules": {"bays": [{"name": "1"}], "interfaces_by_bay": {"1": ["Te1/0/1"]}}}

    iface_module_map = emit_modules_if_requested(
        data, Options(discover_modules="off"), fake_device, entities,
    )

    assert iface_module_map == {}
    assert entities == []
