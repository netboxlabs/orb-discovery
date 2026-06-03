package mapping

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
)

// listKeyAndLeaf splits an absolute path into the list key and the remaining
// leaf path, relative to listPath. Returns ok=false when path is not under
// listPath. e.g. (/interfaces/interface[name=Eth1]/state/mtu, /interfaces/interface)
// -> ("Eth1", "state/mtu", true).
func listKeyAndLeaf(path, listPath string) (key, leaf string, ok bool) {
	prefix := listPath + "["
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := path[len(prefix):]
	closeBracket := strings.Index(rest, "]")
	if closeBracket < 0 {
		return "", "", false
	}
	kv := rest[:closeBracket] // e.g. name=Eth1
	eq := strings.Index(kv, "=")
	if eq < 0 {
		return "", "", false
	}
	key = kv[eq+1:]
	leaf = strings.TrimPrefix(rest[closeBracket+1:], "/")
	return key, leaf, true
}

func strptr(s string) *string { return &s }

func toInt64Ptr(v any) *int64 {
	switch n := v.(type) {
	case int:
		x := int64(n)
		return &x
	case int64:
		return &n
	case uint:
		x := int64(n)
		return &x
	case uint8:
		x := int64(n)
		return &x
	case uint16:
		x := int64(n)
		return &x
	case uint32:
		x := int64(n)
		return &x
	case uint64:
		x := int64(n)
		return &x
	case float64:
		x := int64(n)
		return &x
	case string:
		if x, err := strconv.ParseInt(n, 10, 64); err == nil {
			return &x
		}
	}
	return nil
}

func toStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

func sortStrings(s []string) { sort.Strings(s) }

// filterNonEmpty returns the non-empty arguments in order, used to compose the
// Platform name from the operator's NOS-name prefix and the discovered version.
func filterNonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

// componentsByKey groups the /components/component leaves in snap by their list
// key, returning the per-key leaf maps and the sorted key order. Shared by
// translateDevice (chassis serial) and translateComponents (modules) so the
// grouping logic exists in exactly one place. Returns nil order when the
// profile has no components list_path.
func componentsByKey(profile *Profile, snap map[string]any) (map[string]map[string]any, []string) {
	listPath := profile.Components.ListPath
	if listPath == "" {
		return nil, nil
	}
	byKey := map[string]map[string]any{}
	var order []string
	for path, val := range snap {
		key, leaf, ok := listKeyAndLeaf(path, listPath)
		if !ok {
			continue
		}
		if _, seen := byKey[key]; !seen {
			byKey[key] = map[string]any{}
			order = append(order, key)
		}
		byKey[key][leaf] = val
	}
	sortStrings(order)
	return byKey, order
}

// toTags converts tag names to Diode Tag refs, de-duplicating. It always
// operates on a fresh clone so callers' slices are never mutated.
func toTags(names []string) []*diode.Tag {
	if len(names) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]*diode.Tag, 0, len(names))
	for _, n := range names {
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, &diode.Tag{Name: strptr(n)})
	}
	return out
}

// Translate converts a reconciled snapshot into Diode entities using profile.
// It always emits one *diode.Device, then interfaces, then components
// (ModuleBay before its Module, mirroring snmp-discovery ordering).
func Translate(profile *Profile, snap map[string]any, defaults *config.Defaults) []diode.Entity {
	dev := translateDevice(profile, snap, defaults)
	entities := []diode.Entity{dev}
	entities = append(entities, translateInterfaces(profile, snap, dev, defaults)...)
	entities = append(entities, translateComponents(profile, snap, dev, defaults)...)
	return entities
}

