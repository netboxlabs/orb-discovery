"""Unit tests for batch-5 field mappers (vendor row → SwitchportInfo)."""

from custom_napalm.avaya_ers import _ers_aggregate_to_switchport
from custom_napalm.brocade_netiron import (
    _invert_netiron_vlan_config,
    _netiron_aggregate_to_switchport,
    _netiron_split_port_list,
)
from custom_napalm.extreme_slx import (
    _parse_slx_vlan_brief,
    _slx_aggregate_to_switchport,
    _slx_invert_vlan_brief,
)
from custom_napalm.ubiquiti_edgeswitch import (
    _edgesw_row_to_switchport_info,
    _parse_edgesw_port_membership,
    _parse_edgesw_switchport_summary,
)
from custom_napalm.ubiquiti_unifiswitch import (
    _parse_unifi_vlan_detail,
    _parse_unifi_vlan_list,
    _unifi_aggregate_to_switchport,
)

# ----- Brocade/Extreme NetIron ----------------------------------------------


def test_netiron_split_port_list_short_e_token():
    """NetIron short ``e`` token is recognised alongside ``ethe``."""
    assert _netiron_split_port_list("e 1/1 e 1/3") == ["1/1", "1/3"]
    assert _netiron_split_port_list("ethe 1/1 to 1/4") == ["1/1", "1/2", "1/3", "1/4"]


def test_netiron_split_port_list_bare_digit_range_no_leading_slash():
    """Bare-digit ranges (CES form) expand without a leading slash."""
    assert _netiron_split_port_list("ethe 1 to 4") == ["1", "2", "3", "4"]
    assert _netiron_split_port_list("e 2 e 11") == ["2", "11"]


