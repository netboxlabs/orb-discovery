package policy

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func newTestManager() *Manager {
	return NewManager(context.Background(), testLogger, "", 10)
}

func validPolicy() config.Policy {
	return config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "web",
			Type:    "http",
			Targets: []config.Target{{Host: "example.com"}},
		}},
	}
}

// ---------------------------------------------------------------------------
// validatePolicy
// ---------------------------------------------------------------------------

func TestValidate_OK(t *testing.T) {
	m := newTestManager()
	err := m.validatePolicy("test", validPolicy())
	require.NoError(t, err)
}

func TestValidate_NoProbes(t *testing.T) {
	m := newTestManager()
	err := m.validatePolicy("test", config.Policy{})
	assert.ErrorContains(t, err, "no probes defined")
}

func TestValidate_EmptyProbeName(t *testing.T) {
	m := newTestManager()
	pol := config.Policy{
		Probes: []config.ProbeConfig{{Type: "http", Targets: []config.Target{{Host: "h.com"}}}},
	}
	err := m.validatePolicy("test", pol)
	assert.ErrorContains(t, err, "missing a name")
}

func TestValidate_UnsupportedType(t *testing.T) {
	m := newTestManager()
	pol := config.Policy{
		Probes: []config.ProbeConfig{{Name: "x", Type: "udp", Targets: []config.Target{{Host: "h.com"}}}},
	}
	err := m.validatePolicy("test", pol)
	assert.ErrorContains(t, err, "unsupported type")
}

func TestValidate_SupportedTypesAreCaseInsensitive(t *testing.T) {
	m := newTestManager()
	for _, typ := range []string{"HTTP", "Ping", "DNS", "TCP", "http", "ping", "dns", "tcp"} {
		pol := config.Policy{
			Probes: []config.ProbeConfig{{Name: "x", Type: typ, Targets: []config.Target{{Host: "h.com"}}}},
		}
		assert.NoError(t, m.validatePolicy("test", pol), "type %q should be valid", typ)
	}
}

func TestValidate_NoTargets(t *testing.T) {
	m := newTestManager()
	pol := config.Policy{
		Probes: []config.ProbeConfig{{Name: "x", Type: "http", Targets: []config.Target{}}},
	}
	err := m.validatePolicy("test", pol)
	assert.ErrorContains(t, err, "no targets defined")
}

func TestValidate_EmptyTargetHost(t *testing.T) {
	m := newTestManager()
	pol := config.Policy{
		Probes: []config.ProbeConfig{{Name: "x", Type: "http", Targets: []config.Target{{Host: ""}}}},
	}
	err := m.validatePolicy("test", pol)
	assert.ErrorContains(t, err, "missing a host")
}

// ---------------------------------------------------------------------------
// applyDefaults
// ---------------------------------------------------------------------------

func TestApplyDefaults_FillsIntervalAndTimeout(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{Name: "x", Type: "http", Targets: []config.Target{{Host: "h.com"}}}},
	}
	applyDefaults(&pol)
	assert.Equal(t, "30s", pol.Probes[0].Interval)
	assert.Equal(t, "10s", pol.Probes[0].Timeout)
}

func TestApplyDefaults_PreservesExistingValues(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:     "x",
			Type:     "http",
			Targets:  []config.Target{{Host: "h.com"}},
			Interval: "5s",
			Timeout:  "3s",
		}},
	}
	applyDefaults(&pol)
	assert.Equal(t, "5s", pol.Probes[0].Interval)
	assert.Equal(t, "3s", pol.Probes[0].Timeout)
}

// ---------------------------------------------------------------------------
// Manager.Stop on empty manager
// ---------------------------------------------------------------------------

func TestStop_EmptyManager(t *testing.T) {
	m := newTestManager()
	assert.NoError(t, m.Stop())
}
