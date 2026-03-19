package rollup

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/flow"
)

// ValidMethods, ValidDimensions, and ValidMetrics are used by the manager for config validation.
var (
	ValidMethods = map[string]bool{"sum": true, "max": true, "min": true}

	ValidDimensions = map[string]bool{
		"src_addr":    true,
		"dst_addr":    true,
		"src_port":    true,
		"dst_port":    true,
		"proto":       true,
		"sampler_addr": true,
		"in_if":       true,
		"out_if":      true,
		"src_as":      true,
		"dst_as":      true,
	}

	ValidMetrics = map[string]bool{"bytes": true, "packets": true}
)

// Config defines a single aggregation rule.
type Config struct {
	Method     string
	Name       string
	Metrics    []string
	Dimensions []string
}

// Point is a single aggregated data point produced by a Snapshot.
type Point struct {
	// Attrs holds the dimension key-value pairs for this point's OTLP attributes.
	Attrs map[string]string
	Value int64
}

type bucketEntry struct {
	value uint64
	seen  bool
	attrs map[string]string
}

// Window accumulates flow records and can be snapshotted atomically.
// Snapshot resets the window so each OTLP export period gets a fresh slice.
type Window struct {
	mu      sync.Mutex
	configs []Config
	buckets map[string]map[string]*bucketEntry // rollupName → dimKey → entry
}

// NewWindow creates a Window for the given rollup configs.
func NewWindow(configs []Config) *Window {
	w := &Window{
		configs: configs,
		buckets: make(map[string]map[string]*bucketEntry, len(configs)),
	}
	for _, c := range configs {
		w.buckets[c.Name] = make(map[string]*bucketEntry)
	}
	return w
}

// Add adds a flow record to all rollup accumulators.
func (w *Window) Add(rec flow.FlowRecord) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for i := range w.configs {
		c := &w.configs[i]
		attrs := extractDimensions(rec, c.Dimensions)
		dimKey := buildDimKey(attrs)

		var metricVal uint64
		for _, m := range c.Metrics {
			metricVal += extractMetric(rec, m)
		}

		entry, ok := w.buckets[c.Name][dimKey]
		if !ok {
			entry = &bucketEntry{attrs: attrs}
			w.buckets[c.Name][dimKey] = entry
		}

		switch c.Method {
		case "sum":
			entry.value += metricVal
		case "max":
			if !entry.seen || metricVal > entry.value {
				entry.value = metricVal
			}
		case "min":
			if !entry.seen || metricVal < entry.value {
				entry.value = metricVal
			}
		}
		entry.seen = true
	}
}

// Snapshot returns the current accumulated data for all rollups and resets the window.
// Called once per OTLP export cycle from an observable gauge callback.
func (w *Window) Snapshot() map[string][]Point {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := make(map[string][]Point, len(w.configs))
	for i := range w.configs {
		c := &w.configs[i]
		bucket := w.buckets[c.Name]
		points := make([]Point, 0, len(bucket))
		for _, entry := range bucket {
			if entry.seen {
				points = append(points, Point{Attrs: entry.attrs, Value: int64(entry.value)}) //nolint:gosec
			}
		}
		result[c.Name] = points
		w.buckets[c.Name] = make(map[string]*bucketEntry)
	}
	return result
}

func extractDimensions(rec flow.FlowRecord, dims []string) map[string]string {
	attrs := make(map[string]string, len(dims))
	for _, d := range dims {
		attrs[d] = getField(rec, d)
	}
	return attrs
}

func buildDimKey(attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + attrs[k]
	}
	return strings.Join(parts, ",")
}

func getField(rec flow.FlowRecord, field string) string {
	switch field {
	case "src_addr":
		return rec.SrcAddr
	case "dst_addr":
		return rec.DstAddr
	case "src_port":
		return fmt.Sprintf("%d", rec.SrcPort)
	case "dst_port":
		return fmt.Sprintf("%d", rec.DstPort)
	case "proto":
		return protocolName(rec.Proto)
	case "sampler_addr":
		return rec.SamplerAddr
	case "in_if":
		return fmt.Sprintf("%d", rec.InIf)
	case "out_if":
		return fmt.Sprintf("%d", rec.OutIf)
	case "src_as":
		return fmt.Sprintf("%d", rec.SrcAs)
	case "dst_as":
		return fmt.Sprintf("%d", rec.DstAs)
	default:
		return ""
	}
}

func extractMetric(rec flow.FlowRecord, metric string) uint64 {
	switch metric {
	case "bytes":
		return rec.Bytes
	case "packets":
		return rec.Packets
	default:
		return 0
	}
}

func protocolName(proto uint32) string {
	switch proto {
	case 1:
		return "ICMP"
	case 6:
		return "TCP"
	case 17:
		return "UDP"
	case 47:
		return "GRE"
	case 50:
		return "ESP"
	case 58:
		return "ICMPv6"
	default:
		return fmt.Sprintf("%d", proto)
	}
}
