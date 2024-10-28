import argparse
import sys

import uvicorn
from importlib.metadata import version
from dotenv import load_dotenv
import netboxlabs.diode.sdk.version as SdkVersion

from orb_discovery.client import Client
from orb_discovery.parser import parse_config_file
from orb_discovery.server import app
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
        help="Display Diode Agent, NAPALM and Diode SDK versions",
    )
    parser.add_argument(
        "-c",
        "--config",
        metavar="config.yaml",
        help="Agent yaml configuration file",
        type=str,
        required=True,
    )
    parser.add_argument(
        "-e",
        "--env",
        metavar=".env",
        help="File containing environment variables",
        type=str,
    )
    parser.add_argument(
        "-w",
        "--workers",
        metavar="N",
        help="Number of workers to be used",
        type=int,
        default=2,
    )
    args = parser.parse_args()

    if hasattr(args, "env") and args.env is not None:
        if not load_dotenv(args.env, override=True):
            sys.exit(
                f"ERROR: Unable to load environment variables from file {args.env}"
            )

    
    cfg = parse_config_file(args.config)
      
    try:
        client = Client()
        client.init_client(target=cfg.config.target, api_key=cfg.config.api_key)
        uvicorn.run(
        app,
        host="0.0.0.0",
        port=8096,
    )
    except (KeyboardInterrupt, RuntimeError):
        pass
    except Exception as e:
        sys.exit(f"ERROR: Unable to start discovery backend: {e}")
        
if __name__ == "__main__":
  main()