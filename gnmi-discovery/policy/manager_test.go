package policy

import (
	"context"
	"log/slog"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	var client diode.Client
	m, err := NewManager(context.Background(), slog.Default(), client)
	require.NoError(t, err)
	return m
}

func TestParseAppliesDefaults(t *testing.T) {
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config:
      defaults: {}
    scope:
      targets:
        - host: 10.0.0.1
`)
	policies, err := m.ParsePolicies(data)
	require.NoError(t, err)
	p := policies["p1"]
	require.Equal(t, "auto", p.Config.Mode)
	require.Equal(t, 2000, p.Config.DebounceMs)
	require.Equal(t, "10.0.0.1:9339", p.Scope.Targets[0].Host) // default port appended
	require.Equal(t, "undefined", p.Config.Defaults.Site)
	require.Equal(t, "undefined", p.Config.Defaults.Role)
}

func TestApplyDefaultsIPv6PortAppend(t *testing.T) {
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config: {}
    scope:
      targets:
        - host: "2001:db8::1"
        - host: "[2001:db8::2]:57400"
        - host: "10.0.0.5:6030"
`)
	policies, err := m.ParsePolicies(data)
	require.NoError(t, err)
	hosts := []string{
		policies["p1"].Scope.Targets[0].Host,
		policies["p1"].Scope.Targets[1].Host,
		policies["p1"].Scope.Targets[2].Host,
	}
	require.Equal(t, "[2001:db8::1]:9339", hosts[0]) // bare IPv6 bracketed + default port
	require.Equal(t, "[2001:db8::2]:57400", hosts[1]) // already has port -> untouched
	require.Equal(t, "10.0.0.5:6030", hosts[2])        // already has port -> untouched
}

func TestValidateRejectsBadMode(t *testing.T) {
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config:
      mode: streaming
    scope:
      targets:
        - host: 10.0.0.1
`)
	_, err := m.ParsePolicies(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid mode")
}

func TestValidateRequiresTargets(t *testing.T) {
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config: {}
    scope:
      targets: []
`)
	_, err := m.ParsePolicies(data)
	require.Error(t, err)
}

func TestResolvesEnvInCredentials(t *testing.T) {
	t.Setenv("GNMI_PW", "s3cret")
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config: {}
    scope:
      targets:
        - host: 10.0.0.1
          username: admin
          password: ${GNMI_PW}
`)
	policies, err := m.ParsePolicies(data)
	require.NoError(t, err)
	require.Equal(t, "s3cret", policies["p1"].Scope.Targets[0].Password)
}

func TestResolvesEnvInHostAndTLS(t *testing.T) {
	t.Setenv("GNMI_HOST", "10.9.9.9")
	t.Setenv("GNMI_CA", "/run/secrets/ca.pem")
	m := newTestManager(t)
	data := []byte(`
policies:
  p1:
    config: {}
    scope:
      targets:
        - host: ${GNMI_HOST}
          username: admin
          password: pw
          tls:
            ca: ${GNMI_CA}
`)
	policies, err := m.ParsePolicies(data)
	require.NoError(t, err)
	tgt := policies["p1"].Scope.Targets[0]
	require.Equal(t, "10.9.9.9:9339", tgt.Host) // resolved, THEN default port appended
	require.Equal(t, "/run/secrets/ca.pem", tgt.TLS.CAFile)
}