def test_netiron_aggregate_dual_mode_is_trunk_with_native():
    """Untagged in one VLAN + tagged in others → trunk with the untagged as native."""
    info = _netiron_aggregate_to_switchport({"untagged": 10, "tagged": [20, 30]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 10
    assert info.allowed_vlans == [20, 30]


def test_netiron_aggregate_tagged_only_no_native():
    """Tagged-only port maps to trunk with native=None."""
    info = _netiron_aggregate_to_switchport({"untagged": None, "tagged": [100, 200]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [100, 200]


def test_netiron_invert_lag_membership():
    """``tagged lag <N>`` and ``untagged lag <N>`` lines are captured as ``lagN``."""
    raw = (
        "vlan 100 name DATA\n"
        " tagged lag 1 ethe 1/2\n"
        " untagged ethe 1/3\n"
        "!\n"
        "vlan 300 name NATIVE-LAG\n"
        " untagged lag 5\n"
        "!\n"
    )
    per_port = _invert_netiron_vlan_config(raw)
    assert per_port["lag1"] == {"untagged": [], "tagged": [100]}
    assert per_port["lag5"] == {"untagged": [300], "tagged": []}
    assert per_port["1/2"] == {"untagged": [], "tagged": [100]}
    assert per_port["1/3"] == {"untagged": [100], "tagged": []}


def test_netiron_aggregate_multiple_untagged_yields_routed():
    """Multi-untagged (anomalous; 802.1Q forbids) → routed, not access-on-last."""
    info = _netiron_aggregate_to_switchport({"untagged": [10, 20], "tagged": []})
    assert info.enabled is False
    assert info.admin_mode is None


# ----- Avaya/Extreme ERS ----------------------------------------------------


def test_ers_untag_all_yields_access_on_pvid():
    """``UntagAll`` → access mode on the PVID; membership list is ignored."""
    info = _ers_aggregate_to_switchport(
        {"pvid": 10, "tagging": "UntagAll"}, [10, 99]
    )
    assert info.admin_mode == "access"
    assert info.access_vlan == 10
    assert info.native_vlan is None
    assert info.allowed_vlans is None


def test_ers_untag_pvid_only_yields_trunk_with_native():
    """``UntagPvidOnly`` → trunk with native=PVID; tagged=members minus PVID."""
    info = _ers_aggregate_to_switchport(
        {"pvid": 10, "tagging": "UntagPvidOnly"}, [10, 20, 30]
    )
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 10
    assert info.allowed_vlans == [20, 30]


def test_ers_tag_all_yields_trunk_no_native_with_pvid_tagged():
    """``TagAll`` → trunk, no native; PVID stays in tagged list when a member."""
    info = _ers_aggregate_to_switchport(
        {"pvid": 1, "tagging": "TagAll"}, [1, 100, 200]
    )
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [1, 100, 200]


def test_ers_disable_yields_routed():
    """``Disable`` (or any unknown tagging mode) → routed/disabled."""
    info = _ers_aggregate_to_switchport(
        {"pvid": 1, "tagging": "Disable"}, []
    )
    assert info.enabled is False
    assert info.admin_mode is None


def test_ers_trunk_modes_with_no_membership_yield_routed():
    """UntagPvidOnly / TagAll trunks need membership data; empty → routed."""
    for mode in ("UntagPvidOnly", "TagAll"):
        info = _ers_aggregate_to_switchport(
            {"pvid": 10, "tagging": mode}, []
        )
        assert info.enabled is False, f"{mode} with empty members should be routed"
        assert info.admin_mode is None


# ----- Extreme SLX-OS -------------------------------------------------------


def test_slx_aggregate_access_one_untagged():
    """One untagged VID + no tagged → access mode on that VID."""
    info = _slx_aggregate_to_switchport({"untagged": 100, "tagged": []})
    assert info.enabled is True
    assert info.admin_mode == "access"
    assert info.access_vlan == 100
    assert info.allowed_vlans is None


def test_slx_aggregate_trunk_with_native_and_tagged_only():
    """Untagged + tagged → trunk with native; tagged-only → trunk no native."""
    info = _slx_aggregate_to_switchport({"untagged": 10, "tagged": [20, 30]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 10
    assert info.allowed_vlans == [20, 30]

    info = _slx_aggregate_to_switchport({"untagged": None, "tagged": [100, 200]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [100, 200]


def test_slx_invert_canonicalises_eth_and_po_tokens():
    """Inverter expands ``Eth 0/1`` → ``Ethernet 0/1`` and ``Po 1`` → ``Port-channel 1``."""
    text = (
        "VLAN  Name   Type    State    Ports\n"
        "====  ====   ====    ====     ====\n"
        "10    USERS  STATIC  ACTIVE   Eth 0/1(u) Po 1(t)\n"
        "20    DATA   STATIC  ACTIVE   Eth 0/1(t) Po 1(u)\n"
    )
    rows = _parse_slx_vlan_brief(text)
    per_port = _slx_invert_vlan_brief(rows)
    assert per_port["Ethernet 0/1"] == {"untagged": [10], "tagged": [20]}
    assert per_port["Port-channel 1"] == {"untagged": [20], "tagged": [10]}


def test_slx_aggregate_multiple_untagged_yields_routed():
    """SLX: multi-untagged → routed (anomalous; 802.1Q forbids)."""
    info = _slx_aggregate_to_switchport({"untagged": [10, 20], "tagged": []})
    assert info.enabled is False
    assert info.admin_mode is None


def test_slx_invert_drops_out_of_range_vid():
    """VIDs outside 1..4094 are silently dropped during inversion."""
    rows = [
        {"vlan_id": 99999, "ports": [("Ethernet 0/1", "u")]},
        {"vlan_id": 0, "ports": [("Ethernet 0/2", "t")]},
        {"vlan_id": 100, "ports": [("Ethernet 0/3", "u")]},
    ]
    per_port = _slx_invert_vlan_brief(rows)
    assert "Ethernet 0/1" not in per_port
    assert "Ethernet 0/2" not in per_port
    assert per_port["Ethernet 0/3"] == {"untagged": [100], "tagged": []}


# ----- Ubiquiti EdgeSwitch --------------------------------------------------


def test_edgesw_access_single_untagged_yields_access():
    """Access mode with one untagged participation → access on that VID."""
    summary = {"mode": "access", "pvid": 100}
    membership = {"participation": [100], "tagging": [], "pvid": 100}
    info = _edgesw_row_to_switchport_info("0/1", summary, membership)
    assert info.admin_mode == "access"
    assert info.access_vlan == 100
    assert info.allowed_vlans is None


def test_edgesw_trunk_with_native_collects_tagging_list():
    """Trunk with VLAN 1 untagged + tagging list → trunk + native + tagged."""
    summary = {"mode": "trunk", "pvid": 1}
    membership = {"participation": [1, 10, 20], "tagging": [10, 20], "pvid": None}
    info = _edgesw_row_to_switchport_info("0/2", summary, membership)
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 1
    assert info.allowed_vlans == [10, 20]


def test_edgesw_trunk_no_untagged_member_drops_native():
    """Trunk where every participation VID is tagged → trunk no native."""
    summary = {"mode": "trunk", "pvid": 1}
    membership = {"participation": [10, 20], "tagging": [10, 20], "pvid": None}
    info = _edgesw_row_to_switchport_info("0/4", summary, membership)
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [10, 20]


def test_edgesw_general_mode_collapses_to_trunk():
    """General mode collapses to trunk; native = sole untagged member."""
    summary = {"mode": "general", "pvid": 100}
    membership = {"participation": [100, 200], "tagging": [200], "pvid": 100}
    info = _edgesw_row_to_switchport_info("0/3", summary, membership)
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 100
    assert info.allowed_vlans == [200]


def test_edgesw_access_no_membership_yields_routed():
    """
    Access mode with no participation/tagging data → routed (no PVID fallback).

    This pins the post-codex-review behaviour: the previous PVID-only fallback
    was removed because it could clobber NetBox's existing untagged_vlan when
    the running-config wasn't fetched.
    """
    summary = {"mode": "access", "pvid": 100}
    info = _edgesw_row_to_switchport_info("0/9", summary, None)
    assert info.enabled is False
    assert info.admin_mode is None


def test_edgesw_trunk_no_membership_yields_routed():
    """Trunk mode with no participation/tagging → routed (no PVID-only trunk)."""
    summary = {"mode": "trunk", "pvid": 1}
    info = _edgesw_row_to_switchport_info("0/8", summary, None)
    assert info.enabled is False
    assert info.admin_mode is None


def test_edgesw_routed_mode_yields_routed():
    """``Routed`` Mode column → routed regardless of membership."""
    summary = {"mode": "routed", "pvid": 1}
    info = _edgesw_row_to_switchport_info("0/5", summary, None)
    assert info.enabled is False
    assert info.admin_mode is None


def test_edgesw_access_mismatched_membership_falls_back_to_routed():
    """Access mode + tagged rows → routed (don't clobber NetBox via PATCH)."""
    summary = {"mode": "access", "pvid": 100}
    membership = {"participation": [100, 200], "tagging": [200], "pvid": 100}
    info = _edgesw_row_to_switchport_info("0/1", summary, membership)
    assert info.enabled is False
    assert info.admin_mode is None


def test_edgesw_summary_parser_skips_header_and_dashes():
    """Header row, dashes separator, and blank lines must not produce ports."""
    text = (
        "                                  Acceptable Ingress     Default\n"
        "Interface     Mode         PVID     Frame Types Filtering  Priority\n"
        "------------- ------------ -------- ----------- ---------- ----------\n"
        "0/1           Access       100      Admit All   Disabled   0\n"
        "0/2           Trunk        1        VLAN Only   Enabled    0\n"
    )
    out = _parse_edgesw_switchport_summary(text)
    assert out == {
        "0/1": {"mode": "access", "pvid": 100},
        "0/2": {"mode": "trunk", "pvid": 1},
    }


def test_edgesw_membership_parser_ignores_svi_blocks():
    """``interface vlan N`` SVI blocks must not appear as switchports."""
    cfg = (
        "hostname x\n"
        "!\n"
        "interface vlan 1\n"
        " ip address 192.168.1.1 255.255.255.0\n"
        "!\n"
        "interface 0/1\n"
        " vlan pvid 100\n"
        " vlan participation include 100\n"
        "!\n"
    )
    out = _parse_edgesw_port_membership(cfg)
    assert "vlan" not in out
    assert "0/1" in out
    assert out["0/1"]["participation"] == [100]
    assert out["0/1"]["pvid"] == 100


# ----- Ubiquiti UniFiSwitch -------------------------------------------------


def test_unifi_aggregate_access_one_untagged():
    """One Untagged VID + no Tagged → access mode on that VID."""
    info = _unifi_aggregate_to_switchport({"untagged": 100, "tagged": []})
    assert info.enabled is True
    assert info.admin_mode == "access"
    assert info.access_vlan == 100
    assert info.allowed_vlans is None


def test_unifi_aggregate_trunk_with_native_and_tagged_only():
    """Untagged + Tagged → trunk + native; Tagged-only → trunk no native."""
    info = _unifi_aggregate_to_switchport({"untagged": 1, "tagged": [10, 20]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan == 1
    assert info.allowed_vlans == [10, 20]

    info = _unifi_aggregate_to_switchport({"untagged": None, "tagged": [100, 200]})
    assert info.admin_mode == "trunk"
    assert info.native_vlan is None
    assert info.allowed_vlans == [100, 200]


def test_unifi_aggregate_multiple_untagged_yields_routed():
    """UnifiSwitch: multi-untagged → routed (anomalous; 802.1Q forbids)."""
    info = _unifi_aggregate_to_switchport({"untagged": [10, 20], "tagged": []})
    assert info.enabled is False
    assert info.admin_mode is None


def test_unifi_aggregate_empty_membership_yields_routed():
    """Port with no membership at all classifies as routed/disabled."""
    info = _unifi_aggregate_to_switchport({"untagged": None, "tagged": []})
    assert info.enabled is False
    assert info.admin_mode is None


def test_unifi_parse_vlan_detail_skips_exclude_and_header():
    """Only `Include` rows survive; Exclude/headers/dashes drop out."""
    text = (
        "VLAN ID........................... 100\n"
        "VLAN Name......................... USERS\n"
        "VLAN Type......................... Static\n"
        "\n"
        "   Interface  Current   Configured  Tagging\n"
        "   ---------  --------  ----------  --------\n"
        "   0/1        Include   Autodetect  Untagged\n"
        "   0/2        Include   Autodetect  Tagged\n"
        "   0/3        Exclude   Autodetect  Tagged\n"
    )
    assert _parse_unifi_vlan_detail(text) == [("0/1", "Untagged"), ("0/2", "Tagged")]


def test_unifi_parse_vlan_list_extracts_vids():
    """`show vlan` table yields a deduplicated, ordered VID list."""
    text = (
        "VLAN ID  VLAN Name                    VLAN Type\n"
        "-------  ---------------------------  --------\n"
        "1        Default                      Default\n"
        "10       USERS                        Static\n"
        "100      DATA                         Static\n"
    )
    assert _parse_unifi_vlan_list(text) == [1, 10, 100]
