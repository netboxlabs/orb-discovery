#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Policy Runner."""

import logging
from datetime import datetime, timedelta
from typing import Any

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger
from apscheduler.triggers.date import DateTrigger
from netboxlabs.diode.sdk import DiodeClient

from worker.backend import Backend, load_class
from worker.models import Config, DiodeConfig, Status

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


class PolicyRunner:
    """Policy Runner class."""

    def __init__(self):
        """Initialize the PolicyRunner."""
        self.name = ""
        self.scope = None
        self.policy_config = None
        self.status = Status.NEW
        self.scheduler = BackgroundScheduler()

    def setup(
        self, name: str, diode_config: DiodeConfig, policy_config: Config, scope: Any
    ):
        """
        Set up the policy runner.

        Args:
        ----
            name: Policy name.
            diode_config: Diode configuration data.
            policy_config: Policy configuration data.
            scope: scope data for the customs.

        """
        self.name = name.replace("\r\n", "").replace("\n", "")
        policy_config.package = policy_config.package.replace("\r\n", "").replace(
            "\n", ""
        )
        backend_class = load_class(policy_config.package)
        backend = backend_class()

        metadata = backend.setup()
        client = DiodeClient(
            target=diode_config.target,
            app_name=(
                f"{diode_config.prefix}/{metadata.app_name}"
                if diode_config.prefix
                else metadata.app_name
            ),
            app_version=metadata.app_version,
            api_key=diode_config.api_key,
        )

        self.policy_config = policy_config

        self.scheduler.start()

        if self.policy_config.schedule is not None:
            logger.info(
                f"Policy {self.name}, Package {self.policy_config.package}: Scheduled to run with '{self.policy_config.schedule}'"
            )
            trigger = CronTrigger.from_crontab(self.policy_config.schedule)
        else:
            logger.info(
                f"Policy {self.name}, Package {self.policy_config.package}: One-time run"
            )
            trigger = DateTrigger(run_date=datetime.now() + timedelta(seconds=1))

        self.scheduler.add_job(
            self.run,
            trigger=trigger,
            args=[client, backend, scope, self.policy_config],
        )

        self.status = Status.RUNNING

    def run(self, client: DiodeClient, backend: Backend, scope: Any, config: Config):
        """
        Run the custom backend code for the specified scope.

        Args:
        ----
            client: Diode client.
            backend: Backend class.
            scope: scope data for the custom.
            config: Configuration data containing site information.

        """
        try:
            entities = backend.run(config, scope)
            response = client.ingest(entities)
            if response.errors:
                logger.error(
                    f"ERROR ingestion failed for {self.name} : {response.errors}"
                )
            else:
                logger.info(f"Policy {self.name}: Successful ingestion")
        except Exception as e:
            logger.error(f"Policy {self.name}: {e}")

    def stop(self):
        """Stop the policy runner."""
        self.scheduler.shutdown()
        self.status = Status.FINISHED
