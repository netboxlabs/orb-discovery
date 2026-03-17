package policy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
)

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

// StartAll validates, applies defaults, then starts a Runner per policy.
func (m *Manager) StartAll(policies map[string]config.Policy) error {
	for name, policy := range policies {
		if err := m.validatePolicy(name, policy); err != nil {
			return err
		}
		applyDefaults(&policy)

		runner, err := NewRunner(m.ctx, m.logger, name, policy, m.otelEndpoint, m.otelExportPeriod)
		if err != nil {
			return err
		}
		runner.Start()
		m.policies[name] = runner
		m.logger.Info("started policy", "policy", name)
	}
	return nil
}

// Stop stops all running Runners.
func (m *Manager) Stop() error {
	for name, r := range m.policies {
		if err := r.Stop(); err != nil {
			return fmt.Errorf("stopping policy %s: %w", name, err)
		}
		delete(m.policies, name)
	}
	return nil
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
