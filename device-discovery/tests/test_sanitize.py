"""Unit tests for custom_napalm._sanitize — one test per sensitive pattern per vendor."""
import pytest

from custom_napalm._sanitize import sanitize_fortios, sanitize_huawei_vrp, sanitize_panos

# ── FortiOS ───────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("field", [
    "password", "passwd", "psk", "psksecret", "secret", "auth-password",
])
def test_sanitize_fortios_set_field(field):
    """Verify each sensitive field name is redacted while non-sensitive lines are untouched."""
    raw = f"    set {field} mysecretvalue\n    set ip 1.2.3.4\n"
    result = sanitize_fortios(raw)
    assert "mysecretvalue" not in result
    assert f"set {field} <redacted>" in result
    assert "set ip 1.2.3.4" in result  # non-sensitive line untouched


def test_sanitize_fortios_password_enc():
    """'set password ENC <base64>' — both ENC and the encoded value are redacted."""
    raw = "        set password ENC SH2/kZN9UjH3Kz8abc==\n"
    result = sanitize_fortios(raw)
    assert "SH2/kZN9UjH3Kz8abc==" not in result
    assert "set password <redacted>" in result


def test_sanitize_fortios_standalone_enc():
    """Verify standalone ENC lines have the encoded value redacted."""
    raw = "    ENC SH2/kZN9UjH3Kz8abc==\n"
    result = sanitize_fortios(raw)
    assert "SH2/kZN9UjH3Kz8abc==" not in result
    assert "ENC <redacted>" in result


def test_sanitize_fortios_non_sensitive_untouched():
    """Verify non-sensitive FortiOS lines are returned unchanged."""
    raw = "    set ip 192.168.1.1 255.255.255.0\n    set description \"WAN port\"\n"
    assert sanitize_fortios(raw) == raw


# ── Huawei VRP ────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("line,keyword", [
    ("authentication-mode password cipher %^%#secrethash%^%#", "cipher"),
    ("psk cipher secretpskvalue", "cipher"),
    ("key cipher secretkeyvalue", "cipher"),
    ("local-user admin secret 8 $1$secrethash", "secret"),
    ("snmp-agent community read mycommunity", "community"),
])
def test_sanitize_huawei_vrp_sensitive(line, keyword):
    """Verify each sensitive Huawei VRP keyword has its value redacted."""
    result = sanitize_huawei_vrp(line)
    assert "<redacted>" in result
    assert keyword in result


def test_sanitize_huawei_vrp_non_sensitive_untouched():
    """Verify non-sensitive Huawei VRP lines are returned unchanged."""
    raw = "sysname FGT-TEST\ninterface GigabitEthernet0/0\n ip address 10.0.0.1 255.255.255.0\n"
    assert sanitize_huawei_vrp(raw) == raw


# ── PAN-OS ────────────────────────────────────────────────────────────────────

@pytest.mark.parametrize("tag", [
    "phash", "password", "psk", "secret", "hash", "bind-password", "api-key",
])
def test_sanitize_panos_xml_tag(tag):
    """Verify each sensitive PAN-OS XML tag has its content redacted."""
    raw = f"<{tag}>mysecretvalue</{tag}>"
    result = sanitize_panos(raw)
    assert "mysecretvalue" not in result
    assert f"<{tag}><redacted></{tag}>" == result


def test_sanitize_panos_non_sensitive_untouched():
    """Verify non-sensitive PAN-OS XML tags are returned unchanged."""
    raw = "<hostname>my-device</hostname><model>PA-VM</model>"
    assert sanitize_panos(raw) == raw


def test_sanitize_panos_preserves_surrounding_xml():
    """Verify sensitive tags are redacted while adjacent non-sensitive tags are preserved."""
    raw = "<config><phash>secret</phash><hostname>fw01</hostname></config>"
    result = sanitize_panos(raw)
    assert "<phash><redacted></phash>" in result
    assert "<hostname>fw01</hostname>" in result
