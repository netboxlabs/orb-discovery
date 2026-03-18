package policy

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
)

var nonAlphanumRe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

// sanitize converts an arbitrary string into a valid cloudprober probe name
// component by replacing non-alphanumeric characters with underscores.
func sanitize(s string) string {
	return nonAlphanumRe.ReplaceAllString(s, "_")
}

// BuildCloudproberTextproto generates a cloudprober textproto configuration
// string from a Policy. One cloudprober probe stanza is emitted per target
// per ProbeConfig so that each target can carry its own merged label set.
//
// Label merging priority (most specific wins):
//
//	target.OverrideDefaults > probe.OverrideDefaults > policy.Defaults
func BuildCloudproberTextproto(
	policyName string,
	policy config.Policy,
	otelEndpoint string,
	exportPeriodSec int,
) string {
	var sb strings.Builder

	for _, probe := range policy.Probes {
		interval := probe.Interval
		if interval == "" {
			interval = "30s"
		}
		timeout := probe.Timeout
		if timeout == "" {
			timeout = "10s"
		}

		probeType := strings.ToUpper(probe.Type)

		for _, target := range probe.Targets {
			probeName := fmt.Sprintf("%s_%s_%s",
				sanitize(policyName),
				sanitize(probe.Name),
				sanitize(target.Host),
			)

			sb.WriteString("probe {\n")
			fmt.Fprintf(&sb, "  name: %q\n", probeName)
			fmt.Fprintf(&sb, "  type: %s\n", probeType)
			fmt.Fprintf(&sb, "  targets { host_names: %q }\n", target.Host)
			fmt.Fprintf(&sb, "  interval: %q\n", interval)
			fmt.Fprintf(&sb, "  timeout: %q\n", timeout)

			// Type-specific probe configuration
			switch probe.Type {
			case "http":
				sb.WriteString(buildHTTPProbeConf(probe.HTTP))
			case "ping":
				sb.WriteString(buildPingProbeConf(probe.Ping))
			case "dns":
				sb.WriteString(buildDNSProbeConf(probe.DNS))
			case "tcp":
				sb.WriteString(buildTCPProbeConf(probe.TCP))
			}

			if target.ID != "" {
				fmt.Fprintf(&sb, "  additional_label { key: \"id\" value: %q }\n", target.ID)
			}

			sb.WriteString("}\n\n")
		}
	}

	// Always provide an explicit surfacer so cloudprober never falls back to
	// the default Prometheus surfacer (which requires a pre-initialised HTTP
	// server that we do not set up).
	if otelEndpoint != "" {
		// Strip grpc:// prefix if present — cloudprober wants host:port only
		endpoint := strings.TrimPrefix(otelEndpoint, "grpc://")

		sb.WriteString("surfacer {\n  type: OTEL\n  otel_surfacer {\n")
		fmt.Fprintf(&sb, "    otlp_grpc_exporter {\n      endpoint: %q\n      insecure: true\n    }\n", endpoint)
		fmt.Fprintf(&sb, "    export_interval_sec: %d\n", exportPeriodSec)
		sb.WriteString("    metrics_prefix: \"probe_\"\n")
		sb.WriteString("  }\n}\n\n")
	} else {
		// No OTLP endpoint — use the FILE surfacer (writes to stdout).
		// This still avoids the default Prometheus surfacer.
		sb.WriteString("surfacer { type: FILE }\n\n")
	}

	// Ephemeral HTTP port — avoids conflicts between multiple Runner instances
	sb.WriteString("port: 0\n")

	return sb.String()
}

func buildHTTPProbeConf(h *config.HTTPConf) string {
	if h == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("  http_probe {\n")
	if h.Scheme != "" {
		fmt.Fprintf(&sb, "    scheme: %s\n", strings.ToUpper(h.Scheme))
	}
	if h.Path != "" {
		fmt.Fprintf(&sb, "    relative_url: %q\n", h.Path)
	}
	if h.Port != 0 {
		fmt.Fprintf(&sb, "    port: %d\n", h.Port)
	}
	if h.Method != "" {
		fmt.Fprintf(&sb, "    method: %s\n", strings.ToUpper(h.Method))
	}
	for k, v := range h.Headers {
		fmt.Fprintf(&sb, "    header { name: %q value: %q }\n", k, v)
	}
	sb.WriteString("  }\n")
	return sb.String()
}

func buildPingProbeConf(p *config.PingConf) string {
	if p == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("  ping_probe {\n")
	if p.PacketsPerProbe > 0 {
		fmt.Fprintf(&sb, "    packets_per_probe: %d\n", p.PacketsPerProbe)
	}
	if p.PacketsIntervalMsec > 0 {
		fmt.Fprintf(&sb, "    packets_interval_msec: %d\n", p.PacketsIntervalMsec)
	}
	if p.PayloadSize > 0 {
		fmt.Fprintf(&sb, "    payload_size: %d\n", p.PayloadSize)
	}
	sb.WriteString("  }\n")
	return sb.String()
}

func buildDNSProbeConf(d *config.DNSConf) string {
	if d == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("  dns_probe {\n")
	if d.Domain != "" {
		fmt.Fprintf(&sb, "    resolved_domain: %q\n", d.Domain)
	}
	if d.QueryType != "" {
		fmt.Fprintf(&sb, "    query_type: %s\n", strings.ToUpper(d.QueryType))
	}
	if d.MinAnswers > 0 {
		fmt.Fprintf(&sb, "    min_answers: %d\n", d.MinAnswers)
	}
	sb.WriteString("  }\n")
	return sb.String()
}

func buildTCPProbeConf(t *config.TCPConf) string {
	if t == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("  tcp_probe {\n")
	if t.Port != 0 {
		fmt.Fprintf(&sb, "    port: %d\n", t.Port)
	}
	if t.TLSHandshake {
		sb.WriteString("    tls_handshake: true\n")
	}
	sb.WriteString("  }\n")
	return sb.String()
}

