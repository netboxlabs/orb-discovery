package policy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ullaakut/nmap/v3"
	"github.com/go-co-op/gocron/v2"
	"github.com/netboxlabs/diode-sdk-go/diode"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
)

// Define a custom type for the context key
type contextKey string

// Define the policy key
const policyKey contextKey = "policy"

// Runner represents the policy runner
type Runner struct {
	scanner   *nmap.Scanner
	scheduler gocron.Scheduler
	ctx       context.Context
	client    diode.Client
	logger    *slog.Logger
}

// Configure configures the policy runner
func (r *Runner) Configure(ctx context.Context, logger *slog.Logger, name string, policy config.Policy, client diode.Client) error {
	s, err := gocron.NewScheduler()
	if err != nil {
		return err
	}
	r.scheduler = s

	task := gocron.NewTask(r.run)
	if policy.Config.Schedule != nil {
		_, err = r.scheduler.NewJob(gocron.CronJob(*policy.Config.Schedule, false), task, gocron.WithSingletonMode(gocron.LimitModeReschedule))
	} else {
		_, err = r.scheduler.NewJob(gocron.OneTimeJob(
			gocron.OneTimeJobStartDateTime(time.Now().Add(1*time.Second))), task, gocron.WithSingletonMode(gocron.LimitModeReschedule))
	}
	if err != nil {
		return err
	}
	r.ctx = context.WithValue(ctx, policyKey, name)
	n, err := nmap.NewScanner(
		r.ctx,
		nmap.WithTargets(policy.Scope.Targets...),
		nmap.WithPingScan(),
		nmap.WithNonInteractive(),
	)
	if err != nil {
		return err
	}
	r.scanner = n
	r.client = client
	r.logger = logger
	return nil
}

// run runs the policy
func (r *Runner) run() error {
	result, warnings, err := r.scanner.Run()
	if len(*warnings) > 0 {
		r.logger.Warn("run finished with warnings", slog.String("warnings", fmt.Sprintf("%v", *warnings)))
	}
	if err != nil {
		return err
	}

	entities := make([]diode.Entity, 0, len(result.Hosts))

	for _, host := range result.Hosts {
		ip := &diode.IPAddress{
			Address: diode.String(host.Addresses[0].Addr + "/32"),
		}
		entities = append(entities, ip)
	}

	resp, err := r.client.Ingest(r.ctx, entities)
	if err != nil {
		r.logger.Error("error ingesting entities", slog.Any("error", err), slog.Any("policy", r.ctx.Value(policyKey)))
	} else if resp != nil && resp.Errors != nil {
		r.logger.Error("error ingesting entities", slog.Any("error", resp.Errors), slog.Any("policy", r.ctx.Value(policyKey)))
	} else {
		r.logger.Info("entities ingested successfully", slog.Any("policy", r.ctx.Value(policyKey)))
	}

	return nil
}

// Start starts the policy runner
func (r *Runner) Start() {
	r.scheduler.Start()
}

// Stop stops the policy runner
func (r *Runner) Stop() error {
	return r.scheduler.Shutdown()
}
