package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMergeDefaults(t *testing.T) {
	t.Run("nil override returns policy defaults and result is not aliased to source", func(t *testing.T) {
		policy := &Defaults{
			Site: "NYC",
			Tags: []string{"a", "b"},
			Device: DeviceDefaults{
				Tags: []string{"d1"},
			},
			Interface: InterfaceDefaults{
				Tags: []string{"i1"},
			},
		}
		result := MergeDefaults(policy, nil)
		require.Equal(t, policy.Site, result.Site)
		require.Equal(t, policy.Tags, result.Tags)

		// Mutating result must not affect the original policy defaults.
		result.Tags = append(result.Tags, "injected")
		require.NotContains(t, policy.Tags, "injected", "result.Tags must not alias policy.Tags")

		result.Device.Tags = append(result.Device.Tags, "injected")
		require.NotContains(t, policy.Device.Tags, "injected", "result.Device.Tags must not alias policy.Device.Tags")

		result.Interface.Tags = append(result.Interface.Tags, "injected")
		require.NotContains(t, policy.Interface.Tags, "injected", "result.Interface.Tags must not alias policy.Interface.Tags")
	})

	t.Run("nil policyDefaults with non-nil override does not panic", func(t *testing.T) {
		override := &Defaults{Site: "LA", Role: "Leaf"}
		result := MergeDefaults(nil, override)
		require.NotNil(t, result)
		require.Equal(t, "LA", result.Site)
	})

	t.Run("override sets scalar fields; unset fields fall back to policy", func(t *testing.T) {
		policy := &Defaults{
			Site:     "NYC",
			Role:     "Spine",
			Location: "DC-1",
			Device: DeviceDefaults{
				Manufacturer: "Arista",
				Model:        "7050CX3",
				Platform:     "EOS",
				Comments:     "old comment",
			},
			Interface: InterfaceDefaults{
				Type:        "1000base-t",
				Description: "uplink",
			},
		}
		override := &Defaults{
			Site: "LA",
			Role: "Leaf",
			Device: DeviceDefaults{
				Model:    "7280R3",
				Comments: "new comment",
			},
			Interface: InterfaceDefaults{
				Description: "downlink",
			},
		}
		result := MergeDefaults(policy, override)

		// Override wins.
		require.Equal(t, "LA", result.Site)
		require.Equal(t, "Leaf", result.Role)
		require.Equal(t, "7280R3", result.Device.Model)
		require.Equal(t, "new comment", result.Device.Comments)
		require.Equal(t, "downlink", result.Interface.Description)

		// Policy fallbacks.
		require.Equal(t, "DC-1", result.Location)
		require.Equal(t, "Arista", result.Device.Manufacturer)
		require.Equal(t, "EOS", result.Device.Platform)
		require.Equal(t, "1000base-t", result.Interface.Type)
	})

	t.Run("override sets tags; they win and are not aliased to the override", func(t *testing.T) {
		policy := &Defaults{
			Tags:      []string{"policy-tag"},
			Device:    DeviceDefaults{Tags: []string{"policy-device-tag"}},
			Interface: InterfaceDefaults{Tags: []string{"policy-iface-tag"}},
		}
		override := &Defaults{
			Tags:      []string{"override-tag"},
			Device:    DeviceDefaults{Tags: []string{"override-device-tag"}},
			Interface: InterfaceDefaults{Tags: []string{"override-iface-tag"}},
		}
		result := MergeDefaults(policy, override)

		require.Equal(t, []string{"override-tag"}, result.Tags)
		require.Equal(t, []string{"override-device-tag"}, result.Device.Tags)
		require.Equal(t, []string{"override-iface-tag"}, result.Interface.Tags)

		// Appending to the result must not affect the override source.
		result.Tags = append(result.Tags, "extra")
		require.NotContains(t, override.Tags, "extra", "result.Tags must not alias override.Tags")

		result.Device.Tags = append(result.Device.Tags, "extra")
		require.NotContains(t, override.Device.Tags, "extra", "result.Device.Tags must not alias override.Device.Tags")

		result.Interface.Tags = append(result.Interface.Tags, "extra")
		require.NotContains(t, override.Interface.Tags, "extra", "result.Interface.Tags must not alias override.Interface.Tags")
	})
}

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
