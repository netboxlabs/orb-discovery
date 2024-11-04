#!/usr/bin/env python
# Copyright 2024 NetBox Labs Inc
"""NetBox Labs - Server Unit Tests."""

from unittest.mock import patch

import pytest
import yaml
from fastapi.testclient import TestClient
from pydantic import ValidationError

from orb_discovery.policy.models import PolicyRequest
from orb_discovery.server import app

client = TestClient(app)


@pytest.fixture
def valid_policy_yaml():
    """
    Valid PolicyRequest YAML string.

    Returns a YAML string that represents a valid PolicyRequest object.
    """
    return """
    discovery:
      policies:
        policy1:
          config:
            schedule: "0 * * * *"
            netbox:
              site: "New York"
          data:
            - driver: "ios"
              hostname: "router1"
              username: "admin"
              password: "password"
    """


@pytest.fixture
def multiple_policies_yaml():
    """
    Multiple PolicyRequest YAML string.

    Returns a YAML string that represents a valid PolicyRequest object.
    """
    return """
    discovery:
      policies:
        policy1:
          config:
            schedule: "0 * * * *"
            netbox:
              site: "New York"
          data:
            - driver: "ios"
              hostname: "router1"
              username: "admin"
              password: "password"
        policy2:
          config:
            schedule: "0 * * * *"
            netbox:
              site: "New York"
          data:
            - driver: "ios"
              hostname: "router1"
              username: "admin"
              password: "password"
    """


@pytest.fixture
def mock_valid_policy_request(valid_policy_yaml) -> PolicyRequest:
    """
    Fixture to mock a PolicyRequest object from YAML.

    Parses the valid YAML content and converts it to a PolicyRequest.
    """
    yaml_dict = yaml.safe_load(valid_policy_yaml)
    policy_request = PolicyRequest.model_validate(yaml_dict)
    print(f"Created PolicyRequest: {policy_request}")  # Debug: check contents
    return policy_request


@pytest.fixture
def mock_multiple_policies_request(multiple_policies_yaml):
    """
    Fixture to mock a PolicyRequest object from YAML.

    Parses the valid YAML content and converts it to a PolicyRequest.
    """
    yaml_dict = yaml.safe_load(multiple_policies_yaml)
    return PolicyRequest.model_validate(yaml_dict)


@pytest.fixture
def mock_supported_drivers():
    """
    Fixture to mock supported drivers.

    Mocks the supported drivers to control response in capabilities endpoint.
    """
    with patch(
        "orb_discovery.server.supported_drivers", ["driver1", "driver2"]
    ) as mock:
        yield mock


@pytest.fixture
def mock_version_semver():
    """
    Fixture to mock version_semver.

    Mocks the version_semver function to control version response in status endpoint.
    """
    with patch("orb_discovery.server.version_semver", return_value="1.0.0") as mock:
        yield mock


@pytest.fixture
def mock_manager():
    """
    Fixture to mock the PolicyManager.

    Mocks the PolicyManager to control policy behavior in tests.
    """
    with patch("orb_discovery.server.manager") as mock:
        yield mock


def test_read_status(mock_version_semver):
    """
    Test the /api/v1/status endpoint.

    Ensures that the version and uptime are correctly returned.

    Args:
    ----
        mock_version_semver: Mocked version_semver function.

    """
    response = client.get("/api/v1/status")
    mock_version_semver.assert_called_once()
    assert response.status_code == 200
    assert response.json()["version"] == "1.0.0"
    assert "up_time_seconds" in response.json()


def test_read_capabilities(mock_supported_drivers):
    """
    Test the /api/v1/capabilities endpoint.

    Verifies it returns the mocked supported drivers.

    Args:
    ----
        mock_supported_drivers: Mocked supported drivers list.

    """
    response = client.get("/api/v1/capabilities")
    assert response.status_code == 200
    assert response.json() == {"supported_drivers": mock_supported_drivers}


def test_write_policy_valid_yaml(mock_valid_policy_request, valid_policy_yaml):
    """
    Test posting a valid YAML policy.

    Ensures it is accepted and returns the correct response.

    Args:
    ----
        mock_valid_policy_request: Mocked PolicyRequest object.
        valid_policy_yaml: Valid PolicyRequest YAML string.

    """
    with patch(
        "orb_discovery.server.manager.parse_policy",
        return_value=mock_valid_policy_request,
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            data=valid_policy_yaml,
        )
        assert response.status_code == 201
        assert response.json() == {"detail": "policy 'policy1' is running"}


def test_write_policy_invalid_yaml():
    """Test posting a invalid YAML policy."""
    with patch(
        "orb_discovery.server.manager.parse_policy",
        side_effect=yaml.YAMLError("invalid"),
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            json={"discovery": {"policies": {"policy1": {}}}},
        )
        assert response.status_code == 400
        assert response.json() == {"detail": "Invalid YAML format"}


