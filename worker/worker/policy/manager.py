#!/usr/bin/env python
# Copyright 2025 NetBox Labs Inc
"""Orb Worker Policy Manager."""

import logging
import os

import yaml

from worker.models import DiodeConfig, Policy, PolicyRequest
from worker.policy.job import JobStore
from worker.policy.runner import PolicyRunner

# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def resolve_env_vars(config):
    """
    Recursively resolve environment variables in the configuration.

    Args:
    ----
        config (dict): The configuration dictionary.

    Returns:
    -------
        dict: The configuration dictionary with environment variables resolved.

    """
    if isinstance(config, dict):
        return {k: resolve_env_vars(v) for k, v in config.items()}
    if isinstance(config, list):
        return [resolve_env_vars(i) for i in config]
    if isinstance(config, str) and config.startswith("${") and config.endswith("}"):
        env_var = config[2:-1]
        return os.getenv(env_var, config)
    return config


class PolicyManager:
    """Policy Manager class."""

    def __init__(self):
        """Initialize the PolicyManager instance with an empty list of policies."""
        self.runners = dict[str, PolicyRunner]()
        self.config = None
        self.loaded_modules = set()
        self.job_store = JobStore()

    def get_loaded_modules(self):
        """Return the loaded modules."""
        return self.loaded_modules

    def setup(self, config: DiodeConfig):
        """
        Set up the Policy Manager.

        Args:
        ----
            config(DiodeConfig): The Diode configuration.

        """
        self.config = config

    def start_policy(self, name: str, policy: Policy):
        """
        Start the policy for the given configuration.

        Args:
        ----
            name: Policy name
            policy: Policy configuration

        """
        if self.policy_exists(name):
            raise ValueError(f"policy '{name}' already exists")

        runner = PolicyRunner()
        runner.setup(name, self.config, policy, self.job_store)
        self.loaded_modules.add(policy.config.package)
        self.runners[name] = runner

    def parse_policy(self, config_data: bytes) -> PolicyRequest:
        """
        Parse the YAML configuration data into a Policy object.

        Args:
        ----
            config_data (str): The YAML configuration data as a string.

        Returns:
        -------
            Config: The configuration object.

        """
        config = yaml.safe_load(config_data)
        config = resolve_env_vars(config)
        return PolicyRequest(**config)

    def policy_exists(self, name: str) -> bool:
        """
        Check if the policy exists.

        Args:
        ----
            name: Policy name

        Returns:
        -------
            bool: True if the policy exists, False otherwise

        """
        return name in self.runners

    def delete_policy(self, name: str):
        """
        Delete the policy by name.

        Args:
        ----
            name: Policy name.

        """
        if not self.policy_exists(name):
            raise ValueError(f"policy '{name}' not found")
        self.runners[name].stop()
        del self.runners[name]

    def stop(self):
        """Stop all running policies."""
        for name, runner in self.runners.items():
            logger.info(f"Stopping policy '{name}'")
            runner.stop()
        self.runners = {}

    def get_policy_statuses(self) -> list[dict]:
        """
        Get all policies with their status and jobs.

        Returns
        -------
            list[dict]: List of policy status dictionaries with name, status, and jobs.

        """
        all_jobs = self.job_store.get_all_policies_with_jobs()
        statuses = []

        # Get statuses for all policies that have runners
        for name in self.runners:
            jobs = self.job_store.get_jobs_for_policy(name)
            status = "unknown"
            if len(jobs) > 0:
                latest_job = jobs[-1]
                status = latest_job.status.value
            statuses.append(
                {
                    "name": name,
                    "status": status,
                    "jobs": [
                        {
                            "id": job.id,
                            "status": job.status.value,
                            "reason": job.reason,
                            "entity_count": job.entity_count,
                            "created_at": job.created_at.isoformat(),
                            "updated_at": job.updated_at.isoformat(),
                        }
                        for job in jobs
                    ],
                }
            )

        # Also include policies that have jobs but no active runner
        for name, jobs in all_jobs.items():
            if name not in self.runners:
                status = "unknown"
                if len(jobs) > 0:
                    latest_job = jobs[-1]
                    status = latest_job.status.value
                statuses.append(
                    {
                        "name": name,
                        "status": status,
                        "jobs": [
                            {
                                "id": job.id,
                                "status": job.status.value,
                                "reason": job.reason,
                                "entity_count": job.entity_count,
                                "created_at": job.created_at.isoformat(),
                                "updated_at": job.updated_at.isoformat(),
                            }
                            for job in jobs
                        ],
                    }
                )

        return statuses
