package config

import "time"

type Status struct {
	StartTime     time.Time `json:"start_time"`
	UpTimeSeconds int64     `json:"up_time_seconds"`
	Version       string    `json:"version"`
}

type Scope struct {
	Targets []string `yaml:"targets"`
}

type PolicyConfig struct {
	Schedule *string           `yaml:"schedule"`
	Defaults map[string]string `yaml:"defaults"`
}

type Policy struct {
	Config PolicyConfig `yaml:"config"`
	Scope  Scope        `yaml:"scope"`
}

type StartupConfig struct {
	Target string `yaml:"target"`
	ApiKey string `yaml:"api_key"`
	Host   string `yaml:"host"`
	Port   int32  `yaml:"port"`
}

type Network struct {
	Config   StartupConfig     `yaml:"config"`
	Policies map[string]Policy `mapstructure:"policies"`
}

type Config struct {
	Network Network `mapstructure:"network"`
}
