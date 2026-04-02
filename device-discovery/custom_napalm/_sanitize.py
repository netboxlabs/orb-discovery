# Copyright 2026 NetBox Labs Inc
"""
Per-vendor config sanitization for custom NAPALM drivers.

Each function takes a raw config string and returns it with sensitive values
replaced by the literal string ``<redacted>``.  Functions are pure — no I/O.
"""
import re

# ── FortiOS ───────────────────────────────────────────────────────────────────

_FORTIOS_SET_FIELDS = re.compile(
    r"(set\s+(?:password|passwd|psk|psksecret|secret|auth-password))\s+.*",
    re.IGNORECASE,
)
_FORTIOS_ENC = re.compile(r"(\bENC\b)\s+\S+")


def sanitize_fortios(text: str) -> str:
    """Redact sensitive values from a FortiOS configuration string."""
    text = _FORTIOS_SET_FIELDS.sub(r"\1 <redacted>", text)
    text = _FORTIOS_ENC.sub(r"\1 <redacted>", text)
    return text


# ── Huawei VRP ────────────────────────────────────────────────────────────────

_VRP_CIPHER = re.compile(r"(\bcipher\b)\s+\S+", re.IGNORECASE)
_VRP_SECRET = re.compile(r"(\bsecret\b(?:\s+\d+)?)\s+\S+", re.IGNORECASE)
_VRP_COMMUNITY = re.compile(r"(\bcommunity\b(?:\s+(?:read|write))?)\s+\S+", re.IGNORECASE)


def sanitize_huawei_vrp(text: str) -> str:
    """Redact sensitive values from a Huawei VRP configuration string."""
    text = _VRP_CIPHER.sub(r"\1 <redacted>", text)
    text = _VRP_SECRET.sub(r"\1 <redacted>", text)
    text = _VRP_COMMUNITY.sub(r"\1 <redacted>", text)
    return text


# ── PAN-OS ────────────────────────────────────────────────────────────────────

_PANOS_TAG_RE = re.compile(
    r"(<(phash|password|psk|secret|hash|bind-password|api-key)>)"
    r"[^<]*"
    r"(</\2>)"
)


def sanitize_panos(text: str) -> str:
    """Redact sensitive XML tag content from a PAN-OS configuration string."""
    return _PANOS_TAG_RE.sub(r"\1<redacted>\3", text)
