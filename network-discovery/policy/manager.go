package policy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/network-discovery/config"
	"gopkg.in/yaml.v3"
)

const DefaultAppName = "network-discovery"

type Manager struct {
	policies map[string]*Runner
	client   diode.Client
	logger   *slog.Logger
	ctx      context.Context
}

func (m *Manager) Configure(ctx context.Context, logger *slog.Logger, config config.StartupConfig) error {
	c, err := diode.NewClient(
		config.Target,
		DefaultAppName,
		"0.1.0",
		diode.WithAPIKey(config.ApiKey),
	)
	if err != nil {
		return err
	}
	m.ctx = ctx
	m.logger = logger
	m.client = c
	m.policies = make(map[string]*Runner)
	return nil
}

func (m *Manager) ParsePolicies(data []byte) (map[string]config.Policy, error) {
	var payload config.Config
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	if payload.Network.Policies == nil {
		return nil, errors.New("no policies found in the request")
	}

	return payload.Network.Policies, nil
}

func (m *Manager) HasPolicy(name string) bool {
	_, ok := m.policies[name]
	return ok
}

func (m *Manager) StartPolicy(name string, policy config.Policy) error {
	if len(policy.Scope.Targets) == 0 {
		return fmt.Errorf("%s : no targets found in the policy", name)
	}

	if !m.HasPolicy(name) {
		r := Runner{}
		if err := r.Configure(m.ctx, m.logger, name, policy, &m.client); err != nil {
			return err
		}

		r.Start()
		m.policies[name] = &r
	}
	return nil
}

func (m *Manager) StopPolicy(name string) error {
	if m.HasPolicy(name) {
		if err := m.policies[name].Stop(); err != nil {
			return err
		}
		delete(m.policies, name)
	}
	return nil
}

func (m *Manager) Stop() error {
	for name := range m.policies {
		if err := m.StopPolicy(name); err != nil {
			return err
		}
	}
	return nil
}
