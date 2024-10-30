#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Orb Discovery Server."""

from typing import Union
from fastapi import FastAPI
from fastapi.responses import Response
from datetime import datetime
from orb_discovery.parser import Base
from orb_discovery.discovery import supported_drivers
from orb_discovery.version import version_display
from orb_discovery.policy.manager import PolicyManager

app = FastAPI()
manager = PolicyManager()
start_time = datetime.now()

@app.get("/api/v1/status")
def read_status():
    return {"version" : version_display(), "up_time": datetime.now() - start_time}

@app.get("/api/v1/capabilities")
def read_capabilities():
    return {"supported_drivers": supported_drivers}

@app.post("/api/v1/policies", response_class=Response)
def write_policy(base: Base):
    return {"item_id": base}


@app.delete("/api/v1/policies/{policy_name}")
def delete_policy(policy_name: str):
    try:
        item_id = manager.delete_policy(policy_name)
        q = "Policy deleted"
    except KeyError:
        item_id = None
        q = "Policy not found"
    return {"item_id": item_id, "q": q}
