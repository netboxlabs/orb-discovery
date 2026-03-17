package policy

import (
	"strings"
	"testing"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func httpPolicy(targets ...string) config.Policy {
	ts := make([]config.Target, len(targets))
	for i, h := range targets {
		ts[i] = config.Target{Host: h}
	}
	return config.Policy{
		Probes: []config.ProbeConfig{
			{
				Name:     "web",
				Type:     "http",
				Targets:  ts,
				Interval: "10s",
				Timeout:  "5s",
				HTTP:     &config.HTTPConf{Scheme: "HTTPS", Path: "/health", Port: 443},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// sanitize
// ---------------------------------------------------------------------------

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"hello.world", "hello_world"},
		{"192.168.1.1", "192_168_1_1"},
		{"host-name", "host_name"},
		{"abc/def", "abc_def"},
		{"already_fine_123", "already_fine_123"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, sanitize(c.in), "sanitize(%q)", c.in)
	}
}

// ---------------------------------------------------------------------------
// Probe stanza generation
// ---------------------------------------------------------------------------

func TestBuildProbeNames(t *testing.T) {
	pol := httpPolicy("example.com")
	out := BuildCloudproberTextproto("my-policy", pol, "", 10)
	assert.Contains(t, out, `name: "my_policy_web_example_com"`)
}

func TestBuildOneProbePerTarget(t *testing.T) {
	pol := httpPolicy("host1.com", "host2.com")
	out := BuildCloudproberTextproto("p", pol, "", 10)
	// Count outer probe stanzas only (not type-specific "http_probe {" etc.)
	assert.Equal(t, 2, strings.Count(out, "probe {\n  name:"), "expected one stanza per target")
	assert.Contains(t, out, `host_names: "host1.com"`)
	assert.Contains(t, out, `host_names: "host2.com"`)
}

func TestBuildHTTPProbeConf(t *testing.T) {
	pol := httpPolicy("example.com")
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "http_probe {")
	assert.Contains(t, out, "scheme: HTTPS")
	assert.Contains(t, out, `relative_url: "/health"`)
	assert.Contains(t, out, "port: 443")
}

func TestBuildHTTPProbeConfMethod(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "api",
			Type:    "http",
			Targets: []config.Target{{Host: "api.example.com"}},
			HTTP:    &config.HTTPConf{Method: "post", Headers: map[string]string{"X-Token": "abc"}},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "method: POST")
	assert.Contains(t, out, `header { name: "X-Token" value: "abc" }`)
}

func TestBuildPingProbeConf(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "ping-test",
			Type:    "ping",
			Targets: []config.Target{{Host: "8.8.8.8"}},
			Ping:    &config.PingConf{PacketsPerProbe: 5, PacketsIntervalMsec: 100, PayloadSize: 56},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "ping_probe {")
	assert.Contains(t, out, "packets_per_probe: 5")
	assert.Contains(t, out, "packets_interval_msec: 100")
	assert.Contains(t, out, "payload_size: 56")
}

func TestBuildDNSProbeConf(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "dns-test",
			Type:    "dns",
			Targets: []config.Target{{Host: "8.8.8.8"}},
			DNS:     &config.DNSConf{Domain: "example.com", QueryType: "a", MinAnswers: 1},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "dns_probe {")
	assert.Contains(t, out, `resolved_domain: "example.com"`)
	assert.Contains(t, out, "query_type: A")
	assert.Contains(t, out, "min_answers: 1")
}

func TestBuildTCPProbeConf(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "tcp-test",
			Type:    "tcp",
			Targets: []config.Target{{Host: "db.example.com"}},
			TCP:     &config.TCPConf{Port: 5432, TLSHandshake: true},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "tcp_probe {")
	assert.Contains(t, out, "port: 5432")
	assert.Contains(t, out, "tls_handshake: true")
}

func TestBuildIntervalTimeout(t *testing.T) {
	pol := httpPolicy("h.com")
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, `interval: "10s"`)
	assert.Contains(t, out, `timeout: "5s"`)
}

func TestBuildDefaultIntervalTimeout(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "x",
			Type:    "http",
			Targets: []config.Target{{Host: "h.com"}},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, `interval: "30s"`)
	assert.Contains(t, out, `timeout: "10s"`)
}

func TestProbeTypeIsUppercased(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{{
			Name:    "x",
			Type:    "http",
			Targets: []config.Target{{Host: "h.com"}},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Contains(t, out, "type: HTTP")
}

// ---------------------------------------------------------------------------
// Surfacer
// ---------------------------------------------------------------------------

func TestFileSurfacerWhenNoEndpoint(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "", 10)
	assert.Contains(t, out, "surfacer { type: FILE }")
	assert.NotContains(t, out, "type: OTEL")
}

func TestOTELSurfacerWhenEndpointProvided(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "grpc://localhost:4317", 10)
	assert.Contains(t, out, "type: OTEL")
	assert.Contains(t, out, `endpoint: "localhost:4317"`)
	assert.Contains(t, out, "insecure: true")
	assert.Contains(t, out, "export_interval_sec: 10")
	assert.NotContains(t, out, "type: FILE")
}

func TestOTELSurfacerStripsGrpcPrefix(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "grpc://otel.svc:4317", 15)
	assert.Contains(t, out, `endpoint: "otel.svc:4317"`)
	assert.NotContains(t, out, "grpc://")
}

