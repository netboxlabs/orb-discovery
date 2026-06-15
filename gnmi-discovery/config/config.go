package config

import "time"

// Delivery mode constants (spec §5).
const (
	ModeAuto     = "auto"
	ModeOnChange = "on_change"
	ModeSample   = "sample"
	ModeGet      = "get"
)

// Default config values.
const (
	// DefaultGNMIPort is the IANA-registered gNMI port. Vendors differ in
	// practice (Arista 6030, Nokia 57400), so operators should set an explicit
	// host:port; this default only applies when a target omits the port.
	DefaultGNMIPort       = 9339
	DefaultDebounceMs     = 2000
	DefaultSampleInterval = 300000 // 5m
	DefaultGetInterval    = 900000 // 15m
)

// Status represents the runtime status of the gnmi-discovery service.
type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

// TLSConfig holds per-target TLS settings.
type TLSConfig struct {
	// SkipVerify keeps TLS but does not verify the target certificate (e.g. a
	// self-signed device cert). Insecure is an explicit opt-in to PLAINTEXT (no
	// TLS at all). With neither set and no CA/cert/key, the dialer uses TLS with
	// the system root CAs (secure by default).
	SkipVerify bool   `yaml:"skip_verify,omitempty"`
	Insecure   bool   `yaml:"insecure,omitempty"`
	CAFile     string `yaml:"ca,omitempty"`
	CertFile   string `yaml:"cert,omitempty"`
	KeyFile    string `yaml:"key,omitempty"`
}

// Target is one gNMI endpoint.
type Target struct {
	Host             string    `yaml:"host"`
	Username         string    `yaml:"username,omitempty"`
	Password         string    `yaml:"password,omitempty"`
	TLS              TLSConfig `yaml:"tls,omitempty"`
	Mode             string    `yaml:"mode,omitempty"`    // overrides PolicyConfig.Mode
	Profile          string    `yaml:"profile,omitempty"` // pins a profile; else auto-detect
	OverrideDefaults *Defaults `yaml:"override_defaults,omitempty"`
	NetboxID         *int      `yaml:"netbox_id,omitempty"`
	// Origin is the gNMI path origin sent on Subscribe/Get requests. Unset (nil)
	// defaults to "openconfig" — the canonical OpenConfig origin, required by
	// strict targets like Nokia SR Linux (which otherwise resolves an origin-less
	// path against its native schema and rejects it). Set it explicitly to ""
	// for a target that needs origin-less paths, or to a vendor-specific origin.
	Origin *string `yaml:"origin,omitempty"`
}

// ResolvedOrigin returns the gNMI request-path origin for this target: the
// explicit value when set (including ""), otherwise the "openconfig" default.
func (t Target) ResolvedOrigin() string {
	if t.Origin != nil {
		return *t.Origin
	}
	return "openconfig"
}

// Scope holds the targets for a policy.
type Scope struct {
	Targets []Target `yaml:"targets"`
}

// DeviceDefaults mirrors snmp-discovery's device defaults subset.
type DeviceDefaults struct {
	Manufacturer string   `yaml:"manufacturer,omitempty"`
	Model        string   `yaml:"model,omitempty"`
	Platform     string   `yaml:"platform,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
	Comments     string   `yaml:"comments,omitempty"`
}

// InterfaceDefaults mirrors snmp-discovery's interface defaults subset.
type InterfaceDefaults struct {
	Type        string   `yaml:"if_type,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
}

