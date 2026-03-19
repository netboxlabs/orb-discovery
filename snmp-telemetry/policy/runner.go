package policy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/collector"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/config"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/profiles"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/snmp"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/targets"
)

// Define a custom type for the context key
type contextKey string

// Define the policy key
const (
	policyKey          contextKey = "policy"
	defaultSNMPTimeout            = 5 * time.Second
	defaultProfilesDir            = "/usr/local/share/snmp-profiles"
)

// Runner represents the policy runner for SNMP metrics collection
type Runner struct {
	scheduler        gocron.Scheduler
	ctx              context.Context
	metricsCollector *collector.MetricsCollector
	metricsInterval  time.Duration
	snmpTimeout      time.Duration
	config           config.PolicyConfig
	scope            config.Scope
	logger           *slog.Logger
}

// NewRunner returns a new policy runner.
// instanceProfilesDir is the instance-level default profiles directory set via CLI flag;
// it overrides the compiled-in constant but is itself overridden by policy.Config.ProfilesDir.
func NewRunner(ctx context.Context, logger *slog.Logger, name string, policy config.Policy, clientFactory snmp.ClientFactory, instanceProfilesDir string) (*Runner, error) {
	s, err := gocron.NewScheduler()
	if err != nil {
		return nil, err
	}

	snmpTimeout := time.Duration(policy.Config.SNMPTimeout) * time.Second
	if snmpTimeout == 0 {
		snmpTimeout = defaultSNMPTimeout
	}

	runner := &Runner{
		scheduler:   s,
		logger:      logger,
		snmpTimeout: snmpTimeout,
		config:      policy.Config,
		scope:       policy.Scope,
		ctx:         context.WithValue(ctx, policyKey, name),
	}

	if policy.Config.MetricsInterval == nil || *policy.Config.MetricsInterval <= 0 {
		return nil, fmt.Errorf("metrics_interval must be a positive integer")
	}
	runner.metricsInterval = time.Duration(*policy.Config.MetricsInterval) * time.Second

	// Priority: per-policy config > CLI flag (instanceProfilesDir) > compiled-in constant
	profilesDir := policy.Config.ProfilesDir
	if profilesDir == "" {
		profilesDir = instanceProfilesDir
	}
	if profilesDir == "" {
		profilesDir = defaultProfilesDir
	}
	if _, statErr := os.Stat(profilesDir); statErr != nil {
		return nil, fmt.Errorf("SNMP profiles directory not found: %s", profilesDir)
	}

	loader, loadErr := profiles.NewLoader(profilesDir, logger)
	if loadErr != nil {
		return nil, fmt.Errorf("loading SNMP profiles from %s: %w", profilesDir, loadErr)
	}
	resolvedProfiles, resolveErr := loader.AllResolved()
	if resolveErr != nil {
		return nil, fmt.Errorf("resolving SNMP profiles: %w", resolveErr)
	}
	matcher := profiles.NewMatcher(resolvedProfiles)
	runner.metricsCollector = collector.NewMetricsCollector(clientFactory, matcher, logger, snmpTimeout, policy.Config.Retries)
	logger.Info("SNMP metrics collection enabled", "profiles_dir", profilesDir, "profile_count", loader.Count(), "interval", runner.metricsInterval)

	// Schedule a metrics job for each expanded target
	for _, target := range runner.scope.Targets {
		expandedIPs, err := targets.Expand(target.Host)
		if err != nil {
			logger.Warn("Error expanding target host, skipping", "host", target.Host, "error", err)
			continue
		}
		for _, ip := range expandedIPs {
			t := config.Target{
				Host:           ip,
				Port:           target.Port,
				ID:             target.ID,
				Authentication: target.Authentication,
			}
			if t.Port == 0 {
				t.Port = 161
			}
			metricsTask := gocron.NewTask(runner.runMetrics, t)
			_, err = s.NewJob(gocron.DurationJob(runner.metricsInterval), metricsTask,
				gocron.WithSingletonMode(gocron.LimitModeReschedule))
			if err != nil {
				return nil, fmt.Errorf("scheduling metrics job for %s: %w", ip, err)
			}
		}
	}

	return runner, nil
}

// resolveTargetAuthentication returns the authentication to use for a target.
// Uses target-level auth if available, otherwise falls back to scope-level auth.
func (r *Runner) resolveTargetAuthentication(target config.Target) *config.Authentication {
	if target.Authentication != nil {
		return target.Authentication
	}
	return &r.scope.Authentication
}

// runMetrics collects SNMP operational metrics from a target using its matched profile.
func (r *Runner) runMetrics(target config.Target) {
	policyName := r.ctx.Value(policyKey).(string)
	r.logger.Debug("Running SNMP metrics collection", "host", target.Host, "policy", policyName)
	ctx, cancel := context.WithTimeout(r.ctx, r.metricsInterval)
	defer cancel()
	auth := r.resolveTargetAuthentication(target)
	if err := r.metricsCollector.CollectTarget(ctx, target, auth, policyName); err != nil {
		r.logger.Warn("SNMP metrics collection failed", "host", target.Host, "policy", policyName, "error", err)
	}
}

// Start starts the policy runner
func (r *Runner) Start() {
	r.logger.Info("Starting policy runner", "policy", r.ctx.Value(policyKey))
	r.scheduler.Start()
}

// Stop stops the policy runner
func (r *Runner) Stop() error {
	if err := r.scheduler.StopJobs(); err != nil {
		return err
	}
	return r.scheduler.Shutdown()
}
