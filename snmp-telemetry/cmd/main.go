package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/netboxlabs/orb-discovery/snmp-telemetry/config"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/env"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/metrics"
	"github.com/netboxlabs/orb-discovery/snmp-telemetry/policy"
	"gopkg.in/yaml.v3"
)

// AppName is the application name
const AppName = "snmp-telemetry"

func main() {
	configFile := flag.String("config", "", "path to YAML configuration file (required)")
	otelEndpoint := flag.String("otel-endpoint", "", "OpenTelemetry exporter endpoint (e.g. localhost:4317)."+
		" Environment variable can be used by wrapping it in ${} (e.g. ${OTEL_ENDPOINT})")
	otelExportPeriod := flag.Int("otel-export-period", 10, "period in seconds between OpenTelemetry exports")
	snmpProfilesDir := flag.String("snmp-profiles-dir", "", "default directory for ktranslate-compatible SNMP profile YAML files."+
		" Overrides the built-in default (/usr/local/share/snmp-profiles). Per-policy profiles_dir still takes precedence."+
		" Environment variable can be used by wrapping it in ${} (e.g. ${SNMP_PROFILES_DIR})")
	logLevel := flag.String("log-level", "INFO", "log level (DEBUG, INFO, WARN, ERROR)")
	logFormat := flag.String("log-format", "TEXT", "log format (TEXT, JSON)")
	help := flag.Bool("help", false, "show this help")

	flag.Parse()

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", AppName)
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "error: --config is required\n")
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", AppName)
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := config.NewLogger(*logLevel, *logFormat)

	// Load configuration file
	configData, err := os.ReadFile(*configFile)
	if err != nil {
		logger.Error("Failed to read config file", "path", *configFile, "error", err)
		os.Exit(1)
	}

	var appConfig config.AppConfig
	if err := yaml.Unmarshal(configData, &appConfig); err != nil {
		logger.Error("Failed to parse config file", "path", *configFile, "error", err)
		os.Exit(1)
	}

	if len(appConfig.Policies) == 0 {
		logger.Error("No policies found in config file", "path", *configFile)
		os.Exit(1)
	}

	ctx := context.Background()

	endpoint := env.ResolveEnvOrExit(*otelEndpoint)
	if endpoint != "" {
		if err := metrics.SetupMetricsExport(ctx, logger, endpoint, *otelExportPeriod); err != nil {
			logger.Error("Failed to setup metrics export", "error", err)
			os.Exit(1)
		}
		logger.Info("Metrics export configured", "endpoint", endpoint, "period_seconds", *otelExportPeriod)
	}

	profilesDir := env.ResolveEnvOrExit(*snmpProfilesDir)
	manager := policy.NewManager(ctx, logger, profilesDir)

	if err := manager.StartAll(appConfig.Policies); err != nil {
		logger.Error("Failed to start policies", "error", err)
		os.Exit(1)
	}

	// Handle signals
	done := make(chan bool, 1)
	rootCtx, cancelFunc := context.WithCancel(ctx)

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		for {
			select {
			case <-sigs:
				logger.Warn("stop signal received, stopping snmp-telemetry")
				if err := manager.Stop(); err != nil {
					logger.Error("failed to stop manager", "error", err)
				}
				if err := metrics.Shutdown(ctx); err != nil {
					logger.Error("failed to shutdown metrics", "error", err)
				}
				cancelFunc()
			case <-rootCtx.Done():
				logger.Warn("main context cancelled")
				done <- true
				return
			}
		}
	}()

	<-done
}
