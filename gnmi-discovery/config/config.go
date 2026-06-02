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
	SkipVerify bool   `yaml:"skip_verify,omitempty"`
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

// Defaults holds NetBox defaults applied to discovered entities.
type Defaults struct {
	Site      string            `yaml:"site,omitempty"`
	Location  string            `yaml:"location,omitempty"`
	Role      string            `yaml:"role,omitempty"`
	Tags      []string          `yaml:"tags,omitempty"`
	Device    DeviceDefaults    `yaml:"device,omitempty"`
	Interface InterfaceDefaults `yaml:"interface,omitempty"`
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

// MergeDefaults returns policyDefaults overlaid with non-empty overrideDefaults.
func MergeDefaults(policyDefaults, overrideDefaults *Defaults) *Defaults {
	if overrideDefaults == nil {
		return policyDefaults
	}
	merged := *policyDefaults
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
		merged.Tags = overrideDefaults.Tags
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
		merged.Device.Tags = overrideDefaults.Device.Tags
	}
	if overrideDefaults.Interface.Type != "" {
		merged.Interface.Type = overrideDefaults.Interface.Type
	}
	if overrideDefaults.Interface.Description != "" {
		merged.Interface.Description = overrideDefaults.Interface.Description
	}
	if len(overrideDefaults.Interface.Tags) > 0 {
		merged.Interface.Tags = overrideDefaults.Interface.Tags
	}
	return &merged
}
