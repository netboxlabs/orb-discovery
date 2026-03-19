package server_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/policy"
	"github.com/netboxlabs/orb-discovery/flow-telemetry/server"
)

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	ctx := context.Background()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	manager := policy.NewManager(ctx, logger)
	return server.NewServer("localhost", 0, logger, manager, "test-version")
}

// validPolicyYAML returns a minimal valid policy that listens on the given UDP port.
func validPolicyYAML(name string, port int) []byte {
	return []byte(fmt.Sprintf(`
policies:
  %s:
    scope:
      port: %d
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr, dst_addr]
`, name, port))
}

// --- Status ---

func TestGetStatus_ReturnsVersion(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test-version")
	assert.Contains(t, w.Body.String(), "up_time_seconds")
}

func TestGetStatus_PoliciesField(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "policies")
}

// --- Capabilities ---

func TestGetCapabilities(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil)
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"flow"`)
}

// --- Policies list ---

func TestGetPolicies_Empty(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/policies", nil)
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `[]`, w.Body.String())
}

// --- Create policy ---

func TestCreatePolicy_WrongContentType(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies",
		bytes.NewBufferString("anything"))
	req.Header.Set("Content-Type", "application/json")
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "application/x-yaml")
}

func TestCreatePolicy_MissingContentType(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies",
		bytes.NewBufferString("policies: {}"))
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreatePolicy_InvalidYAML(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies",
		bytes.NewBufferString(": invalid: yaml:::"))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreatePolicy_EmptyPolicies(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies",
		bytes.NewBufferString("policies: {}"))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "no policies found")
}

func TestCreatePolicy_MissingPort(t *testing.T) {
	srv := newTestServer(t)

	body := []byte(`
policies:
  test:
    config:
      rollups:
        - method: sum
          name: flow_bytes
          metrics: [bytes]
          dimensions: [src_addr]
`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "port")
}

func TestCreatePolicy_InvalidRollupMethod(t *testing.T) {
	srv := newTestServer(t)

	body := []byte(`
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
`)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unsupported method")
}

func TestCreatePolicy_Valid(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Stop)

	body := validPolicyYAML("test-policy", 49900)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "test-policy")
	assert.Contains(t, w.Body.String(), "were started")

	// Verify the policy is listed.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/policies", nil)
	srv.Router().ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "test-policy")
	assert.Contains(t, w2.Body.String(), "running")
}

func TestCreatePolicy_Conflict(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Stop)

	body := validPolicyYAML("conflict-policy", 49901)

	// First submission succeeds.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Second submission with the same name → conflict.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)
	assert.Contains(t, w2.Body.String(), "already exists")
}

// --- Delete policy ---

func TestDeletePolicy_NotFound(t *testing.T) {
	srv := newTestServer(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/nonexistent", nil)
	srv.Router().ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "policy not found")
}

func TestDeletePolicy_Valid(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Stop)

	// Create the policy first.
	body := validPolicyYAML("del-policy", 49902)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Delete it.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/del-policy", nil)
	srv.Router().ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "was deleted")

	// Verify it is gone.
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/policies", nil)
	srv.Router().ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
	assert.JSONEq(t, `[]`, w3.Body.String())
}

func TestDeletePolicy_AfterDeletion_NotFound(t *testing.T) {
	srv := newTestServer(t)
	t.Cleanup(srv.Stop)

	body := validPolicyYAML("twice-del", 49903)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	srv.Router().ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// First delete.
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/twice-del", nil)
	srv.Router().ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	// Second delete → not found.
	w3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodDelete, "/api/v1/policies/twice-del", nil)
	srv.Router().ServeHTTP(w3, req3)
	assert.Equal(t, http.StatusNotFound, w3.Code)
}
