#!/usr/bin/env python
# Copyright 2026 NetBox Labs Inc
"""
NetBox Labs - Device Discovery - translate vendor-neutral module payloads.

Translate vendor-neutral module payloads (from custom_napalm._modules) into
NetBox Module + ModuleBay entities.

Called from device_discovery.translate.translate_data in the standalone-
Device branch — VC-of-modular composition is deferred; the translate-
chassis path runs untouched.

Public entry point: emit_modules_if_requested(...). Returns an
``iface_module_map: dict[str, Module]`` that the interface builder
consumes to attach ``module=`` on each interface alongside ``device=``.
"""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING, Any

from device_discovery.policy.models import Options

if TYPE_CHECKING:
    from netboxlabs.diode.sdk.ingester import Device, Entity, Module

logger = logging.getLogger(__name__)


def emit_modules_if_requested(
    data: dict[str, Any],
    options: Options,
    device: Device,
    entities: list[Entity],
) -> dict[str, Module]:
    """
    Maybe-emit Module / ModuleBay entities for the discovered device.

    Reads ``data["modules"]`` (populated upstream in the runner via the
    driver's optional ``get_modules()`` extension) and ``data["chassis_members"]``
    (the VC payload). Mutates ``entities`` in place to append ModuleBay
    and Module entries. Returns a mapping from interface name to the
    Module the interface belongs to, so the interface builder can
    attach ``module=`` per entity.

    Returns an empty dict when:
      - ``options.discover_modules == "off"`` (default; zero behavior change).
      - ``data["modules"]`` is missing, ``None``, or has no bays.
      - The device is a virtual chassis member (VC-of-modular is deferred
        to a follow-up; a single WARNING is logged per cycle).
      - The payload is malformed in a way the helper missed (defensive).
    """
    if options.discover_modules == "off":
        return {}
    # Full implementation lands in Task 9 — see plan and spec.
    raise NotImplementedError("emit_modules_if_requested body lands in Task 9")
