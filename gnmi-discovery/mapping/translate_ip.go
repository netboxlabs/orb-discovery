package mapping

import "strings"

// firstKeyVal expects s to start with "[<key>=<val>]" and returns val plus the
// remainder after the closing "]". Splitting on "]" (not "/") keeps values that
// contain "/" (e.g. Eth1/1) or ":" (IPv6) intact.
func firstKeyVal(s string) (val, rest string, ok bool) {
	if len(s) == 0 || s[0] != '[' {
		return "", "", false
	}
	c := strings.Index(s, "]")
	if c < 0 {
		return "", "", false
	}
	kv := s[1:c] // e.g. name=Ethernet1
	eq := strings.Index(kv, "=")
	if eq < 0 {
		return "", "", false
	}
	return kv[eq+1:], s[c+1:], true
}

// parseIPAddressPath extracts (iface, index, family, ip, leaf) from an
// OpenConfig subinterface IP path under ifaceListPath. family is "ipv4"/"ipv6";
// leaf is the path after the address key (we consume "state/prefix-length").
// Returns ok=false for any path not under the IP subtree.
func parseIPAddressPath(path, ifaceListPath string) (iface, index, family, ip, leaf string, ok bool) {
	if ifaceListPath == "" || !strings.HasPrefix(path, ifaceListPath+"[") {
		return
	}
	rest := path[len(ifaceListPath):] // "[name=Ethernet1]/subinterfaces/..."
	iface, rest, ok = firstKeyVal(rest)
	if !ok {
		return "", "", "", "", "", false
	}
	const sub = "/subinterfaces/subinterface"
	if !strings.HasPrefix(rest, sub) {
		return "", "", "", "", "", false
	}
	rest = rest[len(sub):]
	index, rest, ok = firstKeyVal(rest)
	if !ok {
		return "", "", "", "", "", false
	}
	switch {
	case strings.HasPrefix(rest, "/ipv4/"):
		family, rest = "ipv4", rest[len("/ipv4"):]
	case strings.HasPrefix(rest, "/ipv6/"):
		family, rest = "ipv6", rest[len("/ipv6"):]
	default:
		return "", "", "", "", "", false
	}
	const addr = "/addresses/address"
	if !strings.HasPrefix(rest, addr) {
		return "", "", "", "", "", false
	}
	rest = rest[len(addr):]
	ip, rest, ok = firstKeyVal(rest)
	if !ok {
		return "", "", "", "", "", false
	}
	leaf = strings.TrimPrefix(rest, "/")
	return iface, index, family, ip, leaf, true
}
