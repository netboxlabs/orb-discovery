#!/usr/bin/env python
# Copyright 2026 NetBox Labs Inc
"""NetBox Labs - Policy Models Unit Tests."""

import pytest
from pydantic import ValidationError

from device_discovery.policy.models import Options


def test_options_create_unknown_vlans_default_true():
    """Options.create_unknown_vlans defaults to True."""
    opts = Options()
    assert opts.create_unknown_vlans is True


def test_options_create_unknown_vlans_overridable():
    """Options.create_unknown_vlans accepts an explicit False override."""
    opts = Options(create_unknown_vlans=False)
    assert opts.create_unknown_vlans is False


def test_options_discover_modules_default_off():
    """Options.discover_modules defaults to 'off' (backwards compatibility)."""
    opts = Options()
    assert opts.discover_modules == "off"


@pytest.mark.parametrize("value", ["off", "linecards", "full"])
def test_options_discover_modules_accepts_enum_values(value):
    """Options.discover_modules accepts each documented enum value."""
    opts = Options(discover_modules=value)
    assert opts.discover_modules == value


def test_options_discover_modules_rejects_unknown_value():
    """Options.discover_modules rejects values outside the enum."""
    with pytest.raises(ValidationError):
        Options(discover_modules="bogus")


def test_prefix_parameters_accepts_scope_fields():
    """PrefixParameters carries the four NetBox Prefix scope fields."""
    from device_discovery.policy.models import PrefixParameters

    p = PrefixParameters(
        scope_site="DC-East",
        scope_location="Floor-3",
        scope_region="EMEA",
        scope_site_group="MainGroup",
    )
    assert p.scope_site == "DC-East"
    assert p.scope_location == "Floor-3"
    assert p.scope_region == "EMEA"
    assert p.scope_site_group == "MainGroup"


def test_prefix_parameters_scope_fields_default_to_none():
    """All four scope fields default to None — back-compat for existing configs."""
    from device_discovery.policy.models import PrefixParameters

    p = PrefixParameters()
    assert p.scope_site is None
    assert p.scope_location is None
    assert p.scope_region is None
    assert p.scope_site_group is None


def test_prefix_parameters_inherits_ipam_fields():
    """PrefixParameters keeps the inherited tenant / role / vrf / comments / tags."""
    from device_discovery.policy.models import PrefixParameters

    p = PrefixParameters(role="customer-edge", tenant="acme")
    assert p.role == "customer-edge"
    assert p.tenant == "acme"
    # Inherited from ObjectParameters via IpamParameters
    assert p.tags is None
    assert p.comments is None


def test_defaults_prefix_field_accepts_prefix_parameters():
    """Defaults.prefix accepts a PrefixParameters payload with scope fields."""
    from device_discovery.policy.models import Defaults

    d = Defaults(prefix={"scope_site": "DC-East", "role": "customer-edge"})
    assert d.prefix is not None
    assert d.prefix.scope_site == "DC-East"
    assert d.prefix.role == "customer-edge"


def test_defaults_prefix_back_compat_no_scope():
    """A Defaults payload without scope_* still validates (back-compat)."""
    from device_discovery.policy.models import Defaults

    d = Defaults(prefix={"role": "customer-edge"})
    assert d.prefix is not None
    assert d.prefix.role == "customer-edge"
    assert d.prefix.scope_site is None


def test_options_propagate_defaults_to_prefix_scope_defaults_false():
    """The new Options flag defaults to False (no cascade)."""
    from device_discovery.policy.models import Options

    o = Options()
    assert o.propagate_defaults_to_prefix_scope is False


def test_options_propagate_defaults_to_prefix_scope_accepts_true():
    """The flag accepts True to opt into the cascade."""
    from device_discovery.policy.models import Options

    o = Options(propagate_defaults_to_prefix_scope=True)
    assert o.propagate_defaults_to_prefix_scope is True
