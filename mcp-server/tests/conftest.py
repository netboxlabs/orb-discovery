import pytest


@pytest.fixture
def snmpv2c_auth() -> dict:
    return {"protocol_version": "SNMPv2c", "community": "public"}


@pytest.fixture
def snmpv3_auth() -> dict:
    return {
        "protocol_version": "SNMPv3",
        "username": "admin",
        "security_level": "authPriv",
        "auth_protocol": "SHA",
        "auth_passphrase": "authsecret",
        "priv_protocol": "AES",
        "priv_passphrase": "privsecret",
    }


@pytest.fixture
def basic_targets() -> list[dict]:
    return [{"host": "192.168.1.1"}]
