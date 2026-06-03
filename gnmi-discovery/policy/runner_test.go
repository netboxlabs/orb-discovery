package policy

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/netboxlabs/diode-sdk-go/diode"
	diodepb "github.com/netboxlabs/diode-sdk-go/diode/v1/diodepb"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/gnmi"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/mapping"
	"github.com/stretchr/testify/require"
)

// recordingClient captures Ingest calls.
type recordingClient struct {
	mu         sync.Mutex
	ingested   [][]diode.Entity
	lastOptN   int      // number of IngestOptions on the most recent call
	respErrors []string // if set, returned as IngestResponse.Errors (Go err stays nil)
}

func (c *recordingClient) Ingest(_ context.Context, entities []diode.Entity, opts ...diode.IngestOption) (*diodepb.IngestResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ingested = append(c.ingested, entities)
	c.lastOptN = len(opts) // diode.IngestOption is opaque; we can only assert one was passed
	return &diodepb.IngestResponse{Errors: c.respErrors}, nil
}
func (c *recordingClient) IngestProto(context.Context, []*diodepb.Entity, ...diode.IngestOption) (*diodepb.IngestResponse, error) {
	return &diodepb.IngestResponse{}, nil
}
func (c *recordingClient) Close() error { return nil }
func (c *recordingClient) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ingested)
}

func TestRunnerOnChangeIngests(t *testing.T) {
	store, err := mapping.LoadProfiles("")
	require.NoError(t, err)

	fake := &gnmi.FakeSession{
		Caps:            &gnmi.CapabilitiesResult{Vendor: "Arista"},
		OnChangeSupport: true,
		OnChangeStream: []gnmi.Notification{
			{Updates: []gnmi.Update{{Path: "/system/state/hostname", Value: "r1"}}},
			{SyncDone: true},
		},
	}
	client := &recordingClient{}
	pol := config.Policy{
		Config: config.PolicyConfig{Mode: config.ModeOnChange, DebounceMs: 30, Defaults: config.Defaults{Site: "lab", Role: "router"}},
		Scope:  config.Scope{Targets: []config.Target{{Host: "10.0.0.1:6030"}}},
	}
	r, err := NewRunner(context.Background(), slog.Default(), "p1", pol, client,
		&gnmi.FakeDialer{Session: fake}, store)
	require.NoError(t, err)
	r.Start()
	defer r.Stop()

	require.Eventually(t, func() bool { return client.count() >= 1 }, 2*time.Second, 20*time.Millisecond)
}
