#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Orb Discovery entry point."""

import argparse
import sys

import uvicorn
from importlib.metadata import version
import netboxlabs.diode.sdk.version as SdkVersion

from orb_discovery.client import Client
from orb_discovery.parser import parse_config_file
from orb_discovery.server import app, manager
from orb_discovery.version import version_semver

def main():
    """
    Main entry point for the Agent CLI.

    Parses command-line arguments and starts the backend.
    """
    parser = argparse.ArgumentParser(description="Diode Agent for NAPALM")
    parser.add_argument(
        "-V",
        "--version",
        action="version",
        version=f"Discovery version: {version_semver()}, NAPALM version: {version('napalm')}, "
        f"Diode SDK version: {SdkVersion.version_semver()}",
        help="Display Discovery, NAPALM and Diode SDK versions",
    )
    parser.add_argument(
        "-c",
        "--config",
        metavar="config.yaml",
        help="Agent yaml configuration file",
        type=str,
        required=True,
    )
    args = parser.parse_args()

    try:
        cfg = parse_config_file(args.config)
        client = Client()
        client.init_client(target=cfg.config.target, api_key=cfg.config.api_key)
        manager.update_workers(cfg.config.workers)
        uvicorn.run(
        app,
        host=cfg.config.host,
        port=cfg.config.port,
    )
    except (KeyboardInterrupt, RuntimeError):
        pass
    except Exception as e:
        sys.exit(f"ERROR: Unable to start discovery backend: {e}")
        
if __name__ == "__main__":
  main()