package rollup

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/flow"
)

func TestWindow_Sum(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "bytes_by_src", Metrics: []string{"bytes"}, Dimensions: []string{"src_addr"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 100})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 200})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.2", Bytes: 50})

	snap := w.Snapshot()
	pts := snap["bytes_by_src"]
	assert.Len(t, pts, 2)

	byAddr := make(map[string]int64)
	for _, p := range pts {
		byAddr[p.Attrs["src_addr"]] = p.Value
	}
	assert.Equal(t, int64(300), byAddr["10.0.0.1"])
	assert.Equal(t, int64(50), byAddr["10.0.0.2"])
}

func TestWindow_Max(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "max", Name: "max_bytes", Metrics: []string{"bytes"}, Dimensions: []string{"src_addr"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 500})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 100})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 300})

	snap := w.Snapshot()
	pts := snap["max_bytes"]
	assert.Len(t, pts, 1)
	assert.Equal(t, int64(500), pts[0].Value)
}

func TestWindow_Min(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "min", Name: "min_bytes", Metrics: []string{"bytes"}, Dimensions: []string{"src_addr"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 500})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 100})
	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 300})

	snap := w.Snapshot()
	pts := snap["min_bytes"]
	assert.Len(t, pts, 1)
	assert.Equal(t, int64(100), pts[0].Value)
}

func TestWindow_SnapshotResetsWindow(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "pkts", Metrics: []string{"packets"}, Dimensions: []string{"dst_addr"}},
	})

	w.Add(flow.FlowRecord{DstAddr: "1.2.3.4", Packets: 10})
	snap1 := w.Snapshot()
	assert.Len(t, snap1["pkts"], 1)

	// After snapshot the window is empty.
	snap2 := w.Snapshot()
	assert.Empty(t, snap2["pkts"])
}

func TestWindow_MultipleRollups(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "bytes", Metrics: []string{"bytes"}, Dimensions: []string{"proto"}},
		{Method: "sum", Name: "packets", Metrics: []string{"packets"}, Dimensions: []string{"proto"}},
	})

	w.Add(flow.FlowRecord{Proto: 6, Bytes: 1000, Packets: 5})
	w.Add(flow.FlowRecord{Proto: 6, Bytes: 2000, Packets: 3})

	snap := w.Snapshot()
	assert.Equal(t, int64(3000), snap["bytes"][0].Value)
	assert.Equal(t, int64(8), snap["packets"][0].Value)
}

func TestWindow_DimensionKey_Proto(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "flows", Metrics: []string{"bytes"}, Dimensions: []string{"proto"}},
	})

	w.Add(flow.FlowRecord{Proto: 6, Bytes: 100})  // TCP
	w.Add(flow.FlowRecord{Proto: 17, Bytes: 200}) // UDP

	snap := w.Snapshot()
	assert.Len(t, snap["flows"], 2)

	byProto := make(map[string]int64)
	for _, p := range snap["flows"] {
		byProto[p.Attrs["proto"]] = p.Value
	}
	assert.Equal(t, int64(100), byProto["TCP"])
	assert.Equal(t, int64(200), byProto["UDP"])
}

func TestWindow_NoDimensions(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "total_bytes", Metrics: []string{"bytes"}, Dimensions: []string{}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "1.1.1.1", Bytes: 100})
	w.Add(flow.FlowRecord{SrcAddr: "2.2.2.2", Bytes: 200})

	snap := w.Snapshot()
	pts := snap["total_bytes"]
	assert.Len(t, pts, 1)
	assert.Equal(t, int64(300), pts[0].Value)
}

func TestGetField_AllDimensions(t *testing.T) {
	rec := flow.FlowRecord{
		SamplerAddr: "192.168.1.1",
		SrcAddr:     "10.0.0.1",
		DstAddr:     "10.0.0.2",
		SrcPort:     1234,
		DstPort:     80,
		Proto:       6,
		InIf:        1,
		OutIf:       2,
		SrcAs:       64512,
		DstAs:       64513,
	}

	assert.Equal(t, "192.168.1.1", getField(rec, "sampler_addr"))
	assert.Equal(t, "10.0.0.1", getField(rec, "src_addr"))
	assert.Equal(t, "10.0.0.2", getField(rec, "dst_addr"))
	assert.Equal(t, "1234", getField(rec, "src_port"))
	assert.Equal(t, "80", getField(rec, "dst_port"))
	assert.Equal(t, "TCP", getField(rec, "proto"))
	assert.Equal(t, "1", getField(rec, "in_if"))
	assert.Equal(t, "2", getField(rec, "out_if"))
	assert.Equal(t, "64512", getField(rec, "src_as"))
	assert.Equal(t, "64513", getField(rec, "dst_as"))
	assert.Equal(t, "", getField(rec, "unknown_field"))
}

func TestProtocolName(t *testing.T) {
	cases := []struct {
		proto uint32
		want  string
	}{
		{1, "ICMP"},
		{6, "TCP"},
		{17, "UDP"},
		{47, "GRE"},
		{50, "ESP"},
		{58, "ICMPv6"},
		{0, "0"},
		{255, "255"},
		{89, "89"}, // OSPF — not in the named list
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, protocolName(tc.proto), "proto %d", tc.proto)
	}
}

