package config

import "time"

// Status represents the runtime status of the gnmi-discovery service.
type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

// Policy is a gnmi-discovery policy. Fleshed out in M1.
type Policy struct {
	Config PolicyConfig `yaml:"config"`
	Scope  Scope        `yaml:"scope"`
}

// Policies is the request envelope: {policies: {name: Policy}}.
type Policies struct {
	Policies map[string]Policy `yaml:"policies"`
}

// PolicyConfig holds policy-wide config. Fleshed out in M1.
type PolicyConfig struct {
	Defaults Defaults `yaml:"defaults"`
}

// Scope holds the targets. Fleshed out in M1.
type Scope struct {
	Targets []Target `yaml:"targets"`
}

// Target is one gNMI endpoint. Fleshed out in M1.
type Target struct {
	Host string `yaml:"host"`
}

// Defaults holds NetBox defaults applied to discovered entities. Fleshed out in M1.
type Defaults struct {
	Site string `yaml:"site,omitempty"`
	Role string `yaml:"role,omitempty"`
}
