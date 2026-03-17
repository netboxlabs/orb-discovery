package policy

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/netboxlabs/orb-discovery/snmp-telemetry/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testLogger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

func newTestManager() *Manager {
	return NewManager(context.Background(), testLogger, "")
}

func ptrInt(v int) *int { return &v }

func v2cAuth() config.Authentication {
	return config.Authentication{ProtocolVersion: "SNMPv2c", Community: "public"}
}

func v3AuthAuth() config.Authentication {
	return config.Authentication{
		ProtocolVersion: "SNMPv3",
		SecurityLevel:   "authNoPriv",
		Username:        "admin",
		AuthPassphrase:  "secret123",
		AuthProtocol:    "SHA",
	}
}

func v3PrivAuth() config.Authentication {
	return config.Authentication{
		ProtocolVersion: "SNMPv3",
		SecurityLevel:   "authPriv",
		Username:        "admin",
		AuthPassphrase:  "secret123",
		AuthProtocol:    "SHA",
		PrivPassphrase:  "priv456",
		PrivProtocol:    "AES",
	}
}

func minimalPolicy(auth config.Authentication) config.Policy {
	interval := 60
	return config.Policy{
		Config: config.PolicyConfig{MetricsInterval: &interval},
		Scope: config.Scope{
			Authentication: auth,
			Targets:        []config.Target{{Host: "192.168.1.1"}},
		},
	}
}

// ---------------------------------------------------------------------------
// validatePolicy — authentication
// ---------------------------------------------------------------------------

func TestValidate_V2cPolicyAuth(t *testing.T) {
	m := newTestManager()
	require.NoError(t, m.validatePolicy(minimalPolicy(v2cAuth())))
}

func TestValidate_V3PolicyAuth_AuthNoPriv(t *testing.T) {
	m := newTestManager()
	require.NoError(t, m.validatePolicy(minimalPolicy(v3AuthAuth())))
}

func TestValidate_V3PolicyAuth_AuthPriv(t *testing.T) {
	m := newTestManager()
	require.NoError(t, m.validatePolicy(minimalPolicy(v3PrivAuth())))
}

func TestValidate_NoAuthNoPolicyFallback(t *testing.T) {
	m := newTestManager()
	interval := 60
	pol := config.Policy{
		Config: config.PolicyConfig{MetricsInterval: &interval},
		Scope: config.Scope{
			Targets: []config.Target{{Host: "192.168.1.1"}},
		},
	}
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "no authentication configured")
}

func TestValidate_MissingProtocolVersion(t *testing.T) {
	// Authentication without ProtocolVersion is not treated as a policy-level auth,
	// so targets without per-target auth get "no authentication configured" error.
	m := newTestManager()
	auth := config.Authentication{Community: "public"} // no ProtocolVersion
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.Error(t, err)
}

func TestValidate_PerTargetMissingProtocolVersion(t *testing.T) {
	// Per-target auth with no ProtocolVersion triggers the missing protocol version error.
	m := newTestManager()
	bad := config.Authentication{Community: "public"} // no ProtocolVersion
	interval := 60
	pol := config.Policy{
		Config: config.PolicyConfig{MetricsInterval: &interval},
		Scope: config.Scope{
			Targets: []config.Target{{Host: "10.0.0.1", Authentication: &bad}},
		},
	}
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing protocol version")
}

func TestValidate_UnsupportedProtocolVersion(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{ProtocolVersion: "SNMPv99", Community: "public"}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "unsupported protocol version")
}

func TestValidate_V2cMissingCommunity(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{ProtocolVersion: "SNMPv2c"}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing community")
}

func TestValidate_V1MissingCommunity(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{ProtocolVersion: "SNMPv1"}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing community")
}

func TestValidate_V3InvalidSecurityLevel(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{ProtocolVersion: "SNMPv3", SecurityLevel: "bad"}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "invalid security level")
}

func TestValidate_V3AuthNoPrivMissingUser(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{
		ProtocolVersion: "SNMPv3", SecurityLevel: "authNoPriv",
		AuthPassphrase: "secret", AuthProtocol: "SHA",
	}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing username")
}

func TestValidate_V3AuthNoPrivMissingPassphrase(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{
		ProtocolVersion: "SNMPv3", SecurityLevel: "authNoPriv",
		Username: "admin", AuthProtocol: "SHA",
	}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing auth passphrase")
}

func TestValidate_V3AuthPrivMissingPrivPassphrase(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{
		ProtocolVersion: "SNMPv3", SecurityLevel: "authPriv",
		Username: "admin", AuthPassphrase: "secret", AuthProtocol: "SHA",
		PrivProtocol: "AES",
	}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing priv passphrase")
}

func TestValidate_V3AuthPrivMissingPrivProtocol(t *testing.T) {
	m := newTestManager()
	auth := config.Authentication{
		ProtocolVersion: "SNMPv3", SecurityLevel: "authPriv",
		Username: "admin", AuthPassphrase: "secret", AuthProtocol: "SHA",
		PrivPassphrase: "priv",
	}
	pol := minimalPolicy(auth)
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "missing priv protocol")
}

func TestValidate_PerTargetAuth(t *testing.T) {
	m := newTestManager()
	interval := 60
	auth := v2cAuth()
	pol := config.Policy{
		Config: config.PolicyConfig{MetricsInterval: &interval},
		Scope: config.Scope{
			Targets: []config.Target{{Host: "10.0.0.1", Authentication: &auth}},
		},
	}
	require.NoError(t, m.validatePolicy(pol))
}

// ---------------------------------------------------------------------------
// validatePolicy — metrics_interval
// ---------------------------------------------------------------------------

func TestValidate_MissingMetricsInterval(t *testing.T) {
	m := newTestManager()
	pol := config.Policy{
		Scope: config.Scope{
			Authentication: v2cAuth(),
			Targets:        []config.Target{{Host: "10.0.0.1"}},
		},
	}
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "metrics_interval")
}

func TestValidate_ZeroMetricsInterval(t *testing.T) {
	m := newTestManager()
	zero := 0
	pol := config.Policy{
		Config: config.PolicyConfig{MetricsInterval: &zero},
		Scope: config.Scope{
			Authentication: v2cAuth(),
			Targets:        []config.Target{{Host: "10.0.0.1"}},
		},
	}
	err := m.validatePolicy(pol)
	assert.ErrorContains(t, err, "metrics_interval")
}

// ---------------------------------------------------------------------------
// applyDefaults
// ---------------------------------------------------------------------------

func TestApplyDefaults_SetsDefaultPort(t *testing.T) {
	m := newTestManager()
	pol := minimalPolicy(v2cAuth())
	pol.Scope.Targets[0].Port = 0
	m.applyDefaults(&pol)
	assert.Equal(t, uint16(SNMPDefaultPort), pol.Scope.Targets[0].Port)
}

func TestApplyDefaults_PreservesExistingPort(t *testing.T) {
	m := newTestManager()
	pol := minimalPolicy(v2cAuth())
	pol.Scope.Targets[0].Port = 1161
	m.applyDefaults(&pol)
	assert.Equal(t, uint16(1161), pol.Scope.Targets[0].Port)
}

// ---------------------------------------------------------------------------
// Stop on empty manager
// ---------------------------------------------------------------------------

func TestStop_EmptyManager(t *testing.T) {
	m := newTestManager()
	assert.NoError(t, m.Stop())
}