func TestMetricsPrefixIsProbe(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "grpc://localhost:4317", 10)
	assert.Contains(t, out, `metrics_prefix: "probe_"`)
}

func TestEphemeralPort(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "", 10)
	assert.Contains(t, out, "port: 0")
}

// ---------------------------------------------------------------------------
// Resource attributes (policy-level defaults → OTLP resource attributes)
// ---------------------------------------------------------------------------

func TestResourceAttributesFromPolicyDefaults(t *testing.T) {
	pol := httpPolicy("h.com")
	pol.Defaults = config.Defaults{Site: "dc1", Role: "web", Location: "rack-a", Tenant: "acme", Tags: []string{"prod"}}
	out := BuildCloudproberTextproto("p", pol, "grpc://localhost:4317", 10)

	require.Contains(t, out, "otel_surfacer {")
	// resource_attribute stanzas live inside the otel_surfacer block
	assert.Contains(t, out, `resource_attribute { key: "site" value: "dc1" }`)
	assert.Contains(t, out, `resource_attribute { key: "role" value: "web" }`)
	assert.Contains(t, out, `resource_attribute { key: "location" value: "rack-a" }`)
	assert.Contains(t, out, `resource_attribute { key: "tenant" value: "acme" }`)
	assert.Contains(t, out, `resource_attribute { key: "tag_prod" value: "true" }`)
}

func TestNoResourceAttributesWhenDefaultsEmpty(t *testing.T) {
	out := BuildCloudproberTextproto("p", httpPolicy("h.com"), "grpc://localhost:4317", 10)
	assert.NotContains(t, out, "resource_attribute")
}

// ---------------------------------------------------------------------------
// Delta labels (target/probe overrides → additional_label, no duplication)
// ---------------------------------------------------------------------------

func TestNoDuplicateLabelsWhenNoOverride(t *testing.T) {
	pol := httpPolicy("h.com")
	pol.Defaults = config.Defaults{Site: "dc1", Role: "web"}
	out := BuildCloudproberTextproto("p", pol, "grpc://localhost:4317", 10)
	// policy defaults are resource_attributes; no per-probe additional_label for same values
	assert.NotContains(t, out, "additional_label")
}

func TestTargetOverrideBecomesAdditionalLabel(t *testing.T) {
	site2 := "dc2"
	pol := config.Policy{
		Defaults: config.Defaults{Site: "dc1", Role: "web"},
		Probes: []config.ProbeConfig{{
			Name: "x",
			Type: "http",
			Targets: []config.Target{
				{Host: "h1.com"},
				{Host: "h2.com", OverrideDefaults: &config.Defaults{Site: site2}},
			},
			HTTP: &config.HTTPConf{},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "grpc://localhost:4317", 10)
	// h2 stanza should have additional_label for site=dc2 (the override)
	assert.Contains(t, out, `additional_label { key: "site" value: "dc2" }`)
	// h1 stanza should have no additional_label (inherits policy defaults already in resource_attribute)
	assert.Equal(t, 1, strings.Count(out, "additional_label"), "only the overriding target should emit additional_label")
}

func TestProbeOverrideBecomesAdditionalLabel(t *testing.T) {
	pol := config.Policy{
		Defaults: config.Defaults{Role: "web"},
		Probes: []config.ProbeConfig{{
			Name:             "infra-ping",
			Type:             "ping",
			Targets:          []config.Target{{Host: "8.8.8.8"}},
			OverrideDefaults: &config.Defaults{Role: "infra"},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "grpc://localhost:4317", 10)
	assert.Contains(t, out, `additional_label { key: "role" value: "infra" }`)
}

// ---------------------------------------------------------------------------
// Multiple probes
// ---------------------------------------------------------------------------

func TestMultipleProbesInPolicy(t *testing.T) {
	pol := config.Policy{
		Probes: []config.ProbeConfig{
			{Name: "a", Type: "http", Targets: []config.Target{{Host: "h1.com"}}, HTTP: &config.HTTPConf{}},
			{Name: "b", Type: "ping", Targets: []config.Target{{Host: "8.8.8.8"}},
				Ping: &config.PingConf{PacketsPerProbe: 3}},
		},
	}
	out := BuildCloudproberTextproto("p", pol, "", 10)
	assert.Equal(t, 2, strings.Count(out, "probe {\n  name:"))
	assert.Contains(t, out, "http_probe {")
	assert.Contains(t, out, "ping_probe {")
}

// ---------------------------------------------------------------------------
// equalTags / deltaDefaults (internal helpers via observable output)
// ---------------------------------------------------------------------------

func TestTagsDeltaWhenTagsOverridden(t *testing.T) {
	pol := config.Policy{
		Defaults: config.Defaults{Tags: []string{"prod"}},
		Probes: []config.ProbeConfig{{
			Name:    "x",
			Type:    "ping",
			Targets: []config.Target{{Host: "8.8.8.8", OverrideDefaults: &config.Defaults{Tags: []string{"staging"}}}},
		}},
	}
	out := BuildCloudproberTextproto("p", pol, "grpc://localhost:4317", 10)
	assert.Contains(t, out, `additional_label { key: "tag_staging" value: "true" }`)
	assert.NotContains(t, out, `additional_label { key: "tag_prod" value: "true" }`)
}
