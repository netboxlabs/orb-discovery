#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Attach discovery run_id to per-entity Diode metadata (protobuf Struct)."""

from __future__ import annotations

from typing import Iterable


def apply_run_id_to_entities(entities: Iterable, run_id: str | object) -> None:
    """
    Merge ``run_id`` into the ``metadata`` google.protobuf.Struct on each
    ``ingester_pb2.Entity``'s populated inner message.

    Mirrors snmp-discovery ``annotateEntitiesWithRunID`` for the Python SDK.
    """
    rid = str(run_id)
    for ent in entities:
        which = getattr(ent, "WhichOneof", None)
        if not callable(which):
            continue
        field = which("entity")
        if not field:
            continue
        inner = getattr(ent, field)
        inner.metadata.update({"run_id": rid})
