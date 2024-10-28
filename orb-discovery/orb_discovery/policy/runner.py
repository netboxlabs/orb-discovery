#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Diode NAPALM Agent CLI."""

import logging
from napalm import get_network_driver
from orb_discovery.client import Client
from orb_discovery.discovery import discover_device_driver, supported_drivers
from orb_discovery.parser import (
    DiscoveryConfig,
    Napalm,
)

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)



class PolicyRunner:
    def __init__(self, info: Napalm, config: DiscoveryConfig):
        self.info = info
        self.config = config
        
    def status(self):
        pass
  
  
  
    def start(self):
        """
        Run the device driver code for a single info item.

        Args:
        ----
            info: Information data for the device.
            config: Configuration data containing site information.

        """
        if self.info.driver is None:
            logger.info(f"Hostname {self.info.hostname}: Driver not informed, discovering it")
            self.info.driver = discover_device_driver(self.info)
            if not self.info.driver:
                raise Exception(
                f"Hostname {self.info.hostname}: Not able to discover device driver"
            )
        elif self.info.driver not in supported_drivers:
            raise Exception(
            f"Hostname {self.info.hostname}: specified driver '{self.info.driver}' was not found in the current installed drivers list: "
            f"{supported_drivers}.\nHINT: If '{self.info.driver}' is a napalm community driver, try to perform the following command:"
            f"\n\n\tpip install napalm-{self.info.driver.replace('_', '-')}\n"
        )

        logger.info(f"Hostname {self.info.hostname}: Get driver '{self.info.driver}'")
        np_driver = get_network_driver(self.info.driver)
        logger.info(f"Hostname {self.info.hostname}: Getting information")
        with np_driver(
        self.info.hostname, self.info.username, self.info.password, self.info.timeout, self.info.optional_args
        ) as device:
            data = {
            "driver": self.info.driver,
            "site": self.config.netbox.get("site", None),
            "device": device.get_facts(),
            "interface": device.get_interfaces(),
            "interface_ip": device.get_interfaces_ip(),
        }
            Client().ingest(self.info.hostname, data)