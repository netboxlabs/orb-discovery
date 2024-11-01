#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Policy Runner Unit Tests."""

from unittest.mock import MagicMock, patch

import pytest

from orb_discovery.main import main
from orb_discovery.parser import DiscoveryConfig


@pytest.fixture
def mock_get_network_driver():
    """
    Fixture to mock the get_network_driver function.

    Mocks the get_network_driver function to control its behavior during tests.
    """
    with patch("orb_discovery.cli.cli.get_network_driver") as mock:
        yield mock


@pytest.fixture
def mock_discover_device_driver():
    """
    Fixture to mock the discover_device_driver function.

    Mocks the discover_device_driver function to control its behavior during tests.
    """
    with patch("orb_discovery.cli.cli.discover_device_driver") as mock:
        yield mock


def test_run_driver_no_driver(
    mock_client, mock_get_network_driver, mock_discover_device_driver
):
    """
    Test run_driver function when driver is not provided.

    Args:
    ----
        mock_client: Mocked Client class.
        mock_get_network_driver: Mocked get_network_driver function.
        mock_discover_device_driver: Mocked discover_device_driver function.

    """
    info = Napalm(
        driver=None,
        hostname="test_host",
        username="user",
        password="pass",
        timeout=10,
        optional_args={},
    )
    config = DiscoveryConfig(netbox={"site": "test_site"})

    mock_discover_device_driver.return_value = "test_driver"
    mock_np_driver = MagicMock()
    mock_get_network_driver.return_value = mock_np_driver

    run_driver(info, config)

    mock_discover_device_driver.assert_called_once_with(info)
    mock_get_network_driver.assert_called_once_with("test_driver")
    mock_np_driver.assert_called_once_with("test_host", "user", "pass", 10, {})
    mock_client().ingest.assert_called_once()


def test_run_driver_with_driver(
    mock_client, mock_get_network_driver, mock_discover_device_driver
):
    """
    Test run_driver function when driver is already provided.

    Args:
    ----
        mock_client: Mocked Client class.
        mock_get_network_driver: Mocked get_network_driver function.
        mock_discover_device_driver: Mocked discover_device_driver function.

    """
    info = Napalm(
        driver="ios",
        hostname="test_host",
        username="user",
        password="pass",
        timeout=10,
        optional_args={},
    )
    config = DiscoveryConfig(netbox={"site": "test_site"})

    mock_np_driver = MagicMock()
    mock_get_network_driver.return_value = mock_np_driver

    run_driver(info, config)

    mock_discover_device_driver.assert_not_called()
    mock_get_network_driver.assert_called_once_with("ios")
    mock_np_driver.assert_called_once_with("test_host", "user", "pass", 10, {})
    mock_client().ingest.assert_called_once()



def test_run_driver_with_not_intalled_driver(
    mock_get_network_driver, mock_discover_device_driver
):
    """
    Test run_driver function when driver is provided but not installed.

    Args:
    ----
        mock_get_network_driver: Mocked get_network_driver function.
        mock_discover_device_driver: Mocked discover_device_driver function.

    """
    info = Napalm(
        driver="not_installed",
        hostname="test_host",
        username="user",
        password="pass",
        timeout=10,
        optional_args={},
    )
    config = DiscoveryConfig(netbox={"site": "test_site"})

    mock_np_driver = MagicMock()
    mock_get_network_driver.return_value = mock_np_driver

    with pytest.raises(Exception) as excinfo:
        run_driver(info, config)

    mock_discover_device_driver.assert_not_called()
    mock_get_network_driver.assert_not_called()

    assert str(excinfo.value).startswith(
        f"Hostname {info.hostname}: specified driver '{info.driver}' was not found in the current installed drivers list:"
    )

def test_run_driver_exception(mock_discover_device_driver):
    """
    Test run_driver function when the device driver is not discovered.

    Args:
    ----
        mock_discover_device_driver: Mocked discover_device_driver function.

    """
    info = Napalm(
        driver=None,
        hostname="test_host",
        username="user",
        password="pass",
        timeout=10,
        optional_args={},
    )
    config = DiscoveryConfig(netbox={"site": "test_site"})

    mock_discover_device_driver.return_value = None

    with pytest.raises(Exception) as excinfo:
        run_driver(info, config)

    assert (
        str(excinfo.value)
        == f"Hostname {info.hostname}: Not able to discover device driver"
    )
