package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
	"gopkg.in/yaml.v3"
)

// Status represents the status of a policy
type Status struct {
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	LastError   *string    `json:"last_error,omitempty"`
	LastErrorAt *time.Time `json:"last_error_at,omitempty"`
}

// Manager owns the lifecycle of all policy Runners.
type Manager struct {
	policies         map[string]*Runner
	logger           *slog.Logger
	ctx              context.Context
	otelEndpoint     string
	otelExportPeriod int
}

// NewManager creates a Manager. otelEndpoint may be empty (metrics export
// disabled).
func NewManager(ctx context.Context, logger *slog.Logger, otelEndpoint string, otelExportPeriod int) *Manager {
	return &Manager{
		policies:         make(map[string]*Runner),
		logger:           logger,
		ctx:              ctx,
		otelEndpoint:     otelEndpoint,
		otelExportPeriod: otelExportPeriod,
	}
}

// ParsePolicies parses and validates policies from a YAML request body
func (m *Manager) ParsePolicies(data []byte) (map[string]config.Policy, error) {
	var payload config.Policies
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if len(payload.Policies) == 0 {
		return nil, errors.New("no policies found in the request")
	}

	for name, policy := range payload.Policies {
		if err := m.validatePolicy(name, policy); err != nil {
			return nil, err
		}
		updated := policy
		applyDefaults(&updated)
		payload.Policies[name] = updated
	}

	return payload.Policies, nil
}

// HasPolicy checks if the policy exists
func (m *Manager) HasPolicy(name string) bool {
	_, ok := m.policies[name]
	return ok
}

// StartPolicy starts a single named policy
func (m *Manager) StartPolicy(name string, policy config.Policy) error {
	runner, err := NewRunner(m.ctx, m.logger, name, policy, m.otelEndpoint, m.otelExportPeriod)
	if err != nil {
		return err
	}
	runner.Start()
	m.policies[name] = runner
	m.logger.Info("started policy", "policy", name)
	return nil
}

// StopPolicy stops a single named policy
func (m *Manager) StopPolicy(name string) error {
	r, ok := m.policies[name]
	if !ok {
		return nil
	}
	if err := r.Stop(); err != nil {
		return fmt.Errorf("stopping policy %s: %w", name, err)
	}
	delete(m.policies, name)
	return nil
}

// Stop stops all running Runners.
func (m *Manager) Stop() error {
	for name := range m.policies {
		if err := m.StopPolicy(name); err != nil {
			return err
		}
	}
	return nil
}

// GetCapabilities returns the capabilities of probe-telemetry
func (m *Manager) GetCapabilities() []string {
	return []string{"probes"}
}

// GetPolicyStatuses returns the status of all known policies
func (m *Manager) GetPolicyStatuses() []Status {
	statuses := make([]Status, 0, len(m.policies))
	for name, runner := range m.policies {
		s := Status{Name: name, Status: "running"}
		if err, at := runner.GetLastError(); err != nil {
			msg := err.Error()
			s.Status = "running_with_errors"
			s.LastError = &msg
			s.LastErrorAt = &at
		}
		statuses = append(statuses, s)
	}
	return statuses
}

// validProbeTypes is the set of supported probe type strings.
var validProbeTypes = map[string]bool{
	"http": true,
	"ping": true,
	"dns":  true,
	"tcp":  true,
}

func (m *Manager) validatePolicy(name string, policy config.Policy) error {
	if len(policy.Probes) == 0 {
		return fmt.Errorf("policy %s: no probes defined", name)
	}
	for _, probe := range policy.Probes {
		if probe.Name == "" {
			return fmt.Errorf("policy %s: probe is missing a name", name)
		}
		if !validProbeTypes[strings.ToLower(probe.Type)] {
			return fmt.Errorf("policy %s, probe %s: unsupported type %q (must be one of: http, ping, dns, tcp)",
				name, probe.Name, probe.Type)
		}
		if len(probe.Targets) == 0 {
			return fmt.Errorf("policy %s, probe %s: no targets defined", name, probe.Name)
		}
		for _, t := range probe.Targets {
			if t.Host == "" {
				return fmt.Errorf("policy %s, probe %s: target is missing a host", name, probe.Name)
			}
		}
	}
	return nil
}

// applyDefaults fills in missing interval/timeout with safe defaults.
func applyDefaults(policy *config.Policy) {
	for i := range policy.Probes {
		if policy.Probes[i].Interval == "" {
			policy.Probes[i].Interval = "30s"
		}
		if policy.Probes[i].Timeout == "" {
			policy.Probes[i].Timeout = "10s"
		}
	}
}