func translateDevice(profile *Profile, snap map[string]any, defaults *config.Defaults) *diode.Device {
	dev := &diode.Device{}
	if profile.Device.Hostname != "" {
		if v, ok := snap[profile.Device.Hostname]; ok {
			dev.Name = strptr(toStr(v))
		}
	}
	if defaults != nil {
		if defaults.Site != "" {
			dev.Site = &diode.Site{Name: strptr(defaults.Site)}
		}
		if defaults.Role != "" {
			dev.Role = &diode.DeviceRole{Name: strptr(defaults.Role)}
		}
		if defaults.Device.Manufacturer != "" || defaults.Device.Model != "" {
			dt := &diode.DeviceType{}
			if defaults.Device.Model != "" {
				dt.Model = strptr(defaults.Device.Model)
			}
			if defaults.Device.Manufacturer != "" {
				dt.Manufacturer = &diode.Manufacturer{Name: strptr(defaults.Device.Manufacturer)}
			}
			dev.DeviceType = dt
		}
		// Platform name folds the discovered software version into the operator's
		// platform default (treated as the NOS-name prefix), mirroring the
		// device-discovery convention (no Diode custom fields). e.g. "Arista EOS"
		// + "4.30.1F" -> "Arista EOS 4.30.1F". Set only when at least one of the
		// default prefix or the discovered version is present, so the prior
		// default-only behavior is preserved.
		var osVersion string
		if profile.Device.OSVersion != "" {
			if v, ok := snap[profile.Device.OSVersion]; ok {
				osVersion = toStr(v)
			}
		}
		platformName := strings.TrimSpace(strings.Join(filterNonEmpty(defaults.Device.Platform, osVersion), " "))
		if platformName != "" {
			plat := &diode.Platform{Name: strptr(platformName)}
			if defaults.Device.Manufacturer != "" {
				plat.Manufacturer = &diode.Manufacturer{Name: strptr(defaults.Device.Manufacturer)}
			}
			dev.Platform = plat
		}
		if defaults.Location != "" {
			// Location is scoped to the device's Site (NetBox requires a site).
			dev.Location = &diode.Location{Name: strptr(defaults.Location), Site: dev.Site}
		}
		if defaults.Device.Comments != "" {
			dev.Comments = strptr(defaults.Device.Comments)
		}
		// Device tags = policy-level tags + device-level tags, de-duped clone.
		if tags := toTags(append(append([]string{}, defaults.Tags...), defaults.Device.Tags...)); len(tags) > 0 {
			dev.Tags = tags
		}
	}
	// Device.Serial is taken from the CHASSIS component (the device's own
	// serial). CHASSIS is not an emittable component type, so translateComponents
	// skips it and there is no Module/Device serial conflict.
	if serial := chassisSerial(profile, snap); serial != "" {
		dev.Serial = strptr(serial)
	}
	return dev
}

// chassisSerial returns the serial-no of the component whose type is CHASSIS, or
// "" when there is no such component (or it carries no serial).
func chassisSerial(profile *Profile, snap map[string]any) string {
	typeLeaf := profile.Components.Keys["type"]
	serialLeaf := profile.Components.Keys["serial"]
	if typeLeaf == "" || serialLeaf == "" {
		return ""
	}
	byKey, order := componentsByKey(profile, snap)
	for _, key := range order {
		leaves := byKey[key]
		if strings.ToUpper(toStr(leaves[typeLeaf])) == "CHASSIS" {
			return toStr(leaves[serialLeaf])
		}
	}
	return ""
}

