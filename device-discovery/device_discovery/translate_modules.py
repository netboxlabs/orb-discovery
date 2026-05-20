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
from typing import Any

from netboxlabs.diode.sdk.diode.v1 import ingester_pb2 as pb
from netboxlabs.diode.sdk.ingester import Entity, Manufacturer, Module, ModuleBay, ModuleType

from device_discovery.metrics import get_metric
from device_discovery.policy.models import Options

logger = logging.getLogger(__name__)


def emit_modules_if_requested(
    data: dict[str, Any],
    options: Options,
    device: pb.Device,
    entities: list,
) -> dict[str, pb.Module]:
    """
    Maybe-emit Module / ModuleBay entities for the discovered device.

    Reads ``data["modules"]`` (populated upstream in the runner via the
    driver's optional ``get_modules()`` extension) and
    ``data["chassis_members"]`` (the VC payload). Mutates ``entities``
    in place to append ModuleBay and Module entries. Returns a mapping
    from interface name to the Module the interface belongs to, so the
    interface builder can attach ``module=`` per entity.

    Returns an empty dict when:
      - ``options.discover_modules == "off"`` (default; zero behavior change).
      - ``data["modules"]`` is missing, ``None``, or has no bays.
      - The device is a virtual chassis member (VC-of-modular is deferred
        to a follow-up; a single WARNING is logged per cycle).
      - The payload is malformed in a way the helper missed (defensive).
    """
    if options.discover_modules == "off":
        return {}
    payload = data.get("modules")
    if not _payload_has_bays(payload):
        return {}
    if data.get("chassis_members"):
        device_name = device.name if device.HasField("name") else "<unknown>"
        logger.warning(
            "module discovery deferred for virtual chassis members "
            "— tracked as follow-up to OBS-1594",
            extra={
                "device": device_name,
                "members": len(data["chassis_members"].get("members", [])),
                "modules_dropped": len(payload["bays"]),
            },
        )
        _bump("modules_dropped", len(payload["bays"]), {"reason": "vc_of_modular"})
        return {}

    mode = options.discover_modules
    manufacturer_name = _manufacturer_from_device(device)
    iface_module_map: dict[str, pb.Module] = {}
    for bay_data in payload["bays"]:
        try:
            _emit_bay_recursive(
                bay_data=bay_data,
                parent_device=device,
                parent_module=None,
                mode=mode,
                manufacturer_name=manufacturer_name,
                entities=entities,
                iface_module_map=iface_module_map,
                interfaces_by_bay=payload.get("interfaces_by_bay", {}),
            )
        except Exception:
            logger.warning(
                "malformed module payload bay — skipping",
                extra={"bay": bay_data.get("name")},
                exc_info=True,
            )
            _bump("modules_dropped", 1, {"reason": "malformed"})
    return iface_module_map


def _bump(metric_name: str, value: int, attrs: dict[str, str]) -> None:
    """
    Increment an OTel counter when metrics are enabled; otherwise no-op.

    Kept narrow on purpose: ``get_metric`` returns ``None`` whenever
    ``setup_metrics_export`` has not been called (every test and the dry-
    run mode both leave it unconfigured), so wrapping the .add() call
    here saves every caller from writing the same guard.
    """
    counter = get_metric(metric_name)
    if counter is not None:
        counter.add(value, attrs)


def _payload_has_bays(payload: Any) -> bool:
    """Defensive guard: payload is a dict with a non-empty bays list."""
    if not isinstance(payload, dict):
        return False
    bays = payload.get("bays")
    return isinstance(bays, list) and len(bays) > 0


def _manufacturer_from_device(device: pb.Device) -> str:
    """
    Read the device's manufacturer name; fall back to a placeholder.

    Modules and ModuleTypes need a Manufacturer reference. v1 reuses the
    device's manufacturer for all installed modules — operators wanting
    per-module manufacturer overrides can add them in NetBox by hand.
    """
    try:
        return device.device_type.manufacturer.name
    except AttributeError:
        return "Unknown"


def _emit_bay_recursive(
    *,
    bay_data: dict,
    parent_device: pb.Device | None,
    parent_module: pb.Module | None,
    mode: str,
    manufacturer_name: str,
    entities: list,
    iface_module_map: dict[str, pb.Module],
    interfaces_by_bay: dict[str, list[str]],
) -> None:
    """
    Recursively emit a ModuleBay + Module + their nested sub-bays.

    Exactly one of ``parent_device`` or ``parent_module`` is set:
      - Top-level bays point at the Device (parent_device != None).
      - Sub-bays point at the parent Module (parent_module != None).

    ``linecards`` mode short-circuits: any bay whose module.type is
    ``"transceiver"`` is skipped entirely (including its sub_bays);
    interfaces that would have routed to a skipped sub-bay fall back to
    the nearest emitted ancestor in ``iface_module_map``.
    """
    module_data = bay_data["module"]
    if mode == "linecards" and module_data["type"] == "transceiver":
        return

    bay_kwargs: dict[str, Any] = {
        "name": bay_data["name"],
        "position": bay_data.get("position") or bay_data["name"],
    }
    if parent_device is not None:
        bay_kwargs["device"] = parent_device
    if parent_module is not None:
        bay_kwargs["module"] = parent_module
    bay = ModuleBay(**bay_kwargs)
    entities.append(Entity(module_bay=bay))
    _bump("module_bays_emitted", 1, {"vendor": manufacturer_name})

    module_type = ModuleType(
        manufacturer=Manufacturer(name=manufacturer_name),
        model=module_data["model"] or "Unknown",
    )
    module_kwargs: dict[str, Any] = {
        "module_bay": bay,
        "module_type": module_type,
        "serial": module_data["serial"],
    }
    if parent_device is not None:
        module_kwargs["device"] = parent_device
    if module_data.get("description"):
        module_kwargs["description"] = module_data["description"]
    module = Module(**module_kwargs)
    entities.append(Entity(module=module))
    _bump(
        "modules_emitted", 1,
        {"vendor": manufacturer_name, "type": module_data["type"]},
    )

    # Map interfaces owned by THIS bay (top-level or sub) to this module.
    # Deepest match wins because we walk parent first then sub-bays — a
    # later assignment for a deeper bay overwrites the earlier one.
    for ifname in interfaces_by_bay.get(bay_data["name"], []):
        iface_module_map[ifname] = module

    if mode == "linecards":
        # Drop all sub_bays in linecards mode regardless of type. The
        # interfaces that would have routed to those sub-bays stay on
        # the parent linecard module via the assignment loop above.
        return

    for sub_bay in module_data.get("sub_bays", []):
        _emit_bay_recursive(
            bay_data=sub_bay,
            parent_device=None,
            parent_module=module,
            mode=mode,
            manufacturer_name=manufacturer_name,
            entities=entities,
            iface_module_map=iface_module_map,
            interfaces_by_bay=interfaces_by_bay,
        )
