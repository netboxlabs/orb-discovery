# SLX-OS Custom NAPALM Driver — Design Spec

**Date:** 2026-04-15
**Issue:** OBS-2623
**Branch:** feat/OBS-2623-slxos-driver

---

## Overview

Add a custom NAPALM driver (`slxos`) for Extreme Networks SLX-OS switches. SLX-OS is a Linux-based
NOS originally developed by Brocade and now maintained by Extreme Networks (post-acquisition). It
is found on the SLX 9150, SLX 9250, SLX 9640, and SLX 9740 product families — DC and campus
switching (OBS-2182 priority tier 3).

---

## Approach

**Netmiko + ntc-templates (Approach A)** — SLX-OS has an SSH CLI and two ntc-templates exist for
`extreme_slxos`: `show ip interface brief` and `show clock`. All other commands use regex parsing.

- **Netmiko device type:** `extreme_slx`
- **ntc-templates platform:** `extreme_slxos`

---

## Command Map

| Method | Commands | Parser |
|--------|----------|--------|
| `get_facts` | `show version` | regex |
| `get_facts` (interface_list) | `show ip interface brief` | ntc-template |
| `get_interfaces` | `show interface brief` | regex |
| `get_interfaces_ip` | `show ip interface brief` | ntc-template |
| `get_config` | `show running-config` | raw text |
| `get_vlans` | `show vlan brief` | regex |

---

## Parsing Strategy

### get_facts — `show version`

Typical SLX-OS output:
```
Extreme Networks Routing Operating System Software
  SLX-OS Software Version: SLX-OS 20.2.3
  Copyright (c) 2010-2021 Extreme Networks Inc.

Chassis information for: SLX_9640
  SN:               FTX2244H01B3
  Part Number:      103-000003-07

System uptime: 0 days, 2 hours, 17 minutes, 30 seconds
System Name: slx9640
```

Regex targets:
- Hostname: `System Name:\s+(\S+)`
- Model: `Chassis.*?:\s+(.+)` (first match)
- OS version: `SLX-OS Software Version:\s+\S+\s+(\S+)` or `SLX-OS\s+(\S+)`
- Serial: `SN:\s+(\S+)`
- Uptime: `System uptime:\s+(.+)`

Interface list reuses `show ip interface brief` via ntc-template (same call as `get_interfaces_ip`).

### get_interfaces — `show interface brief`

Typical output:
```
Interface              State    Speed
Management 1           up       1G
Ethernet 0/1           up       10G
Ethernet 0/2           down     1G
Port-channel 1         up       -
Loopback 1             up       -
Ve 10                  up       -
```

Regex: `^((?:Ethernet|Management|Port-channel|Loopback|Ve)\s+\S+)\s+(up|down)\s+(\S+)`

### get_interfaces_ip — `show ip interface brief`

ntc-template (`extreme_slxos`) captures: INTERFACE, IP_ADDRESS, VRF, STATUS, PROTOCOL.

Template expects `(Port-channel|Loopback|Ethernet|Ve)` prefixes. `Management` interfaces are
excluded from the template regex — parse them via fallback regex if needed.

### get_config — `show running-config`

Raw text, no parsing. Honours `sanitized` flag.

### get_vlans — `show vlan brief`

Typical output:
```
VLAN  Name            State    Ports
1     Default         active   Eth 0/1 Eth 0/2
10    Management      active   Eth 0/3
20    Servers         active   Eth 0/4
```

Regex: capture VLAN ID and name from each row; ports from the trailing token list.

---

## Config Sanitization

SLX-OS sensitive patterns:
- `password encrypted <hash>` — in `username` lines
- `enable secret sha256 <hash>`
- `snmp-server community <string> ro/rw`
- `radius-server host ... key <key>`
- `tacacs-server host ... key <key>`

---

## File Structure

```
custom_napalm/slxos.py
tests/custom_drivers/slxos/
  __init__.py
  conftest.py
  test_driver.py
  mock_data/
    test_get_facts/normal/
      show_version.txt
      show_ip_interface_brief.txt
      expected_result.json
    test_get_interfaces/normal/
      show_interface_brief.txt
      expected_result.json
    test_get_interfaces_ip/normal/
      show_ip_interface_brief.txt
      expected_result.json
    test_get_config/normal/
      show_running-config.txt
      expected_result.json
    test_get_config_sanitized/normal/
      show_running-config.txt
      expected_result.json
    test_get_vlans/normal/
      show_vlan_brief.txt
      expected_result.json
```

---

## Validation

End-to-end with mockit (`extreme_slx` DEVICE_TYPE). Note: the mockit container has empty command
templates for `extreme_slx`, so responses will be empty — the driver gracefully returns empty dicts
when commands return no data.