// PrefixDefaults holds NetBox defaults applied to discovered IP prefixes.
type PrefixDefaults struct {
	Role        string   `yaml:"role,omitempty"`
	Tenant      string   `yaml:"tenant,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// VlanDefaults holds NetBox defaults applied to discovered VLANs.
type VlanDefaults struct {
	Group       string   `yaml:"group,omitempty"`
	Tenant      string   `yaml:"tenant,omitempty"`
	Role        string   `yaml:"role,omitempty"`
	Tags        []string `yaml:"tags,omitempty"`
	Description string   `yaml:"description,omitempty"`
}

// InterfacePattern maps an interface-name regex to a NetBox interface type.
// Mirrors snmp-discovery's InterfacePattern so policy YAML is portable between
// the two backends.
type InterfacePattern struct {
	Match string `yaml:"match"` // regex matched against the interface name
	Type  string `yaml:"type"`  // NetBox interface type assigned on match
}

// Defaults holds NetBox defaults applied to discovered entities.
type Defaults struct {
	Site      string            `yaml:"site,omitempty"`
	Location  string            `yaml:"location,omitempty"`
	Role      string            `yaml:"role,omitempty"`
	Tags      []string          `yaml:"tags,omitempty"`
	Device    DeviceDefaults    `yaml:"device,omitempty"`
	Interface InterfaceDefaults `yaml:"interface,omitempty"`
	Vlan      VlanDefaults      `yaml:"vlan,omitempty"`
	Prefix    PrefixDefaults    `yaml:"prefix,omitempty"`
	// InterfacePatterns map interface-name regexes to NetBox types (first match
	// wins) and take precedence over the discovered OpenConfig type and the
	// Interface.Type default. InterfaceExcludePatterns are name regexes that skip
	// an interface entirely. Both mirror snmp-discovery.
	InterfacePatterns        []InterfacePattern `yaml:"interface_patterns,omitempty"`
	InterfaceExcludePatterns []string           `yaml:"interface_exclude_patterns,omitempty"`
}

// PolicyConfig holds policy-wide config (spec §7).
type PolicyConfig struct {
	Mode             string   `yaml:"mode,omitempty"`
	DebounceMs       int      `yaml:"debounce_ms,omitempty"`
	SampleIntervalMs int      `yaml:"sample_interval_ms,omitempty"`
	GetIntervalMs    int      `yaml:"get_interval_ms,omitempty"`
	Defaults         Defaults `yaml:"defaults"`
}

// Policy is a gnmi-discovery policy.
type Policy struct {
	Config PolicyConfig `yaml:"config"`
	Scope  Scope        `yaml:"scope"`
}

// Policies is the request envelope: {policies: {name: Policy}}.
type Policies struct {
	Policies map[string]Policy `yaml:"policies"`
}

// cloneStrings returns a new slice with the same elements as src, sharing no
// backing array with the original. A nil src returns nil.
func cloneStrings(src []string) []string {
	return append([]string(nil), src...)
}

// clonePatterns returns a new slice with the same elements as src, sharing no
// backing array with the original. A nil src returns nil.
func clonePatterns(src []InterfacePattern) []InterfacePattern {
	return append([]InterfacePattern(nil), src...)
}

// MergeDefaults returns policyDefaults overlaid with non-empty overrideDefaults.
// The returned *Defaults owns all of its slice fields — no backing array is
// shared with either input, so callers may freely append without corrupting the
// parsed config.
func MergeDefaults(policyDefaults, overrideDefaults *Defaults) *Defaults {
	if policyDefaults == nil {
		if overrideDefaults == nil {
			return &Defaults{}
		}
		// Return a deep copy of override so the caller still owns all slices.
		cp := *overrideDefaults
		cp.Tags = cloneStrings(overrideDefaults.Tags)
		cp.Device.Tags = cloneStrings(overrideDefaults.Device.Tags)
		cp.Interface.Tags = cloneStrings(overrideDefaults.Interface.Tags)
		cp.Vlan.Tags = cloneStrings(overrideDefaults.Vlan.Tags)
		cp.Prefix.Tags = cloneStrings(overrideDefaults.Prefix.Tags)
		cp.InterfacePatterns = clonePatterns(overrideDefaults.InterfacePatterns)
		cp.InterfaceExcludePatterns = cloneStrings(overrideDefaults.InterfaceExcludePatterns)
		return &cp
	}
	if overrideDefaults == nil {
		// Shallow-copy the struct, then clone every slice field so the result
		// doesn't alias policyDefaults' backing arrays.
		cp := *policyDefaults
		cp.Tags = cloneStrings(policyDefaults.Tags)
		cp.Device.Tags = cloneStrings(policyDefaults.Device.Tags)
		cp.Interface.Tags = cloneStrings(policyDefaults.Interface.Tags)
		cp.Vlan.Tags = cloneStrings(policyDefaults.Vlan.Tags)
		cp.Prefix.Tags = cloneStrings(policyDefaults.Prefix.Tags)
		cp.InterfacePatterns = clonePatterns(policyDefaults.InterfacePatterns)
		cp.InterfaceExcludePatterns = cloneStrings(policyDefaults.InterfaceExcludePatterns)
		return &cp
	}
	merged := *policyDefaults
	// Clone slices inherited from the policy copy so they don't alias the source.
	merged.Tags = cloneStrings(policyDefaults.Tags)
	merged.Device.Tags = cloneStrings(policyDefaults.Device.Tags)
	merged.Interface.Tags = cloneStrings(policyDefaults.Interface.Tags)
	merged.Vlan.Tags = cloneStrings(policyDefaults.Vlan.Tags)
	merged.Prefix.Tags = cloneStrings(policyDefaults.Prefix.Tags)
	merged.InterfacePatterns = clonePatterns(policyDefaults.InterfacePatterns)
	merged.InterfaceExcludePatterns = cloneStrings(policyDefaults.InterfaceExcludePatterns)

	if overrideDefaults.Site != "" {
		merged.Site = overrideDefaults.Site
	}
	if overrideDefaults.Role != "" {
		merged.Role = overrideDefaults.Role
	}
	if overrideDefaults.Location != "" {
		merged.Location = overrideDefaults.Location
	}
	if len(overrideDefaults.Tags) > 0 {
		merged.Tags = cloneStrings(overrideDefaults.Tags)
	}
	if overrideDefaults.Device.Manufacturer != "" {
		merged.Device.Manufacturer = overrideDefaults.Device.Manufacturer
	}
	if overrideDefaults.Device.Model != "" {
		merged.Device.Model = overrideDefaults.Device.Model
	}
	if overrideDefaults.Device.Platform != "" {
		merged.Device.Platform = overrideDefaults.Device.Platform
	}
	if overrideDefaults.Device.Comments != "" {
		merged.Device.Comments = overrideDefaults.Device.Comments
	}
	if len(overrideDefaults.Device.Tags) > 0 {
		merged.Device.Tags = cloneStrings(overrideDefaults.Device.Tags)
	}
	if overrideDefaults.Interface.Type != "" {
		merged.Interface.Type = overrideDefaults.Interface.Type
	}
	if overrideDefaults.Interface.Description != "" {
		merged.Interface.Description = overrideDefaults.Interface.Description
	}
	if len(overrideDefaults.Interface.Tags) > 0 {
		merged.Interface.Tags = cloneStrings(overrideDefaults.Interface.Tags)
	}
	if len(overrideDefaults.InterfacePatterns) > 0 {
		merged.InterfacePatterns = clonePatterns(overrideDefaults.InterfacePatterns)
	}
	if len(overrideDefaults.InterfaceExcludePatterns) > 0 {
		merged.InterfaceExcludePatterns = cloneStrings(overrideDefaults.InterfaceExcludePatterns)
	}
	if overrideDefaults.Vlan.Group != "" {
		merged.Vlan.Group = overrideDefaults.Vlan.Group
	}
	if overrideDefaults.Vlan.Tenant != "" {
		merged.Vlan.Tenant = overrideDefaults.Vlan.Tenant
	}
	if overrideDefaults.Vlan.Role != "" {
		merged.Vlan.Role = overrideDefaults.Vlan.Role
	}
	if overrideDefaults.Vlan.Description != "" {
		merged.Vlan.Description = overrideDefaults.Vlan.Description
	}
	if len(overrideDefaults.Vlan.Tags) > 0 {
		merged.Vlan.Tags = cloneStrings(overrideDefaults.Vlan.Tags)
	}
	if overrideDefaults.Prefix.Role != "" {
		merged.Prefix.Role = overrideDefaults.Prefix.Role
	}
	if overrideDefaults.Prefix.Tenant != "" {
		merged.Prefix.Tenant = overrideDefaults.Prefix.Tenant
	}
	if overrideDefaults.Prefix.Description != "" {
		merged.Prefix.Description = overrideDefaults.Prefix.Description
	}
	if len(overrideDefaults.Prefix.Tags) > 0 {
		merged.Prefix.Tags = cloneStrings(overrideDefaults.Prefix.Tags)
	}
	return &merged
}
