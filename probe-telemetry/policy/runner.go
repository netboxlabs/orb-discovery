package policy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

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

// Start begins the probe loop in a background goroutine.
func (r *Runner) Start() {
	go r.prb.Start(r.ctx)
}

// Stop cancels the runner context, causing cloudprober to stop all probes.
func (r *Runner) Stop() error {
	r.cancel()
	return nil
}
