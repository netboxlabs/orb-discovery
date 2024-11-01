#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Policy Manager Unit Tests."""
import os
from pathlib import Path
from unittest.mock import mock_open, patch

import pytest

from orb_discovery.parser import (
    Config,
    ParseException,
    parse_config,
    parse_config_file,
    resolve_env_vars,
)


@pytest.fixture
def valid_yaml():
    """Valid Yaml Generator."""
    return """
    diode:
      config:
        target: "target_value"
        api_key: "api_key_value"
        tls_verify: true
      policies:
        policy1:
          config:
            netbox:
              site: "New York"
          data:
            - driver: "ios"
              hostname: "router1"
              username: "admin"
              password: "password"
    """


@pytest.fixture
def invalid_yaml():
    """Invalid Yaml Generator."""
    return """
    diode:
      config:
        target: "target_value"
        api_key: "api_key_value"
        tls_verify: true
      policies:
        policy1:
          config:
            netbox:
              site: "New York"
          data:
            - driver: "ios"
              hostname: "router1"
              username: "admin"
              # Missing password field
    """


@pytest.fixture
def mock_start_policy():
    """
    Fixture to mock the start_policy function.

    Mocks the start_policy method to control its behavior during tests.
    """
    with patch("orb_discovery.cli.cli.start_policy") as mock:
        yield mock



def test_start_policy(mock_thread_pool_executor, mock_as_completed):
    """
    Test start_policy function with different configurations.

    Args:
    ----
        mock_thread_pool_executor: Mocked ThreadPoolExecutor class.
        mock_as_completed: Mocked as_completed funtion.

    """
    cfg = Policy(
        config=DiscoveryConfig(netbox={"site": "test_site"}),
        data=[
            Napalm(
                driver="driver",
                hostname="host",
                username="user",
                password="pass",
                timeout=10,
                optional_args={},
            )
        ],
    )
    max_workers = 2

    mock_future = MagicMock()
    mock_future.result.return_value = None
    mock_executor = MagicMock()
    mock_executor.submit.return_value = mock_future
    mock_thread_pool_executor.return_value = mock_executor
    mock_as_completed.return_value = [mock_future]

    start_policy("policy", cfg, max_workers)

    mock_thread_pool_executor.assert_called_once_with(max_workers=2)
    mock_future.result.assert_called_once()


def test_start_policy_exception(mock_thread_pool_executor, mock_as_completed):
    """
    Test start_policy function with different configurations.

    Args:
    ----
        mock_thread_pool_executor: Mocked ThreadPoolExecutor class.
        mock_as_completed: Mocked as_completed funtion.

    """
    cfg = Policy(
        config=DiscoveryConfig(netbox={"site": "test_site"}),
        data=[
            Napalm(
                driver="driver",
                hostname="host",
                username="user",
                password="pass",
                timeout=10,
                optional_args={},
            )
        ],
    )
    max_workers = 2

    mock_future = MagicMock()
    mock_future.result.side_effect = Exception("Test exception")
    mock_executor = MagicMock()
    mock_executor.submit.return_value = mock_future
    mock_thread_pool_executor.return_value = mock_executor
    mock_as_completed.return_value = [mock_future]

    start_policy("policy", cfg, max_workers)

    mock_thread_pool_executor.assert_called_once_with(max_workers=2)
    mock_future.result.assert_called_once()
