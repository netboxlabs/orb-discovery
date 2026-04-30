"""Unit tests for batch-3 field mappers (vendor row → SwitchportInfo)."""

from custom_napalm.aruba_aoscx_ssh import (
    _aoscx_ssh_row_to_switchport_info,
    _parse_aoscx_show_vlan_port_config,
)
from custom_napalm.dell_ftos import (
    _ftos_row_to_switchport_info,
    _parse_ftos_show_interfaces_switchport,
)
from custom_napalm.extreme_exos import (
    _exos_merge_to_switchport_info,
    _parse_exos_show_vlan,
)
from custom_napalm.hp_comware import (
    _comware_merge_to_switchport_info,
    _expand_comware_iface,
    _parse_comware_display_vlan_all,
    _parse_comware_interface_brief_modes,
)
from custom_napalm.huawei_vrp import _huawei_row_to_switchport_info

# ----- Huawei VRP -----------------------------------------------------------


def test_huawei_unknown_link_type_routed():
    """Desirable / auto / dot1q-tunnel link-types map to routed."""
    info = _huawei_row_to_switchport_info({"link_type": "desirable", "vlan_id": "1"})
    assert info.enabled is False


def test_huawei_hybrid_collapses_to_trunk():
    """Hybrid maps to trunk classification with PVID as native."""
    info = _huawei_row_to_switchport_info({
        "link_type": "hybrid",
        "vlan_id": "50",
        "trunk_vlan_list": ["100", "200"],
    })
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 50
    assert info.allowed_vlans == [100, 200]


def test_huawei_full_range_is_wildcard():
    """The 1-4094 trunk range collapses to the wildcard."""
    info = _huawei_row_to_switchport_info({
        "link_type": "trunk",
        "vlan_id": "1",
        "trunk_vlan_list": ["1-4094"],
    })
    assert info.allowed_vlans == "all"


def test_huawei_bool_pvid_rejected():
    """Bool PVID is rejected before int() coercion (bool is a subclass of int)."""
    info = _huawei_row_to_switchport_info({
        "link_type": "access",
        "vlan_id": True,
        "trunk_vlan_list": [],
    })
    assert info.access_vlan is None


# ----- Dell FTOS ------------------------------------------------------------


def test_ftos_general_mode_trunk():
    """FTOS `general` mode classifies as trunk with native + tagged."""
    info = _ftos_row_to_switchport_info({
        "switchport": "Enabled",
        "administrative_mode": "general",
        "native_vlan": "99",
        "trunking_vlans_enabled": "100,200",
    })
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 99
    assert info.allowed_vlans == [100, 200]


def test_ftos_switchport_disabled_routed():
    """Switchport=Disabled → routed regardless of other fields."""
    info = _ftos_row_to_switchport_info({"switchport": "Disabled"})
    assert info.enabled is False


def test_ftos_bool_vid_rejected():
    """Bool fields in access/native VLAN positions are rejected before int() coercion."""
    info = _ftos_row_to_switchport_info({
        "switchport": "Enabled",
        "administrative_mode": "trunk",
        "trunking_native_mode_vlan": True,
        "trunking_vlans_enabled": "100",
    })
    assert info.native_vlan is None
    assert info.allowed_vlans == [100]


def test_ftos_section_parser_handles_multiple_ports():
    """Section parser separates ports correctly on `Name:` header."""
    text = (
        "\nName: GigabitEthernet 0/1\n"
        "Switchport: Enabled\n"
        "Administrative mode: access\n"
        "Access Mode VLAN: 10\n"
        "\n"
        "Name: GigabitEthernet 0/2\n"
        "Switchport: Disabled\n"
    )
    rows = _parse_ftos_show_interfaces_switchport(text)
    assert [r["interface"] for r in rows] == [
        "GigabitEthernet 0/1",
        "GigabitEthernet 0/2",
    ]


# ----- AOS-CX SSH -----------------------------------------------------------


def test_aoscx_ssh_native_tagged_no_untagged():
    """native-tagged folds the native VID into the tagged list with no untagged."""
    info = _aoscx_ssh_row_to_switchport_info({
        "port": "1/1/1",
        "mode": "native-tagged",
        "native": "99",
        "tagged": "100, 200",
    })
    assert info.native_vlan is None
    assert info.allowed_vlans == [100, 200, 99]


def test_aoscx_ssh_trunk_empty_tagged_is_wildcard():
    """Empty tagged list under trunk mode is the AOS-CX wildcard."""
    info = _aoscx_ssh_row_to_switchport_info({
        "port": "1/1/1",
        "mode": "trunk",
        "native": "--",
        "tagged": "--",
    })
    assert info.allowed_vlans == "all"


def test_aoscx_ssh_routed_yields_routed():
    """Routed mode classifies as routed regardless of other columns."""
    info = _aoscx_ssh_row_to_switchport_info({
        "port": "1/1/1",
        "mode": "routed",
        "native": "--",
        "tagged": "--",
    })
    assert info.enabled is False


