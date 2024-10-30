#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""Orb Discovery Policy Manager."""

import logging
import yaml
from concurrent.futures import ThreadPoolExecutor, as_completed

from orb_discovery.parser import resolve_env_vars, ParseException, Policy
from orb_discovery.policy.runner import PolicyRunner


# Set up logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class PolicyManager:
    def __init__(self):
        self.max_workers = 2
        self.runners = []
    
    
    def update_workers(self, max_workers: int):
        """
        Update the maximum number of workers in the pool.
        """
        self.max_workers = max_workers   


    def start_policy(self, cfg: str):
        """
        Start the policy for the given configuration.

        Args:
        ----
            name: Policy name
            cfg: Configuration data for the policy.
            max_workers: Maximum number of threads in the pool.
        """
        pass        
        # with ThreadPoolExecutor(max_workers=self.max_workers) as executor:
        #     futures = [executor.submit(run_driver, info, cfg.config) for info in cfg.data]

        # for future in as_completed(futures):
        #     try:
        #         future.result()
        #     except Exception as e:
        #         logger.error(f"Error while processing policy {name}: {e}")

        
    def parse_policy(config_data: str) -> Policy:
        """
        Parse the YAML configuration data into a Policy object.
    
        Args:
        ----
            config_data (str): The YAML configuration data as a string.
        
        Returns:
        -------
            Config: The configuration object.
        
        """
        try:
            config = yaml.safe_load(config_data)
            config = resolve_env_vars(config)
            return Policy(**config)
        except Exception as e:
            raise ParseException(f"Error parsing configuration: {e}")
        
    def delete_policy(self, name: str):
            """
            Delete the policy by name.
    
            Args:
            ----
                name: Policy name.
    
            """
            for policy in self.policies:
                if policy.name == name:
                    self.policies.remove(policy)
                    return
            raise Exception(f"Policy {name} not found")