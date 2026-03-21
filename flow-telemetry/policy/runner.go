package policy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/netboxlabs/orb-discovery/flow-telemetry/config"
	"github.com/netboxlabs/orb-discovery/flow-telemetry/flow"
	"github.com/netboxlabs/orb-discovery/flow-telemetry/metrics"
	"github.com/netboxlabs/orb-discovery/flow-telemetry/rollup"
)

// Runner manages the lifecycle of a single flow-telemetry policy:
// it owns the UDP listener, the rollup window, and the OTLP gauge registrations.
type Runner struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
	name   string
	policy config.Policy
	window *rollup.Window
	reg    metric.Registration // kept alive to prevent GC

	mu        sync.RWMutex
	lastErr   error
	lastErrAt time.Time
}

// NewRunner creates a Runner (does not start it).
func NewRunner(ctx context.Context, logger *slog.Logger, name string, policy config.Policy) *Runner {
	ctx, cancel := context.WithCancel(ctx)
	return &Runner{
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
		name:   name,
		policy: policy,
	}
}

// SetError records a runtime error on the runner.
func (r *Runner) SetError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = err
	r.lastErrAt = time.Now()
}

// ClearError clears any previously recorded runtime error.
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

// Start begins flow ingestion and registers OTLP observable gauges.
func (r *Runner) Start() error {
	// Build rollup configs from policy
	rollupCfgs := make([]rollup.Config, len(r.policy.Config.Rollups))
	for i, rl := range r.policy.Config.Rollups {
		rollupCfgs[i] = rollup.Config{
			Method:     rl.Method,
			Name:       rl.Name,
			Metrics:    rl.Metrics,
			Dimensions: rl.Dimensions,
		}
	}
	r.window = rollup.NewWindow(rollupCfgs)

	// Register OTLP observable gauges (one per rollup, one shared callback).
	if m := metrics.GetMeter(); m != nil {
		if err := r.registerGauges(m); err != nil {
			return err
		}
	}

	// Start the goflow2 UDP listener.
	flowCh, errCh, err := flow.NewListener(r.ctx, r.logger, r.policy.Config, r.policy.Scope.Host, r.policy.Scope.Port)
	if err != nil {
		if r.reg != nil {
			_ = r.reg.Unregister()
		}
		return fmt.Errorf("starting flow listener: %w", err)
	}

	// Consume decoded flow records and add them to the rollup window.
	go func() {
		for rec := range flowCh {
			r.window.Add(rec)
		}
	}()

	// Monitor the listener error channel.
	go func() {
		for err := range errCh {
			r.SetError(fmt.Errorf("flow listener error: %w", err))
		}
		// errCh closed without context cancellation = listener stopped unexpectedly
		if r.ctx.Err() == nil {
			r.SetError(fmt.Errorf("flow listener stopped unexpectedly"))
		}
	}()

	r.logger.Info("policy runner started", "policy", r.name, "port", r.policy.Scope.Port)
	return nil
}

// Stop cancels the flow listener and deregisters OTLP gauges.
func (r *Runner) Stop() error {
	r.cancel()
	if r.reg != nil {
		if err := r.reg.Unregister(); err != nil {
			r.logger.Warn("error unregistering OTLP callback", "policy", r.name, "error", err)
		}
	}
	r.logger.Info("policy runner stopped", "policy", r.name)
	return nil
}

// registerGauges creates one Int64ObservableGauge per rollup and registers a single
// shared callback that snapshots the window (and resets it) on each OTLP export cycle.
func (r *Runner) registerGauges(m metric.Meter) error {
	gauges := make([]metric.Int64ObservableGauge, len(r.policy.Config.Rollups))
	observables := make([]metric.Observable, len(r.policy.Config.Rollups))

	for i, rl := range r.policy.Config.Rollups {
		metricName := "flow." + rl.Name
		g, err := m.Int64ObservableGauge(metricName,
			metric.WithDescription("flow rollup: "+rl.Name+" ("+rl.Method+" of "+fmt.Sprintf("%v", rl.Metrics)+")"),
		)
		if err != nil {
			return fmt.Errorf("creating gauge for %s: %w", metricName, err)
		}
		gauges[i] = g
		observables[i] = g
	}

	// Capture locals for the closure.
	window := r.window
	scopeID := r.policy.Scope.ID
	rollupCfgs := r.policy.Config.Rollups

	reg, err := m.RegisterCallback(func(_ context.Context, obs metric.Observer) error {
		snapshot := window.Snapshot()
		for i, rl := range rollupCfgs {
			for _, pt := range snapshot[rl.Name] {
				attrs := []attribute.KeyValue{}
				if scopeID != "" {
					attrs = append(attrs, attribute.String("netbox_id", scopeID))
				}
				for k, v := range pt.Attrs {
					attrs = append(attrs, attribute.String(k, v))
				}
				obs.ObserveInt64(gauges[i], pt.Value, metric.WithAttributes(attrs...))
			}
		}
		return nil
	}, observables...)
	if err != nil {
		return fmt.Errorf("registering OTLP callback: %w", err)
	}

	r.reg = reg
	return nil
}
