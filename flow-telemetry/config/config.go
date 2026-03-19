package config

import "time"

// Status represents the runtime status of flow-telemetry.
type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

// Policies is the top-level YAML envelope for a policy submission.
type Policies struct {
	Policies map[string]Policy `yaml:"policies"`
}

// Rollup defines a single flow aggregation rule.
type Rollup struct {
	// Method is the aggregation function: sum, max, or min.
	Method string `yaml:"method"`
	// Name is used as the metric name suffix: flow.<name>.
	Name string `yaml:"name"`
	// Metrics lists the flow fields to aggregate (bytes, packets).
	Metrics []string `yaml:"metrics"`
	// Dimensions lists the flow fields to group by (src_addr, dst_addr, proto, …).
	Dimensions []string `yaml:"dimensions"`
}

// PolicyConfig is the configuration block of a flow-telemetry policy.
type PolicyConfig struct {
	// Protocol selects the flow decoder: auto, netflow5, netflow9, ipfix, sflow.
	// Defaults to "auto".
	Protocol string `yaml:"protocol,omitempty"`
	// Workers sets the number of UDP receiver goroutines. Defaults to 2.
	Workers int `yaml:"workers,omitempty"`
	// QueueSize sets the UDP receive queue depth. Defaults to 10000.
	QueueSize int `yaml:"queue_size,omitempty"`
	// Rollups defines the aggregation rules. At least one is required.
	Rollups []Rollup `yaml:"rollups"`
}

// Scope identifies the flow listener endpoint.
type Scope struct {
	// Host is the IP address to bind the UDP listener to. Defaults to "0.0.0.0".
	Host string `yaml:"host,omitempty"`
	// Port is the UDP port to listen on (required).
	Port int `yaml:"port"`
	// ID is an optional identifier attached to all exported metrics as an OTLP attribute.
	// Use it to distinguish between multiple flow-telemetry instances (e.g. by site or role).
	ID string `yaml:"id,omitempty"`
}

// Policy is a single flow-telemetry policy.
type Policy struct {
	Config PolicyConfig `yaml:"config"`
	Scope  Scope        `yaml:"scope"`
}
