package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestUnmarshalPolicy(t *testing.T) {
	data := []byte(`
policies:
  gnmi_fabric:
    config:
      debounce_ms: 2000
      mode: auto
      sample_interval_ms: 300000
      get_interval_ms: 900000
      defaults:
        site: New York NY
        role: Router
    scope:
      targets:
        - host: 10.0.0.11:6030
          username: ${GNMI_USER}
          password: ${GNMI_PASS}
          tls:
            skip_verify: true
          profile: arista_eos
        - host: 10.0.0.21:57400
          username: admin
          password: pw
          mode: on_change
`)
	var p Policies
	require.NoError(t, yaml.Unmarshal(data, &p))
	pol := p.Policies["gnmi_fabric"]
	require.Equal(t, 2000, pol.Config.DebounceMs)
	require.Equal(t, "auto", pol.Config.Mode)
	require.Len(t, pol.Scope.Targets, 2)
	require.Equal(t, "10.0.0.11:6030", pol.Scope.Targets[0].Host)
	require.True(t, pol.Scope.Targets[0].TLS.SkipVerify)
	require.Equal(t, "arista_eos", pol.Scope.Targets[0].Profile)
	require.Equal(t, "on_change", pol.Scope.Targets[1].Mode)
}
