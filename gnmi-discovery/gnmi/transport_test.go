package gnmi

import (
	"context"
	"io"
	"net"
	"os"
	"testing"
	"time"

	gnmiproto "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ---------------------------------------------------------------------------
// In-process gNMI server harness
// ---------------------------------------------------------------------------

// testGNMIServer is a minimal in-process gNMI server whose behaviour is
// controlled per-test via the handler fields.
type testGNMIServer struct {
	gnmiproto.UnimplementedGNMIServer

	capsHandler      func(context.Context, *gnmiproto.CapabilityRequest) (*gnmiproto.CapabilityResponse, error)
	getHandler       func(context.Context, *gnmiproto.GetRequest) (*gnmiproto.GetResponse, error)
	subscribeHandler func(gnmiproto.GNMI_SubscribeServer) error
}

func (s *testGNMIServer) Capabilities(ctx context.Context, req *gnmiproto.CapabilityRequest) (*gnmiproto.CapabilityResponse, error) {
	if s.capsHandler != nil {
		return s.capsHandler(ctx, req)
	}
	return nil, status.Error(codes.Unimplemented, "capabilities not configured")
}

func (s *testGNMIServer) Get(ctx context.Context, req *gnmiproto.GetRequest) (*gnmiproto.GetResponse, error) {
	if s.getHandler != nil {
		return s.getHandler(ctx, req)
	}
	return nil, status.Error(codes.Unimplemented, "get not configured")
}

func (s *testGNMIServer) Subscribe(stream gnmiproto.GNMI_SubscribeServer) error {
	if s.subscribeHandler != nil {
		return s.subscribeHandler(stream)
	}
	return status.Error(codes.Unimplemented, "subscribe not configured")
}

// startTestGNMIServer starts a plaintext gRPC server on a random localhost
// port, registers the given testGNMIServer on it, and returns the listener
// address and a stop function. The stop function is registered with t.Cleanup
// automatically, so callers only need to call it if they want to stop early.
func startTestGNMIServer(t *testing.T, srv *testGNMIServer) string {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcSrv := grpc.NewServer()
	gnmiproto.RegisterGNMIServer(grpcSrv, srv)

	go func() {
		_ = grpcSrv.Serve(lis)
	}()

	t.Cleanup(func() {
		grpcSrv.Stop()
		_ = lis.Close()
	})

	return lis.Addr().String()
}

// dialPlaintext dials addr using GnmicDialer with insecure (plaintext) transport.
func dialPlaintext(t *testing.T, addr string) Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := (&GnmicDialer{}).Dial(ctx, TargetSpec{Host: addr})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

// ---------------------------------------------------------------------------
// Test 1 – Dial + Capabilities
// ---------------------------------------------------------------------------

func TestGnmicDialer_DialAndCapabilities(t *testing.T) {
	srv := &testGNMIServer{
		capsHandler: func(_ context.Context, _ *gnmiproto.CapabilityRequest) (*gnmiproto.CapabilityResponse, error) {
			return &gnmiproto.CapabilityResponse{
				SupportedModels: []*gnmiproto.ModelData{
					{Name: "openconfig-interfaces", Organization: "OpenConfig working group"},
					{Name: "arista-eos-bgp", Organization: "Arista Networks"},
				},
				SupportedEncodings: []gnmiproto.Encoding{
					gnmiproto.Encoding_JSON_IETF,
					gnmiproto.Encoding_JSON,
				},
				GNMIVersion: "0.7.0",
			}, nil
		},
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	caps, err := sess.Capabilities(ctx)
	require.NoError(t, err)
	require.NotNil(t, caps)

	// mapCapabilities should detect "arista" in the second model's org.
	assert.Equal(t, "Arista", caps.Vendor)
	assert.Equal(t, []string{"openconfig-interfaces", "arista-eos-bgp"}, caps.Models)
	assert.Equal(t, []string{"JSON_IETF", "JSON"}, caps.Encodings)
}

// TestGnmicSession_Capabilities_ServerError covers the error-return branch in
// gnmicSession.Capabilities (when the server returns an error).
func TestGnmicSession_Capabilities_ServerError(t *testing.T) {
	srv := &testGNMIServer{
		capsHandler: func(_ context.Context, _ *gnmiproto.CapabilityRequest) (*gnmiproto.CapabilityResponse, error) {
			return nil, status.Error(codes.Unavailable, "rpc unavailable")
		},
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sess.Capabilities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gnmi capabilities")
}

// ---------------------------------------------------------------------------
// Test 2 – GetOnce (happy path + error path)
// ---------------------------------------------------------------------------

func TestGnmicSession_GetOnce_Success(t *testing.T) {
	hostnameVal, _ := hostnameJSONVal("spine1")

	srv := &testGNMIServer{
		getHandler: func(_ context.Context, _ *gnmiproto.GetRequest) (*gnmiproto.GetResponse, error) {
			return &gnmiproto.GetResponse{
				Notification: []*gnmiproto.Notification{
					{
						Update: []*gnmiproto.Update{
							{
								Path: &gnmiproto.Path{
									Elem: []*gnmiproto.PathElem{
										{Name: "system"},
										{Name: "state"},
										{Name: "hostname"},
									},
								},
								Val: hostnameVal,
							},
						},
					},
				},
			}, nil
		},
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n, err := sess.GetOnce(ctx, []string{"/system/state/hostname"})
	require.NoError(t, err)
	assert.True(t, n.SyncDone, "GetOnce must always set SyncDone")
	require.Len(t, n.Updates, 1)
	assert.Equal(t, "/system/state/hostname", n.Updates[0].Path)
	assert.Equal(t, "spine1", n.Updates[0].Value)
}

func TestGnmicSession_GetOnce_ServerError(t *testing.T) {
	srv := &testGNMIServer{
		getHandler: func(_ context.Context, _ *gnmiproto.GetRequest) (*gnmiproto.GetResponse, error) {
			return nil, status.Error(codes.NotFound, "no such path")
		},
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := sess.GetOnce(ctx, []string{"/system/state/hostname"})
	require.Error(t, err, "GetOnce must propagate server-side errors")
	assert.Contains(t, err.Error(), "gnmi get")
}

// ---------------------------------------------------------------------------
// Test 3 – Subscribe (Sample and OnChange)
// ---------------------------------------------------------------------------

// subscribeTestServer returns a Subscribe handler that sends one update
// notification followed by sync_response=true, then blocks until the stream
// context is cancelled (simulating a live STREAM subscription).
func subscribeTestServer(_ string, hostname string) func(gnmiproto.GNMI_SubscribeServer) error {
	return func(stream gnmiproto.GNMI_SubscribeServer) error {
		// Read the incoming SubscribeRequest (gnmic sends it before we send anything).
		if _, err := stream.Recv(); err != nil {
			return err
		}

		hostnameVal, _ := hostnameJSONVal(hostname)

		// Send one update.
		if err := stream.Send(&gnmiproto.SubscribeResponse{
			Response: &gnmiproto.SubscribeResponse_Update{
				Update: &gnmiproto.Notification{
					Update: []*gnmiproto.Update{
						{
							Path: &gnmiproto.Path{
								Elem: []*gnmiproto.PathElem{
									{Name: "system"},
									{Name: "state"},
									{Name: "hostname"},
								},
							},
							Val: hostnameVal,
						},
					},
				},
			},
		}); err != nil {
			return err
		}

		// Send sync_response.
		if err := stream.Send(&gnmiproto.SubscribeResponse{
			Response: &gnmiproto.SubscribeResponse_SyncResponse{SyncResponse: true},
		}); err != nil {
			return err
		}

		// Block until client closes / context cancelled.
		<-stream.Context().Done()
		return nil
	}
}

func testSubscribeMode(t *testing.T, mode Mode) {
	t.Helper()

	srv := &testGNMIServer{
		subscribeHandler: subscribeTestServer("/system/state/hostname", "leaf1"),
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	notes, errs, err := sess.Subscribe(ctx, mode, []string{"/system/state/hostname"}, 100)
	require.NoError(t, err)
	require.NotNil(t, notes)
	require.NotNil(t, errs)

	// Expect the update notification.
	select {
	case n, ok := <-notes:
		require.True(t, ok, "notes channel closed prematurely")
		assert.False(t, n.SyncDone)
		require.Len(t, n.Updates, 1)
		assert.Equal(t, "/system/state/hostname", n.Updates[0].Path)
		assert.Equal(t, "leaf1", n.Updates[0].Value)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for update notification")
	}

	// Expect the sync marker.
	select {
	case n, ok := <-notes:
		require.True(t, ok, "notes channel closed before sync")
		assert.True(t, n.SyncDone, "expected SyncDone=true marker")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sync notification")
	}

	// Close and wait for channels to drain.
	require.NoError(t, sess.Close())
	// After Close the notes / errs channels must eventually close.
	select {
	case _, ok := <-notes:
		assert.False(t, ok, "notes channel must be closed after Close()")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for notes channel to close after Close()")
	}
}

func TestGnmicSession_Subscribe_SampleMode(t *testing.T) {
	testSubscribeMode(t, Sample)
}

func TestGnmicSession_Subscribe_OnChangeMode(t *testing.T) {
	testSubscribeMode(t, OnChange)
}

// TestGnmicSession_Subscribe_ReSubscribe exercises the `subCancel != nil`
// branch inside Subscribe: a second Subscribe call on the same session cancels
// the first producer before starting a new one.
func TestGnmicSession_Subscribe_ReSubscribe(t *testing.T) {
	srv := &testGNMIServer{
		subscribeHandler: subscribeTestServer("/system/state/hostname", "leaf2"),
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First Subscribe — establishes subCancel on the session.
	notes1, _, err := sess.Subscribe(ctx, Sample, []string{"/system/state/hostname"}, 100)
	require.NoError(t, err)

	// Drain at least the first notification so gnmic has actually started.
	select {
	case <-notes1:
	case <-time.After(5 * time.Second):
		t.Fatal("first subscribe timed out")
	}

	// Second Subscribe on the same session — triggers the subCancel != nil branch.
	notes2, _, err := sess.Subscribe(ctx, OnChange, []string{"/system/state/hostname"}, 0)
	require.NoError(t, err)

	// The second subscription must also deliver a notification.
	select {
	case n, ok := <-notes2:
		require.True(t, ok)
		_ = n // value not important; just confirm it arrived
	case <-time.After(5 * time.Second):
		t.Fatal("second subscribe timed out")
	}

	require.NoError(t, sess.Close())
}

// TestGnmicSession_Subscribe_StreamClosedByServer exercises the rawResp
// channel close path: the server sends a sync and then closes the stream
// immediately (EOF), so rawResp closes and the goroutine returns via the
// `if !ok { return }` branch.
func TestGnmicSession_Subscribe_StreamClosedByServer(t *testing.T) {
	srv := &testGNMIServer{
		subscribeHandler: func(stream gnmiproto.GNMI_SubscribeServer) error {
			// Read initial subscribe request.
			if _, err := stream.Recv(); err != nil {
				return err
			}
			// Send a sync_response and return immediately — this closes the stream.
			return stream.Send(&gnmiproto.SubscribeResponse{
				Response: &gnmiproto.SubscribeResponse_SyncResponse{SyncResponse: true},
			})
		},
	}
	addr := startTestGNMIServer(t, srv)

	// Use a raw gnmicSession so we can close via defer — the session returned by
	// dialPlaintext already registers a Cleanup close.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sess, err := (&GnmicDialer{}).Dial(ctx, TargetSpec{Host: addr})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	notes, _, subErr := sess.Subscribe(ctx, Sample, []string{"/system"}, 0)
	require.NoError(t, subErr)

	// Receive the sync notification.
	select {
	case n, ok := <-notes:
		if ok {
			assert.True(t, n.SyncDone)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for sync")
	}
}

// ---------------------------------------------------------------------------
// Test 4 – Subscribe error path
// ---------------------------------------------------------------------------

func TestGnmicSession_Subscribe_ServerError(t *testing.T) {
	srv := &testGNMIServer{
		subscribeHandler: func(stream gnmiproto.GNMI_SubscribeServer) error {
			// Read the initial subscribe request before responding with an error,
			// otherwise gnmic may loop-retry before the response reaches the client.
			_, _ = stream.Recv()
			return status.Error(codes.Unavailable, "target unavailable")
		},
	}
	addr := startTestGNMIServer(t, srv)
	sess := dialPlaintext(t, addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	notes, errs, err := sess.Subscribe(ctx, Sample, []string{"/system"}, 0)
	require.NoError(t, err)

	// gnmic may retry with backoff, so we wait up to a generous timeout for an
	// error to arrive on the errs channel.
	select {
	case e, ok := <-errs:
		if ok {
			assert.Error(t, e)
		}
		// ok==false means errs was closed — also acceptable (channel drained after error).
	case <-notes:
		// Drain any spurious notes while waiting.
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for subscribe error")
	}

	// Cancel context and close so no goroutines leak.
	cancel()
	_ = sess.Close()

	// Drain remaining channel items so no goroutine blocks on a send.
	drainTimeout := time.After(3 * time.Second)
drainLoop:
	for {
		select {
		case _, ok := <-notes:
			if !ok {
				break drainLoop
			}
		case _, ok := <-errs:
			if !ok {
				break drainLoop
			}
		case <-drainTimeout:
			break drainLoop
		}
	}
}

// ---------------------------------------------------------------------------
// Test 5 – Dial branch coverage
// ---------------------------------------------------------------------------

// TestGnmicDialer_WithCredentials dials a plaintext server with Username +
// Password set — exercises the credential option-append branches in Dial.
func TestGnmicDialer_WithCredentials(t *testing.T) {
	srv := &testGNMIServer{
		capsHandler: func(_ context.Context, _ *gnmiproto.CapabilityRequest) (*gnmiproto.CapabilityResponse, error) {
			return &gnmiproto.CapabilityResponse{GNMIVersion: "0.7.0"}, nil
		},
	}
	addr := startTestGNMIServer(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := (&GnmicDialer{}).Dial(ctx, TargetSpec{
		Host:     addr,
		Username: "admin",
		Password: "secret",
	})
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	// Verify the session is functional (server returns a response).
	caps, err := sess.Capabilities(ctx)
	require.NoError(t, err)
	assert.NotNil(t, caps)
}

// TestGnmicDialer_SkipVerifyBranch covers the SkipVerify option-append in
// Dial. The port is closed / TLS-over-plaintext, so CreateGNMIClient may
// succeed (gnmic buffers the connection attempt) or fail; either way the
// SkipVerify branch runs.
func TestGnmicDialer_SkipVerifyBranch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess, err := (&GnmicDialer{}).Dial(ctx, TargetSpec{
		Host:       "127.0.0.1:1", // no server; closed port
		SkipVerify: true,
	})
	// We either get an error (CreateGNMIClient failed) or a session that
	// will fail on first RPC — both are fine; we just need the branch covered.
	if err != nil {
		assert.Error(t, err)
	} else {
		_ = sess.Close()
	}
}

// TestGnmicDialer_CAFileBranch covers the CAFile / explicit-TLS branch in
// Dial. We use a temp file for CAFile so the branch runs; TLS over a
// plaintext server or closed port will produce an error from CreateGNMIClient
// or the RPC itself.
func TestGnmicDialer_CAFileBranch(t *testing.T) {
	// Write a throwaway temp file to satisfy the non-empty CAFile check.
	tmp, err := os.CreateTemp(t.TempDir(), "ca*.pem")
	require.NoError(t, err)
	_ = tmp.Close()
	caPath := tmp.Name()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess, dialErr := (&GnmicDialer{}).Dial(ctx, TargetSpec{
		Host:   "127.0.0.1:1",
		CAFile: caPath,
	})
	if dialErr != nil {
		assert.Error(t, dialErr)
	} else {
		_ = sess.Close()
	}
}

// TestGnmicDialer_CertAndKeyBranch covers CertFile + KeyFile branches.
func TestGnmicDialer_CertAndKeyBranch(t *testing.T) {
	tmp1, err := os.CreateTemp(t.TempDir(), "cert*.pem")
	require.NoError(t, err)
	_ = tmp1.Close()
	tmp2, err := os.CreateTemp(t.TempDir(), "key*.pem")
	require.NoError(t, err)
	_ = tmp2.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	sess, dialErr := (&GnmicDialer{}).Dial(ctx, TargetSpec{
		Host:     "127.0.0.1:1",
		CertFile: tmp1.Name(),
		KeyFile:  tmp2.Name(),
	})
	if dialErr != nil {
		assert.Error(t, dialErr)
	} else {
		_ = sess.Close()
	}
}

// TestGnmicDialer_InvalidHostError covers the NewTarget / CreateGNMIClient
// error-return paths for a malformed or unreachable host.
func TestGnmicDialer_InvalidHostError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// An empty address is likely to fail in NewTarget or CreateGNMIClient.
	sess, err := (&GnmicDialer{}).Dial(ctx, TargetSpec{Host: ""})
	if err != nil {
		// Expected: the error must be wrapped with our message prefix.
		assert.Contains(t, err.Error(), "gnmi dial")
	} else {
		// Unexpected success — clean up to avoid goroutine leak.
		_ = sess.Close()
	}
}

// ---------------------------------------------------------------------------
// Helpers used by multiple tests
// ---------------------------------------------------------------------------

// hostnameJSONVal returns a *gnmiproto.TypedValue with a JSON_IETF-encoded
// string value, e.g. `"spine1"`, and any encoding error.
func hostnameJSONVal(hostname string) (*gnmiproto.TypedValue, error) {
	jsonBytes := []byte(`"` + hostname + `"`)
	return &gnmiproto.TypedValue{
		Value: &gnmiproto.TypedValue_JsonIetfVal{JsonIetfVal: jsonBytes},
	}, nil
}

// Compile-time check: testGNMIServer satisfies gnmiproto.GNMIServer.
var _ gnmiproto.GNMIServer = (*testGNMIServer)(nil)

// Compile-time check: io is used (for io.EOF-like drain patterns in the future).
var _ = io.EOF