def test_write_policy_validation_error():
    """Test posting a valid YAML policy but with invalid field."""
    with patch(
        "orb_discovery.server.manager.parse_policy",
        side_effect=ValidationError.from_exception_data(
            "Invalid data",
            line_errors=[
                {
                    "loc": ("discovery", "policies", "policy1", "config", "schedule"),
                    "msg": "field required",
                    "type": "missing",
                },
            ],
        ),
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            json={"discovery": {"policies": {"policy1": {}}}},
        )
        assert response.status_code == 400
        assert response.json() == {
            "detail": [
                {
                    "field": "discovery.policies.policy1.config.schedule",
                    "type": "missing",
                    "error": "Field required",
                }
            ]
        }


def test_write_policy_unexpected_parser_error():
    """Test posting a invalid YAML policy."""
    with patch(
        "orb_discovery.server.manager.parse_policy",
        side_effect=Exception("unexpected error"),
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            json={"discovery": {"policies": {"policy1": {}}}},
        )
        assert response.status_code == 400
        assert response.json() == {"detail": "unexpected error"}


def test_write_policy_invalid_content_type():
    """
    Test the /api/v1/policies endpoint with invalid content type.

    Ensures a 400 error is returned.

    """
    response = client.post(
        "/api/v1/policies",
        headers={"content-type": "application/json"},
        json={"discovery": {"policies": {"policy1": {}}}},
    )
    assert response.status_code == 400
    assert (
        response.json()["detail"]
        == "invalid Content-Type. Only 'application/x-yaml' is supported"
    )


def test_write_policy_multiple_policies_error(
    mock_multiple_policies_request, multiple_policies_yaml
):
    """
    Test posting multiple policies.

    Ensures a 400 error is returned, as only one policy is allowed.

    Args:
    ----
        mock_multiple_policies_request: Mocked PolicyRequest object.
        multiple_policies_yaml: Multiple policies YAML string.

    """
    with patch(
        "orb_discovery.server.manager.parse_policy",
        return_value=mock_multiple_policies_request,
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            data=multiple_policies_yaml,
        )
        assert response.status_code == 400
        assert response.json()["detail"] == "only one policy allowed per request"


def test_write_policy_no_policy_error():
    """
    Test posting a request with no policies.

    Ensures a 400 error is returned, indicating no policy was found.

    """
    with patch(
        "orb_discovery.server.parse_yaml_body",
        return_value=PolicyRequest(discovery={"policies": {}}),
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            json={"discovery": {"policies": {}}},
        )
        assert response.status_code == 400
        assert response.json()["detail"] == "no policy found in request"


def test_policy_start_error(mock_valid_policy_request, valid_policy_yaml):
    """
    Test starting a policy that already exists.

    Args:
    ----
        mock_valid_policy_request: Mocked PolicyRequest object.
        valid_policy_yaml: Valid PolicyRequest YAML string.

    """
    with patch(
        "orb_discovery.server.manager.parse_policy",
        return_value=mock_valid_policy_request,
    ), patch(
        "orb_discovery.server.manager.start_policy",
        side_effect=Exception("Policy exists"),
    ):
        response = client.post(
            "/api/v1/policies",
            headers={"Content-Type": "application/x-yaml"},
            data=valid_policy_yaml,
        )
        assert response.status_code == 400
        assert response.json() == {"detail": "Policy exists"}


def test_delete_policy(mock_manager):
    """
    Test deleting a valid policy.

    Ensures the policy is successfully deleted and correct response is returned.

    Args:
    ----
        mock_manager: Mocked PolicyManager instance.

    """
    mock_manager.policy_exists.return_value = True
    response = client.delete("/api/v1/policies/policy1")
    assert response.status_code == 200
    assert response.json() == {"detail": "policy 'policy1' was deleted"}


def test_delete_policy_not_found(mock_manager):
    """
    Test deleting a non-existent policy.

    Ensures a 404 error is returned if the policy does not exist.

    Args:
    ----
        mock_manager: Mocked PolicyManager instance.

    """
    mock_manager.policy_exists.return_value = False
    response = client.delete("/api/v1/policies/policy1")
    assert response.status_code == 404
    assert response.json()["detail"] == "policy 'policy1' not found"


def test_delete_policy_error(mock_manager):
    """
    Test deleting a policy that raises an exception.

    Ensures a 400 error is returned if an exception is raised.

    Args:
    ----
        mock_manager: Mocked PolicyManager instance.

    """
    mock_manager.policy_exists.return_value = True
    mock_manager.delete_policy.side_effect = Exception("unexpected error")
    response = client.delete("/api/v1/policies/policy1")
    assert response.status_code == 400
    assert response.json()["detail"] == "unexpected error"
