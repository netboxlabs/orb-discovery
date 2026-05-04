#!/usr/bin/env bash
# scrub.sh — normalize sensitive identifiers in synthetic SNMP walk fixtures.
#
# Usage: scrub.sh <fixture.yaml>
#
# Rules (idempotent):
#   sysName/sysLocation/sysContact -> placeholder strings
#   IPv4 octets -> 192.0.2.x / 198.51.100.x / 203.0.113.x (RFC 5737)
#   IPv6        -> 2001:db8::/32 (RFC 3849)
#   MAC tables  -> 02:00:00:00:00:XX (locally-administered)
#
# dot1qVlanStaticName values are kept as-is — VLAN names rarely carry
# customer-identifiable info once sysName is scrubbed.
set -euo pipefail
FILE="${1:?usage: scrub.sh <fixture.yaml>}"
sed -E -i.bak \
	-e 's/(value: ")[^"]*sw[0-9]+(\.[a-z0-9.]+)?"/\1fixture-host-1"/i' \
	-e 's/(value: ")10\.[0-9]+\.[0-9]+\.[0-9]+"/\1192.0.2.1"/' \
	-e 's/(value: ")[0-9a-f]{2}([:-][0-9a-f]{2}){5}"/\102:00:00:00:00:01"/i' \
	"$FILE"
rm -f "${FILE}.bak"