def test_aoscx_ssh_bool_native_rejected():
    """Bool native VID is rejected; coerce_vid handles the bool guard."""
    info = _aoscx_ssh_row_to_switchport_info({
        "port": "1/1/1",
        "mode": "access",
        "native": True,
        "tagged": "--",
    })
    assert info.access_vlan is None


def test_aoscx_ssh_parser_skips_separator_rows():
    """Table separator rows must not produce bogus port entries."""
    text = (
        "Port    Mode             Native VLAN   Tagged VLAN(s)\n"
        "-----   --------------   -----------   ------------------\n"
        "1/1/1   access           10            --\n"
    )
    rows = _parse_aoscx_show_vlan_port_config(text)
    assert [r["port"] for r in rows] == ["1/1/1"]


# ----- HP Comware -----------------------------------------------------------


def test_comware_iface_expand_known_prefixes():
    """Abbreviated interface names expand to the full form `display interface` emits."""
    assert _expand_comware_iface("GE1/0/1") == "GigabitEthernet1/0/1"
    assert _expand_comware_iface("XGE1/0/49") == "Ten-GigabitEthernet1/0/49"
    assert _expand_comware_iface("BAGG1") == "Bridge-Aggregation1"


def test_comware_iface_expand_passthrough():
    """Names already in full form (or unknown prefixes) are returned unchanged."""
    assert _expand_comware_iface("GigabitEthernet1/0/1") == "GigabitEthernet1/0/1"
    assert _expand_comware_iface("Loopback0") == "Loopback0"
    assert _expand_comware_iface("notaport") == "notaport"


def test_comware_brief_modes_expands_iface_names():
    """Brief-mode rows get their abbreviated names expanded in the modes dict."""
    rows = [{"interface": "GE1/0/1", "type": "A", "vlan_id": "10"}]
    modes = _parse_comware_interface_brief_modes(rows)
    assert "GigabitEthernet1/0/1" in modes
    assert "GE1/0/1" not in modes


def test_comware_bool_pvid_rejected():
    """Bool PVID is rejected before int() coercion (uses coerce_vid)."""
    rows = [{"interface": "GE1/0/1", "type": "A", "vlan_id": True}]
    modes = _parse_comware_interface_brief_modes(rows)
    assert modes["GigabitEthernet1/0/1"]["pvid"] is None


def test_comware_brief_modes_skip_route_rows():
    """Route-mode rows (no Type letter) are skipped from the modes dict."""
    rows = [
        {"interface": "GE1/0/1", "type": "A", "vlan_id": "10"},
        {"interface": "Vlan-interface1", "type": "", "vlan_id": ""},
    ]
    modes = _parse_comware_interface_brief_modes(rows)
    assert "GigabitEthernet1/0/1" in modes
    assert "Vlan-interface1" not in modes


def test_comware_route_mode_iface_routed():
    """An interface with no Type letter (route mode) classifies as routed."""
    info = _comware_merge_to_switchport_info("GE1/0/1", {}, {})
    assert info.enabled is False


def test_comware_hybrid_collapses_to_trunk():
    """Hybrid mode is treated as trunk with PVID as native."""
    modes = {"GE1/0/1": {"mode": "hybrid", "pvid": 50}}
    membership = {"GE1/0/1": {"tagged": [100, 200], "untagged": [50]}}
    info = _comware_merge_to_switchport_info("GE1/0/1", modes, membership)
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 50
    assert info.allowed_vlans == [100, 200]


def test_comware_invert_vlan_all():
    """Inverter builds per-port membership from per-VLAN sections; expands abbrevs."""
    text = (
        " VLAN ID: 10\n"
        " Tagged Ports:\n"
        "   None\n"
        " Untagged Ports:\n"
        "   GE1/0/1\n"
        "\n"
        " VLAN ID: 100\n"
        " Tagged Ports:\n"
        "   GE1/0/2\n"
        " Untagged Ports:\n"
        "   None\n"
        "\n"
        " VLAN ID: 200\n"
        " Tagged Ports:\n"
        "   GE1/0/2\n"
        " Untagged Ports:\n"
        "   None\n"
    )
    membership = _parse_comware_display_vlan_all(text)
    assert membership == {
        "GigabitEthernet1/0/1": {"tagged": [], "untagged": [10]},
        "GigabitEthernet1/0/2": {"tagged": [100, 200], "untagged": []},
    }


# ----- Extreme EXOS ---------------------------------------------------------


def test_exos_parse_show_vlan_with_lowercase_flags():
    """`(t)` and `(u)` lowercase parse as tagged/untagged."""
    text = "VLAN Tag: 10\n   1 (u)\nVLAN Tag: 100\n   1 (t)\n"
    membership = _parse_exos_show_vlan(text)
    assert membership == {"1": {"tagged": [100], "untagged": [10]}}


def test_exos_merge_no_untagged_yields_trunk_no_native():
    """Tagged-only port maps to trunk with no native."""
    info = _exos_merge_to_switchport_info({"tagged": [100, 200], "untagged": []})
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [100, 200]


def test_exos_merge_empty_membership_yields_routed():
    """A port with no membership classifies as routed."""
    info = _exos_merge_to_switchport_info({"tagged": [], "untagged": []})
    assert info.enabled is False
