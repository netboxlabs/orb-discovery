package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/gnmi"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/policy"
	"github.com/stretchr/testify/require"
)

func TestStatusRoute(t *testing.T) {
	logger := slog.Default()
	var client diode.Client
	mgr, err := policy.NewManager(context.Background(), logger, client,
		&gnmi.FakeDialer{Session: &gnmi.FakeSession{}}, "")
	require.NoError(t, err)
	s := NewServer("127.0.0.1", 0, logger, mgr, "test")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

// TestStatusConcurrent fires many concurrent GET /api/v1/status requests to
// verify there is no data race on s.stat (caught by -race).
func TestStatusConcurrent(t *testing.T) {
	logger := slog.Default()
	var client diode.Client
	mgr, err := policy.NewManager(context.Background(), logger, client,
		&gnmi.FakeDialer{Session: &gnmi.FakeSession{}}, "")
	require.NoError(t, err)
	s := NewServer("127.0.0.1", 0, logger, mgr, "test")

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
			w := httptest.NewRecorder()
			s.Router().ServeHTTP(w, req)
			// Each goroutine must receive a valid 200 response.
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
		}()
	}
	wg.Wait()
}