func translateInterfaces(profile *Profile, snap map[string]any, dev *diode.Device, defaults *config.Defaults) []diode.Entity {
	listPath := profile.Interfaces.ListPath
	if listPath == "" {
		return nil
	}
	// group leaf values by interface key
	byKey := map[string]map[string]any{}
	var order []string
	for path, val := range snap {
		key, leaf, ok := listKeyAndLeaf(path, listPath)
		if !ok {
			continue
		}
		if _, seen := byKey[key]; !seen {
			byKey[key] = map[string]any{}
			order = append(order, key)
		}
		byKey[key][leaf] = val
	}
	sortStrings(order)

	ifType := "other"
	var ifDefaultDesc string
	var ifTags []*diode.Tag
	if defaults != nil {
		if defaults.Interface.Type != "" {
			ifType = defaults.Interface.Type
		}
		ifDefaultDesc = defaults.Interface.Description
		ifTags = toTags(defaults.Interface.Tags)
	}

	var out []diode.Entity
	for _, key := range order {
		leaves := byKey[key]
		iface := &diode.Interface{
			Device: dev,
			Name:   strptr(key),
			Type:   strptr(ifType),
		}
		if len(ifTags) > 0 {
			iface.Tags = ifTags
		}
		if leafPath := profile.Interfaces.Keys["description"]; leafPath != "" {
			if v, ok := leaves[leafPath]; ok {
				iface.Description = strptr(toStr(v))
			}
		}
		// Fall back to the policy's default interface description when the device
		// did not report one.
		if (iface.Description == nil || *iface.Description == "") && ifDefaultDesc != "" {
			iface.Description = strptr(ifDefaultDesc)
		}
		if leafPath := profile.Interfaces.Keys["admin_status"]; leafPath != "" {
			if v, ok := leaves[leafPath]; ok {
				enabled := strings.EqualFold(toStr(v), "UP")
				iface.Enabled = &enabled
			}
		}
		if leafPath := profile.Interfaces.Keys["mtu"]; leafPath != "" {
			if v, ok := leaves[leafPath]; ok {
				iface.Mtu = toInt64Ptr(v)
			}
		}
		out = append(out, iface)
	}
	return out
}

// emittableComponentTypes are OpenConfig component types we surface as Modules
// in the MVP. PSU/FAN/SENSOR are classified but not emitted (spec §2).
var emittableComponentTypes = map[string]bool{
	"LINECARD":    true,
	"MODULE":      true,
	"SUPERVISOR":  true,
	"FABRIC":      true,
	"TRANSCEIVER": true,
}

// translateComponents emits, per inventory-bearing component, a standalone
// ModuleBay entity FOLLOWED BY its Module. This mirrors snmp-discovery's
// ordering (Device -> ModuleBay -> Module) so Diode creates the module bay
// before the module that references it. The ModuleType always carries a
// Manufacturer (policy default, else "Unknown"), never model-only.
func translateComponents(profile *Profile, snap map[string]any, dev *diode.Device, defaults *config.Defaults) []diode.Entity {
	if profile.Components.ListPath == "" {
		return nil
	}
	byKey, order := componentsByKey(profile, snap)

	typeLeaf := profile.Components.Keys["type"]
	serialLeaf := profile.Components.Keys["serial"]
	partLeaf := profile.Components.Keys["part"]

	mfg := "Unknown"
	if defaults != nil && defaults.Device.Manufacturer != "" {
		mfg = defaults.Device.Manufacturer
	}

	var out []diode.Entity
	for _, key := range order {
		leaves := byKey[key]
		ctype := strings.ToUpper(toStr(leaves[typeLeaf]))
		if !emittableComponentTypes[ctype] {
			continue
		}
		// 1) standalone ModuleBay, emitted first
		bay := &diode.ModuleBay{Device: dev, Name: strptr(key)}
		out = append(out, bay)

		// 2) Module referencing the bay, with a manufacturer-bearing ModuleType
		model := "Unknown"
		if partLeaf != "" {
			if v, ok := leaves[partLeaf]; ok && toStr(v) != "" {
				model = toStr(v)
			}
		}
		mod := &diode.Module{
			Device:    dev,
			ModuleBay: bay,
			ModuleType: &diode.ModuleType{
				Model:        strptr(model),
				Manufacturer: &diode.Manufacturer{Name: strptr(mfg)},
			},
		}
		if serialLeaf != "" {
			if v, ok := leaves[serialLeaf]; ok {
				mod.Serial = strptr(toStr(v))
			}
		}
		out = append(out, mod)
	}
	return out
}
