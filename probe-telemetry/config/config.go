package config

import "time"

// Status represents the status of the probe-telemetry service
type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

// Policies represents a collection of policies (used for HTTP API body parsing)
type Policies struct {
	Policies map[string]Policy `yaml:"policies"`
}

// Defaults holds NetBox-contextual metadata that becomes OTLP attributes on
// every metric emitted for the associated probe target.
// Tags are exported as individual "tag_<value>=true" labels.
type Defaults struct {
	Site     string   `yaml:"site,omitempty"`
	Role     string   `yaml:"role,omitempty"`
	Location string   `yaml:"location,omitempty"`
	Tenant   string   `yaml:"tenant,omitempty"`
	Tags     []string `yaml:"tags,omitempty"`
}

// MergeDefaults merges override into base; non-zero override fields win.
// Tags are replaced (not appended) when override specifies any.
func MergeDefaults(base, override Defaults) Defaults {
	merged := base
	if override.Site != "" {
		merged.Site = override.Site
	}
	if override.Role != "" {
		merged.Role = override.Role
	}
	if override.Location != "" {
		merged.Location = override.Location
	}
	if override.Tenant != "" {
		merged.Tenant = override.Tenant
	}
	if len(override.Tags) > 0 {
		merged.Tags = override.Tags
	}
	return merged
}

// Policy groups a set of probes sharing a common defaults baseline.
type Policy struct {
	Defaults Defaults      `yaml:"defaults,omitempty"`
	Probes   []ProbeConfig `yaml:"probes"`
}

// Target is a single probe destination. OverrideDefaults, if set, takes
// precedence over the probe-level and policy-level defaults.
type Target struct {
	Host             string    `yaml:"host"`
	ID               string    `yaml:"id,omitempty"`
	OverrideDefaults *Defaults `yaml:"override_defaults,omitempty"`
}

// ProbeConfig describes one logical probe (potentially multiple targets).
type ProbeConfig struct {
	Name             string    `yaml:"name"`
	Type             string    `yaml:"type"`             // http, ping, dns, tcp
	Targets          []Target  `yaml:"targets"`
	Interval         string    `yaml:"interval"`         // e.g. "10s", "1m" — default "30s"
	Timeout          string    `yaml:"timeout"`          // e.g. "5s"        — default "10s"
	OverrideDefaults *Defaults `yaml:"override_defaults,omitempty"`
	HTTP             *HTTPConf `yaml:"http,omitempty"`
	Ping             *PingConf `yaml:"ping,omitempty"`
	DNS              *DNSConf  `yaml:"dns,omitempty"`
	TCP              *TCPConf  `yaml:"tcp,omitempty"`
}

// HTTPConf holds HTTP-probe-specific options.
type HTTPConf struct {
	Scheme  string            `yaml:"scheme"`  // HTTP (default) or HTTPS
	Path    string            `yaml:"path"`
	Port    int               `yaml:"port"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// PingConf holds ICMP-ping-probe-specific options.
type PingConf struct {
	PacketsPerProbe     int `yaml:"packets_per_probe"`
	PacketsIntervalMsec int `yaml:"packets_interval_msec"`
	PayloadSize         int `yaml:"payload_size"`
}

// DNSConf holds DNS-probe-specific options.
type DNSConf struct {
	Domain     string `yaml:"domain"`
	QueryType  string `yaml:"query_type"` // A, AAAA, MX
	MinAnswers int    `yaml:"min_answers"`
}

// TCPConf holds TCP-probe-specific options.
type TCPConf struct {
	Port         int  `yaml:"port"`
	TLSHandshake bool `yaml:"tls_handshake"`
}
