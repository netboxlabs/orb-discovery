package policy

import (
	"context"
	"log/slog"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"github.com/netboxlabs/orb-discovery/gnmi-discovery/config"
)

// Runner owns the subscriptions for one policy. Lifecycle added in M6.
type Runner struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
	name   string
	policy config.Policy
	client diode.Client
}

// NewRunner creates a runner for a policy.
func NewRunner(ctx context.Context, logger *slog.Logger, name string, policy config.Policy, client diode.Client) (*Runner, error) {
	rctx, cancel := context.WithCancel(ctx)
	return &Runner{ctx: rctx, cancel: cancel, logger: logger, name: name, policy: policy, client: client}, nil
}

// Start begins the runner (no-op until M6).
func (r *Runner) Start() {}

// Stop cancels the runner.
func (r *Runner) Stop() error {
	r.cancel()
	return nil
}

// State returns the policy state string.
func (r *Runner) State() string { return "running" }
