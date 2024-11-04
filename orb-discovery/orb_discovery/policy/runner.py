#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Orb Discovery Policy Runner."""

import logging
import uuid
from datetime import datetime, timedelta

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger
from napalm import get_network_driver

from orb_discovery.client import Client
from orb_discovery.discovery import discover_device_driver, supported_drivers
from orb_discovery.policy.models import Config, Napalm, Status

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class PolicyRunner:
    """Policy Runner class."""

    def __init__(self):
        """Initialize the PolicyRunner."""
        self.name = ""
        self.infos = dict[str, Napalm]()
        self.config = None
        self.current_status = Status.NEW
        self.scheduler = BackgroundScheduler()

    def status(self) -> Status:
        """
        Get the current status of the policy runner.

        Returns
        -------
            Status: The current status of the policy runner.

        """
        return self.current_status

    def setup(self, name: str, config: Config, infos: list[Napalm]):
        """
        Set up the policy runner.

        Args:
        ----
            name: Policy name.
            config: Configuration data containing site information.
            infos: Information data for the devices.

        """
        self.name = name
        self.config = config

        if self.config is None:
            self.config = Config(netbox={})
        elif self.config.netbox is None:
            self.config.netbox = {}

        self.scheduler.start()
        for info in infos:
            if info.driver and info.driver not in supported_drivers:
                self.scheduler.shutdown()
                raise Exception(
                    f"Hostname {info.hostname}: specified driver '{info.driver}' was not found in the "
                    f"current installed drivers list: {supported_drivers}."
                )

            if self.config.schedule is not None:
                trigger = CronTrigger.from_crontab(self.config.schedule)
            else:
                # Schedule a one-time job to run after 1 second
                trigger = DateTrigger(run_date=datetime.now() + timedelta(seconds=1))

            id = str(uuid.uuid4())
            self.infos[id] = info
            self.scheduler.add_job(
                self.run, id=id, trigger=trigger, args=[id, info, self.config]
            )

            self.current_status = Status.RUNNING

    def run(self, id: str, info: Napalm, config: Config):
        """
        Run the device driver code for a single info item.

        Args:
        ----
            id: Job ID.
            info: Information data for the device.
            config: Configuration data containing site information.

        """
        if info.driver is None:
            logger.info(
                f"Policy {self.name}, Hostname {info.hostname}: Driver not informed, discovering it"
            )
            info.driver = discover_device_driver(info)
            if info.driver is None:
                self.current_status = Status.FAILED
                logger.error(
                    f"Policy {self.name}, Hostname {info.hostname}: Not able to discover device driver"
                )
                try:
                    self.scheduler.remove_job(id)
                except Exception as e:
                    logger.error(
                        f"Policy {self.name}, Hostname {info.hostname}: Error removing job: {e}"
                    )
                return

        logger.info(
            f"Policy {self.name}, Hostname {info.hostname}: Get driver '{info.driver}'"
        )

        try:
            np_driver = get_network_driver(info.driver)
            logger.info(
                f"Policy {self.name}, Hostname {info.hostname}: Getting information"
            )
            with np_driver(
                info.hostname,
                info.username,
                info.password,
                info.timeout,
                info.optional_args,
            ) as device:
                data = {
                    "driver": info.driver,
                    "site": config.netbox.get("site", None),
                    "device": device.get_facts(),
                    "interface": device.get_interfaces(),
                    "interface_ip": device.get_interfaces_ip(),
                }
                Client().ingest(info.hostname, data)
        except Exception as e:
            logger.error(f"Policy {self.name}, Hostname {info.hostname}: {e}")

    def stop(self):
        """Stop the policy runner."""
        self.scheduler.shutdown()
        self.current_status = Status.FINISHED
