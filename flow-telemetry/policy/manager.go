package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"gopkg.in/yaml.v3"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/config"
	"github.com/netboxlabs/orb-discovery/flow-telemetry/rollup"
)

// Status represents the status of a policy.
type Status struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Manager manages flow-telemetry policy runners.
type Manager struct {
	policies map[string]*Runner
	logger   *slog.Logger
	ctx      context.Context
}

// NewManager returns a new policy manager.
func NewManager(ctx context.Context, logger *slog.Logger) *Manager {
	return &Manager{
		ctx:      ctx,
		logger:   logger,
		policies: make(map[string]*Runner),
	}
}

// ParsePolicies parses and validates policies from a YAML request body.
func (m *Manager) ParsePolicies(data []byte) (map[string]config.Policy, error) {
	var payload config.Policies
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if len(payload.Policies) == 0 {
		return nil, errors.New("no policies found in the request")
	}

	for name, policy := range payload.Policies {
		if err := m.validatePolicy(policy); err != nil {
			return nil, fmt.Errorf("%s: invalid policy: %w", name, err)
		}
	}

	return payload.Policies, nil
}

// HasPolicy checks whether the named policy is running.
func (m *Manager) HasPolicy(name string) bool {
	_, ok := m.policies[name]
	return ok
}

// StartPolicy starts a single named policy.
func (m *Manager) StartPolicy(name string, policy config.Policy) error {
	if m.HasPolicy(name) {
		return fmt.Errorf("policy %s already exists", name)
	}

	r := NewRunner(m.ctx, m.logger, name, policy)
	if err := r.Start(); err != nil {
		return err
	}

	m.policies[name] = r
	m.logger.Info("started policy", "policy", name)
	return nil
}

// StopPolicy stops the named policy.
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

// Stop stops all running policies.
func (m *Manager) Stop() error {
	for name := range m.policies {
		if err := m.StopPolicy(name); err != nil {
			return err
		}
	}
	return nil
}

// GetCapabilities returns the capabilities of flow-telemetry.
func (m *Manager) GetCapabilities() []string {
	return []string{"flow"}
}

// GetPolicyStatuses returns the status of all known policies.
func (m *Manager) GetPolicyStatuses() []Status {
	statuses := make([]Status, 0, len(m.policies))
	for name := range m.policies {
		statuses = append(statuses, Status{Name: name, Status: "running"})
	}
	return statuses
}

// validatePolicy checks that the policy config is valid.
func (m *Manager) validatePolicy(policy config.Policy) error {
	cfg := policy.Config

	if policy.Scope.Port <= 0 {
		return fmt.Errorf("port must be a positive integer")
	}

	if len(cfg.Rollups) == 0 {
		return fmt.Errorf("at least one rollup must be defined")
	}

	for i, r := range cfg.Rollups {
		if r.Name == "" {
			return fmt.Errorf("rollup[%d]: name is required", i)
		}
		if !rollup.ValidMethods[r.Method] {
			return fmt.Errorf("rollup %q: unsupported method %q (must be sum, max, or min)", r.Name, r.Method)
		}
		if len(r.Metrics) == 0 {
			return fmt.Errorf("rollup %q: at least one metric is required", r.Name)
		}
		for _, metric := range r.Metrics {
			if !rollup.ValidMetrics[metric] {
				return fmt.Errorf("rollup %q: unsupported metric %q (must be bytes or packets)", r.Name, metric)
			}
		}
		for _, dim := range r.Dimensions {
			if !rollup.ValidDimensions[dim] {
				return fmt.Errorf("rollup %q: unsupported dimension %q", r.Name, dim)
			}
		}
	}

	return nil
}
