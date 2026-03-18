package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/netboxlabs/orb-discovery/snmp-telemetry/config"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/env"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/snmp"
	"gopkg.in/yaml.v3"
)

const (
	// SNMPDefaultPort is the default SNMP port
	SNMPDefaultPort = 161
)

// Status represents the status of a policy
type Status struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Manager manages snmp-telemetry policy runners
type Manager struct {
	policies           map[string]*Runner
	logger             *slog.Logger
	ctx                context.Context
	defaultProfilesDir string
}

// NewManager returns a new policy manager
func NewManager(ctx context.Context, logger *slog.Logger, defaultProfilesDir string) *Manager {
	return &Manager{
		ctx:                ctx,
		logger:             logger,
		policies:           make(map[string]*Runner),
		defaultProfilesDir: defaultProfilesDir,
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
		if err := m.validatePolicy(policy); err != nil {
			return nil, fmt.Errorf("%s: invalid policy: %w", name, err)
		}
	}

	for name := range payload.Policies {
		updated := payload.Policies[name]
		if err := m.resolveAuthenticationEnvVars(&updated); err != nil {
			return nil, fmt.Errorf("%s: failed to resolve environment variables: %w", name, err)
		}
		m.applyDefaults(&updated)
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
	if m.HasPolicy(name) {
		return fmt.Errorf("policy %s already exists", name)
	}

	clientFactory := func(host string, port uint16, retries int, timeout time.Duration, authentication *config.Authentication, logger *slog.Logger) (snmp.Walker, error) {
		return snmp.NewClient(host, port, retries, timeout, authentication, logger)
	}

	r, err := NewRunner(m.ctx, m.logger, name, policy, clientFactory, m.defaultProfilesDir)
	if err != nil {
		return err
	}

	r.Start()
	m.policies[name] = r
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

// Stop stops all running policies
func (m *Manager) Stop() error {
	for name := range m.policies {
		if err := m.StopPolicy(name); err != nil {
			return err
		}
	}
	return nil
}

// GetCapabilities returns the capabilities of snmp-telemetry
func (m *Manager) GetCapabilities() []string {
	return []string{"targets"}
}

// GetPolicyStatuses returns the status of all known policies
func (m *Manager) GetPolicyStatuses() []Status {
	statuses := make([]Status, 0, len(m.policies))
	for name := range m.policies {
		statuses = append(statuses, Status{Name: name, Status: "running"})
	}
	return statuses
}

// applyDefaults applies the default values to the policy
func (m *Manager) applyDefaults(policy *config.Policy) {
	for i, target := range policy.Scope.Targets {
		if target.Port == 0 {
			policy.Scope.Targets[i].Port = SNMPDefaultPort
		}
	}
}

// validateAuthentication validates a single authentication configuration
func (m *Manager) validateAuthentication(auth *config.Authentication, context string) error {
	if auth == nil {
		return fmt.Errorf("%s: authentication is nil", context)
	}

	if auth.ProtocolVersion == "" {
		return fmt.Errorf("%s: missing protocol version", context)
	}

	if auth.ProtocolVersion != "SNMPv1" && auth.ProtocolVersion != "SNMPv2c" && auth.ProtocolVersion != "SNMPv3" {
		return fmt.Errorf("%s: unsupported protocol version", context)
	}

	if auth.ProtocolVersion == "SNMPv2c" || auth.ProtocolVersion == "SNMPv1" {
		if auth.Community == "" {
			return fmt.Errorf("%s: missing community", context)
		}
	}

	if auth.ProtocolVersion == "SNMPv3" {
		if auth.SecurityLevel != "noAuthNoPriv" &&
			auth.SecurityLevel != "authNoPriv" &&
			auth.SecurityLevel != "authPriv" {
			return fmt.Errorf("%s: invalid security level %s", context, auth.SecurityLevel)
		}
		if auth.SecurityLevel == "authNoPriv" || auth.SecurityLevel == "authPriv" {
			if auth.Username == "" {
				return fmt.Errorf("%s: missing username", context)
			}
			if auth.AuthPassphrase == "" {
				return fmt.Errorf("%s: missing auth passphrase", context)
			}
			if auth.AuthProtocol == "" {
				return fmt.Errorf("%s: missing auth protocol", context)
			}
		}
		if auth.SecurityLevel == "authPriv" {
			if auth.PrivPassphrase == "" {
				return fmt.Errorf("%s: missing priv passphrase", context)
			}
			if auth.PrivProtocol == "" {
				return fmt.Errorf("%s: missing priv protocol", context)
			}
		}
	}

	return nil
}

// validatePolicy validates the policy
func (m *Manager) validatePolicy(policy config.Policy) error {
	hasPolicyAuth := policy.Scope.Authentication.ProtocolVersion != ""

	if hasPolicyAuth {
		if err := m.validateAuthentication(&policy.Scope.Authentication, "policy-level"); err != nil {
			return err
		}
	}

	for _, target := range policy.Scope.Targets {
		if target.Authentication != nil {
			context := fmt.Sprintf("target %s", target.Host)
			if err := m.validateAuthentication(target.Authentication, context); err != nil {
				return err
			}
		} else if !hasPolicyAuth {
			return fmt.Errorf("target %s: no authentication configured and no policy-level fallback available", target.Host)
		}
	}

	if policy.Config.MetricsInterval == nil || *policy.Config.MetricsInterval <= 0 {
		return fmt.Errorf("metrics_interval must be a positive integer")
	}

	return nil
}

// resolveAuthenticationEnvVarsForAuth resolves environment variables for a single Authentication
func (m *Manager) resolveAuthenticationEnvVarsForAuth(auth *config.Authentication, context string) error {
	if auth == nil {
		return nil
	}

	fields := []struct {
		field *string
		label string
	}{
		{&auth.Community, "community"},
		{&auth.Username, "username"},
		{&auth.AuthPassphrase, "auth_passphrase"},
		{&auth.PrivPassphrase, "priv_passphrase"},
	}

	for _, f := range fields {
		resolved, err := env.ResolveEnv(*f.field)
		if err != nil {
			return fmt.Errorf("%s: failed to resolve %s environment variable: %w", context, f.label, err)
		}
		*f.field = resolved
	}

	return nil
}

// resolveAuthenticationEnvVars resolves environment variables in authentication configuration
func (m *Manager) resolveAuthenticationEnvVars(policy *config.Policy) error {
	if err := m.resolveAuthenticationEnvVarsForAuth(&policy.Scope.Authentication, "policy-level"); err != nil {
		return err
	}

	for i := range policy.Scope.Targets {
		if policy.Scope.Targets[i].Authentication != nil {
			context := fmt.Sprintf("target %s", policy.Scope.Targets[i].Host)
			if err := m.resolveAuthenticationEnvVarsForAuth(policy.Scope.Targets[i].Authentication, context); err != nil {
				return err
			}
		}
	}

	return nil
}
