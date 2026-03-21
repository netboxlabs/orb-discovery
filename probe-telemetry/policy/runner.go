package policy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	configpb "github.com/cloudprober/cloudprober/config/proto"
	"github.com/cloudprober/cloudprober/prober"
	"github.com/cloudprober/cloudprober/state"
	"google.golang.org/protobuf/encoding/prototext"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
)

// newCloudproberMux creates a fresh HTTP mux and installs it as cloudprober's
// default. Each Runner gets its own mux so cloudprober can re-register its
// internal handlers (e.g. /status/static/) without conflicting with a
// previously created Runner.
func newCloudproberMux() {
	state.SetDefaultHTTPServeMux(http.NewServeMux())
}

// Runner wraps a cloudprober Prober for a single policy. Each Runner owns its
// own prober.Prober instance and cancellable context, so policies are fully
// isolated from one another.
type Runner struct {
	prb    *prober.Prober
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger

	mu        sync.RWMutex
	lastErr   error
	lastErrAt time.Time
}

// NewRunner creates and initialises a cloudprober Prober from the supplied
// policy configuration. It does NOT start probing; call Start() for that.
func NewRunner(
	ctx context.Context,
	logger *slog.Logger,
	name string,
	policy config.Policy,
	otelEndpoint string,
	otelExportPeriod int,
) (*Runner, error) {
	newCloudproberMux()

	cfgText := BuildCloudproberTextproto(name, policy, otelEndpoint, otelExportPeriod)
	logger.Debug("cloudprober config", "policy", name, "config", cfgText)

	cfg := &configpb.ProberConfig{}
	if err := prototext.Unmarshal([]byte(cfgText), cfg); err != nil {
		return nil, fmt.Errorf("policy %s: failed to parse cloudprober config: %w", name, err)
	}

	runCtx, cancel := context.WithCancel(ctx)

	prb, err := prober.Init(runCtx, cfg, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("policy %s: failed to init cloudprober prober: %w", name, err)
	}

	return &Runner{
		prb:    prb,
		ctx:    runCtx,
		cancel: cancel,
		logger: logger,
	}, nil
}

// SetError records an error on the runner.
func (r *Runner) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = err
	r.lastErrAt = time.Now()
}

// ClearError clears any previously recorded error.
func (r *Runner) ClearError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = nil
	r.lastErrAt = time.Time{}
}

// GetLastError returns the last recorded error and the time it was set.
func (r *Runner) GetLastError() (error, time.Time) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastErr, r.lastErrAt
}

// Start begins the probe loop in a background goroutine.
func (r *Runner) Start() {
	go func() {
		r.prb.Start(r.ctx)
		// r.ctx.Err() is non-nil only when Stop() → cancel() was called (normal shutdown).
		// If the goroutine exits with ctx still active, the prober crashed unexpectedly.
		if r.ctx.Err() == nil {
			r.SetError(fmt.Errorf("prober exited unexpectedly"))
		}
	}()
}

// Stop cancels the runner context, causing cloudprober to stop all probes.
func (r *Runner) Stop() error {
	r.cancel()
	return nil
}