func TestExtractMetric_Bytes(t *testing.T) {
	rec := flow.FlowRecord{Bytes: 9999}
	assert.Equal(t, uint64(9999), extractMetric(rec, "bytes"))
}

func TestExtractMetric_Packets(t *testing.T) {
	rec := flow.FlowRecord{Packets: 42}
	assert.Equal(t, uint64(42), extractMetric(rec, "packets"))
}

func TestExtractMetric_Unknown(t *testing.T) {
	rec := flow.FlowRecord{Bytes: 100, Packets: 10}
	assert.Equal(t, uint64(0), extractMetric(rec, "bits"))
}

func TestWindow_MultipleMetrics(t *testing.T) {
	// A single rollup can sum bytes+packets together.
	w := NewWindow([]Config{
		{Method: "sum", Name: "combined", Metrics: []string{"bytes", "packets"}, Dimensions: []string{"src_addr"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "1.2.3.4", Bytes: 1000, Packets: 5})
	w.Add(flow.FlowRecord{SrcAddr: "1.2.3.4", Bytes: 500, Packets: 3})

	snap := w.Snapshot()
	pts := snap["combined"]
	assert.Len(t, pts, 1)
	// combined value = (1000+5) + (500+3) = 1508
	assert.Equal(t, int64(1508), pts[0].Value)
}

func TestWindow_Max_SingleEntry(t *testing.T) {
	// Only one record — the "seen" flag must be set correctly.
	w := NewWindow([]Config{
		{Method: "max", Name: "max_pkts", Metrics: []string{"packets"}, Dimensions: []string{"dst_addr"}},
	})

	w.Add(flow.FlowRecord{DstAddr: "5.6.7.8", Packets: 77})

	snap := w.Snapshot()
	assert.Equal(t, int64(77), snap["max_pkts"][0].Value)
}

func TestWindow_Min_SingleEntry(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "min", Name: "min_pkts", Metrics: []string{"packets"}, Dimensions: []string{"dst_addr"}},
	})

	w.Add(flow.FlowRecord{DstAddr: "5.6.7.8", Packets: 33})

	snap := w.Snapshot()
	assert.Equal(t, int64(33), snap["min_pkts"][0].Value)
}

func TestWindow_MultipleDimensions(t *testing.T) {
	// src_addr + dst_addr combination → two separate buckets.
	w := NewWindow([]Config{
		{Method: "sum", Name: "flows", Metrics: []string{"bytes"},
			Dimensions: []string{"src_addr", "dst_addr"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "1.1.1.1", DstAddr: "2.2.2.2", Bytes: 100})
	w.Add(flow.FlowRecord{SrcAddr: "1.1.1.1", DstAddr: "3.3.3.3", Bytes: 200})
	w.Add(flow.FlowRecord{SrcAddr: "1.1.1.1", DstAddr: "2.2.2.2", Bytes: 50})

	snap := w.Snapshot()
	pts := snap["flows"]
	assert.Len(t, pts, 2)

	byKey := make(map[string]int64)
	for _, p := range pts {
		key := p.Attrs["src_addr"] + "->" + p.Attrs["dst_addr"]
		byKey[key] = p.Value
	}
	assert.Equal(t, int64(150), byKey["1.1.1.1->2.2.2.2"])
	assert.Equal(t, int64(200), byKey["1.1.1.1->3.3.3.3"])
}

func TestWindow_EmptyAdd_NoSnapshot(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "bytes", Metrics: []string{"bytes"}, Dimensions: []string{"src_addr"}},
	})

	// No Add calls — Snapshot should return empty slice.
	snap := w.Snapshot()
	assert.Empty(t, snap["bytes"])
}

func TestWindow_SnapshotReturnsDimAttrs(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "flows", Metrics: []string{"bytes"},
			Dimensions: []string{"src_addr", "proto"}},
	})

	w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Proto: 17, Bytes: 100})

	snap := w.Snapshot()
	pts := snap["flows"]
	require.Len(t, pts, 1)
	assert.Equal(t, "10.0.0.1", pts[0].Attrs["src_addr"])
	assert.Equal(t, "UDP", pts[0].Attrs["proto"])
}

func TestBuildDimKey_Deterministic(t *testing.T) {
	// buildDimKey must produce the same string regardless of map iteration order.
	attrs1 := map[string]string{"src_addr": "1.1.1.1", "dst_addr": "2.2.2.2", "proto": "TCP"}
	attrs2 := map[string]string{"proto": "TCP", "dst_addr": "2.2.2.2", "src_addr": "1.1.1.1"}

	assert.Equal(t, buildDimKey(attrs1), buildDimKey(attrs2))
}

func TestWindow_Concurrent(t *testing.T) {
	w := NewWindow([]Config{
		{Method: "sum", Name: "bytes", Metrics: []string{"bytes"}, Dimensions: []string{"src_addr"}},
	})

	const goroutines = 20
	const recordsEach = 50
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < recordsEach; j++ {
				w.Add(flow.FlowRecord{SrcAddr: "10.0.0.1", Bytes: 1})
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}

	snap := w.Snapshot()
	pts := snap["bytes"]
	require.Len(t, pts, 1)
	assert.Equal(t, int64(goroutines*recordsEach), pts[0].Value)
}
