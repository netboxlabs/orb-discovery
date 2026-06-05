package mapping

import (
	"sort"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
)

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

// translateIPs emits an IPAddress per (interface, subinterface index, family, ip)
// that reports a prefix-length. index 0 assigns to the parent interface; index>0
// emits a child virtual subinterface "<iface>.<index>" (once) and assigns there.
func translateIPs(profile *Profile, snap map[string]any, dev *diode.Device) []diode.Entity {
	listPath := profile.Interfaces.ListPath
	if listPath == "" {
		return nil
	}
	type addrKey struct{ iface, index, family, ip string }
	prefixLen := map[addrKey]string{}
	var order []addrKey
	for path, val := range snap {
		iface, index, family, ip, leaf, ok := parseIPAddressPath(path, listPath)
		if !ok || leaf != "state/prefix-length" {
			continue
		}
		k := addrKey{iface, index, family, ip}
		if _, seen := prefixLen[k]; !seen {
			order = append(order, k)
		}
		prefixLen[k] = toStr(val)
	}
	sort.Slice(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.iface != b.iface {
			return a.iface < b.iface
		}
		if a.index != b.index {
			return a.index < b.index
		}
		if a.family != b.family {
			return a.family < b.family
		}
		return a.ip < b.ip
	})

	children := map[string]*diode.Interface{} // "<iface>.<index>" -> emitted child
	var out []diode.Entity
	for _, k := range order {
		pl := prefixLen[k]
		if pl == "" {
			continue // skip addresses with no prefix-length (do not guess /32 or /128)
		}
		var assigned *diode.Interface
		if k.index == "0" {
			assigned = &diode.Interface{Device: dev, Name: strptr(k.iface)}
		} else {
			name := k.iface + "." + k.index
			ch, ok := children[name]
			if !ok {
				ch = &diode.Interface{
					Device: dev,
					Name:   strptr(name),
					Type:   strptr("virtual"),
					Parent: &diode.Interface{Device: dev, Name: strptr(k.iface)},
				}
				children[name] = ch
				out = append(out, ch) // emit the child subinterface before its IP
			}
			assigned = ch
		}
		out = append(out, &diode.IPAddress{
			Address:        strptr(k.ip + "/" + pl),
			Status:         strptr("active"),
			AssignedObject: assigned,
		})
	}
	return out
}
