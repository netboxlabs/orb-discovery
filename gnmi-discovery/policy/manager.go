package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
	"gopkg.in/yaml.v3"
)

// Manager owns the set of running policies.
type Manager struct {
	// mu guards the policies map — the HTTP server calls StartPolicy /
	// StopPolicy / GetPolicyStatuses from concurrent request goroutines, so an
	// unguarded map would hit Go's "concurrent map iteration and map write"
	// panic. Runner-internal state has its own locks; mu only protects the map.
	mu       sync.RWMutex
	policies map[string]*Runner
	client   diode.Client
	logger   *slog.Logger
	ctx      context.Context
}

// NewManager returns a new policy manager.
func NewManager(ctx context.Context, logger *slog.Logger, client diode.Client) (*Manager, error) {
	return &Manager{
		ctx:      ctx,
		client:   client,
		logger:   logger,
		policies: make(map[string]*Runner),
	}, nil
}

// ParsePolicies unmarshals and validates the request body.
func (m *Manager) ParsePolicies(data []byte) (map[string]config.Policy, error) {
	var payload config.Policies
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if len(payload.Policies) == 0 {
		return nil, errors.New("no policies found in the request")
	}
	return payload.Policies, nil
}

// HasPolicy reports whether a policy is running.
func (m *Manager) HasPolicy(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.policies[name]
	return ok
}

// StartPolicy starts a policy (no-op runner until M6).
func (m *Manager) StartPolicy(name string, policy config.Policy) error {
	if len(policy.Scope.Targets) == 0 {
		return fmt.Errorf("%s : no targets found in the policy", name)
	}
	m.mu.Lock()
	if _, ok := m.policies[name]; ok {
		m.mu.Unlock()
		return nil // already running
	}
	r, err := NewRunner(m.ctx, m.logger, name, policy, m.client)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	m.policies[name] = r
	m.mu.Unlock()
	r.Start() // launch goroutines outside the lock
	return nil
}

// StopPolicy stops and removes a policy. The runner is detached under the lock,
// then stopped outside it (Stop blocks on goroutine unwind — not something to
// hold the map lock for).
func (m *Manager) StopPolicy(name string) error {
	m.mu.Lock()
	r, ok := m.policies[name]
	if ok {
		delete(m.policies, name)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}
	return r.Stop()
}

// Stop stops all policies.
func (m *Manager) Stop() error {
	m.mu.Lock()
	runners := make([]*Runner, 0, len(m.policies))
	for _, r := range m.policies {
		runners = append(runners, r)
	}
	m.policies = make(map[string]*Runner)
	m.mu.Unlock()
	for _, r := range runners {
		if err := r.Stop(); err != nil {
			return err
		}
	}
	return nil
}

// GetCapabilities returns the backend capabilities.
func (m *Manager) GetCapabilities() []string {
	return []string{"targets", "on_change", "sample", "get"}
}

// Status is the per-policy status surfaced by /api/v1/status.
type Status struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// GetPolicyStatuses returns the status of all known policies. (M6.3 replaces the
// body with run-derived status; the RLock guard stays.)
func (m *Manager) GetPolicyStatuses() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	statuses := make([]Status, 0, len(m.policies))
	for name, r := range m.policies {
		statuses = append(statuses, Status{Name: name, Status: r.State()})
	}
	return statuses
}
