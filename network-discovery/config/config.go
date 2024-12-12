package config

import "time"

// Status represents the status of the network-discovery service
type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

// Scope represents the scope of a policy
type Scope struct {
	Targets []string `yaml:"targets"`
	Timeout int      `yaml:"timeout"`
}

// PolicyConfig represents the configuration of a policy
type PolicyConfig struct {
	Schedule *string           `yaml:"schedule"`
	Defaults map[string]string `yaml:"defaults"`
}

// Policy represents a network-discovery policy
type Policy struct {
	Config PolicyConfig `yaml:"config"`
	Scope  Scope        `yaml:"scope"`
}

// StartupConfig represents the configuration of the network-discovery service
type StartupConfig struct {
	Host      string `yaml:"host"`
	Port      int32  `yaml:"port"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
}

// Network represents the network-discovery configuration
type Network struct {
	Config   StartupConfig     `yaml:"config"`
	Policies map[string]Policy `mapstructure:"policies"`
}

// DiodeConfig represents the configuration of diode service
type DiodeConfig struct {
	Target string `yaml:"target"`
	APIKey string `yaml:"api_key"`
}

// Diode represents the root configuration of diode service
type Diode struct {
	Config DiodeConfig `yaml:"config"`
}

// Config represents the root configuration of the network-discovery service
type Config struct {
	Diode   Diode   `yaml:"diode"`
	Network Network `yaml:"network_discovery"`
}
