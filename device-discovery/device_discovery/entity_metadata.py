#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Attach discovery run_id to per-entity Diode metadata (protobuf Struct).

Duplicated from worker/worker/entity_metadata.py — keep in sync when changing logic.
"""

from __future__ import annotations

from typing import Iterable


def apply_run_id_to_entities(entities: Iterable, run_id: str | object) -> None:
    """
    Merge ``run_id`` into the ``metadata`` google.protobuf.Struct on each
    ``ingester_pb2.Entity``'s populated inner message.
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
