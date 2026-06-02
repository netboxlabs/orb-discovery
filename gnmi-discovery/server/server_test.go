package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/policy"
	"github.com/stretchr/testify/require"
)

func TestStatusRoute(t *testing.T) {
	logger := slog.Default()
	var client diode.Client
	mgr, err := policy.NewManager(context.Background(), logger, client)
	require.NoError(t, err)
	s := NewServer("127.0.0.1", 0, logger, mgr, "test")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
