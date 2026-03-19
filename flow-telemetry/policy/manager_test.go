package policy

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTestManager() *Manager {
	return NewManager(context.Background(), slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func TestParsePolicies_Valid(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr, dst_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Contains(t, policies, "test")
	assert.Equal(t, 9995, policies["test"].Scope.Port)
}

func TestParsePolicies_Empty(t *testing.T) {
	m := newTestManager()
	_, err := m.ParsePolicies([]byte("policies: {}"))
	assert.ErrorContains(t, err, "no policies found")
}

func TestParsePolicies_MissingPort(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "port must be a positive integer")
}

func TestParsePolicies_NoRollups(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups: []
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "at least one rollup must be defined")
}

func TestParsePolicies_InvalidMethod(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: average
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "unsupported method")
}

func TestParsePolicies_InvalidMetric(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: flow_bits
          metrics: [bits]
          dimensions: [src_addr]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "unsupported metric")
}

func TestParsePolicies_InvalidDimension(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [hostname]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "unsupported dimension")
}

func TestParsePolicies_AllMethods(t *testing.T) {
	m := newTestManager()
	for _, method := range []string{"sum", "max", "min"} {
		yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: ` + method + `
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
		_, err := m.ParsePolicies([]byte(yaml))
		assert.NoError(t, err, "method %q should be valid", method)
	}
}

func TestParsePolicies_MultiplePolicies(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  netflow:
    scope:
      port: 9995
    config:
      protocol: netflow9
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr, dst_addr, proto]
  sflow:
    scope:
      port: 6343
    config:
      protocol: sflow
      rollups:
        - method: sum
          name: packets
          metrics: [packets]
          dimensions: [sampler_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Len(t, policies, 2)
}

func TestGetCapabilities(t *testing.T) {
	m := newTestManager()
	assert.Equal(t, []string{"flow"}, m.GetCapabilities())
}

func TestGetPolicyStatuses_Empty(t *testing.T) {
	m := newTestManager()
	assert.Empty(t, m.GetPolicyStatuses())
}

func TestHasPolicy_False(t *testing.T) {
	m := newTestManager()
	assert.False(t, m.HasPolicy("nonexistent"))
}

func TestStopPolicy_NotFound_NoError(t *testing.T) {
	m := newTestManager()
	assert.NoError(t, m.StopPolicy("nonexistent"))
}

func TestStop_EmptyManager(t *testing.T) {
	m := newTestManager()
	assert.NoError(t, m.Stop())
}

func TestParsePolicies_MissingRollupName(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          metrics: [bytes]
          dimensions: [src_addr]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "name is required")
}

func TestParsePolicies_EmptyMetrics(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: []
          dimensions: [src_addr]
`
	_, err := m.ParsePolicies([]byte(yaml))
	assert.ErrorContains(t, err, "at least one metric is required")
}

func TestParsePolicies_AllValidDimensions(t *testing.T) {
	m := newTestManager()
	dims := []string{
		"src_addr", "dst_addr", "src_port", "dst_port",
		"proto", "sampler_addr", "in_if", "out_if", "src_as", "dst_as",
	}
	for _, dim := range dims {
		yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [` + dim + `]
`
		_, err := m.ParsePolicies([]byte(yaml))
		assert.NoError(t, err, "dimension %q should be valid", dim)
	}
}

func TestParsePolicies_ProtocolOptions(t *testing.T) {
	m := newTestManager()
	for _, proto := range []string{"auto", "netflow5", "netflow9", "ipfix", "sflow", ""} {
		yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      protocol: ` + proto + `
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
		_, err := m.ParsePolicies([]byte(yaml))
		assert.NoError(t, err, "protocol %q should parse without error", proto)
	}
}

func TestParsePolicies_WorkersAndQueueSize(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      workers: 4
      queue_size: 50000
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Equal(t, 4, policies["test"].Config.Workers)
	assert.Equal(t, 50000, policies["test"].Config.QueueSize)
}

func TestParsePolicies_ScopeHost(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
      host: 192.168.1.1
    config:
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Equal(t, "192.168.1.1", policies["test"].Scope.Host)
}

func TestParsePolicies_ScopeID(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
      id: site-a
    config:
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Equal(t, "site-a", policies["test"].Scope.ID)
}

func TestParsePolicies_ScopeDefaults(t *testing.T) {
	m := newTestManager()
	yaml := `
policies:
  test:
    scope:
      port: 9995
    config:
      rollups:
        - method: sum
          name: bytes
          metrics: [bytes]
          dimensions: [src_addr]
`
	policies, err := m.ParsePolicies([]byte(yaml))
	assert.NoError(t, err)
	assert.Equal(t, "", policies["test"].Scope.Host)
	assert.Equal(t, "", policies["test"].Scope.ID)
}
