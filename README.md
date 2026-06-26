> ## ⚠️ This repository has moved
>
> The discovery backends (`device-discovery`, `network-discovery`, `snmp-discovery`,
> `gnmi-discovery`) and the `worker` now live in
> **[netboxlabs/orb-agent](https://github.com/netboxlabs/orb-agent)** under
> `orb-discovery/`, built into the `netboxlabs/orb-agent` image. This repository is
> archived and read-only; open issues were migrated. New work, releases, and issues
> happen in orb-agent. The PyPI packages `netboxlabs-device-discovery` and
> `netboxlabs-orb-worker` continue to publish (now from orb-agent).

# orb-discovery

Orb discovery backends collection

- [device-discovery](./device-discovery/README.md) - Device Discovery Backend that uses [NAPALM](https://github.com/napalm-automation/napalm) Drivers.
- [network-discovery](./network-discovery/README.md) - Network Discovery Backend which is a wrapper over [NMAP](https://nmap.org/) scanner.
- [worker](./worker/README.md) - A Worker Backend that allows to run custom implementation as part of Orb Agent.
- [snmp-discovery](./snmp-discovery/README.md) - Device discovery that uses SNMP
- [gnmi-discovery](./gnmi-discovery/README.md) - **(experimental)** Event-driven device discovery that uses [gNMI](https://github.com/openconfig/gnmi) subscriptions over [OpenConfig](https://www.openconfig.net/) models.