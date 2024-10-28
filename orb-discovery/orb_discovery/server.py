#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Orb Discovery Server."""

from typing import Union
from fastapi import FastAPI
from datetime import datetime, timedelta
from orb_discovery.discovery import supported_drivers

app = FastAPI()
start_time = datetime.now()

@app.get("/api/v1/status")
def read_status():
    return {"UpTime": datetime.now() - start_time}

@app.get("/api/v1/capabilities")
def read_capabilities():
    return {"SupportedDrivers": supported_drivers}

@app.get("/api/v1/policies")
def read_policies(item_id: int, q: Union[str, None] = None):
    return {"item_id": item_id, "q": q}

@app.get("/api/v1/policies/{policy_name}")
def read_policy(item_id: int, q: Union[str, None] = None):
    return {"item_id": item_id, "q": q}

@app.post("/api/v1/policies/{policy_name}")
def write_policy(item_id: int, q: Union[str, None] = None):
    return {"item_id": item_id, "q": q}


@app.delete("/api/v1/policies/{policy_name}")
def delete_policy(item_id: int, q: Union[str, None] = None):
    return {"item_id": item_id, "q": q}

