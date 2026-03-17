package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/env"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/policy"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/version"
)

// AppName is the application name
const AppName = "probe-telemetry"

func main() {
	configFile := flag.String("config", "", "path to YAML configuration file (required)")
	otelEndpoint := flag.String("otel-endpoint", "", "OpenTelemetry gRPC exporter endpoint (e.g. grpc://localhost:4317)."+
		" Environment variable can be used by wrapping it in ${} (e.g. ${OTEL_ENDPOINT})")
	otelExportPeriod := flag.Int("otel-export-period", 10, "period in seconds between OpenTelemetry metric exports")
	logLevel := flag.String("log-level", "INFO", "log level (DEBUG, INFO, WARN, ERROR)")
	logFormat := flag.String("log-format", "TEXT", "log format (TEXT, JSON)")
	help := flag.Bool("help", false, "show this help")

	flag.Parse()

	if *help {
		fmt.Fprintf(os.Stderr, "Usage of %s (version %s):\n", AppName, version.GetBuildVersion())
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *configFile == "" {
		fmt.Fprintf(os.Stderr, "error: --config is required\n\nUsage of %s:\n", AppName)
		flag.PrintDefaults()
		os.Exit(1)
	}

	logger := config.NewLogger(*logLevel, *logFormat)
	logger.Info("starting "+AppName, "version", version.GetBuildVersion())

	raw, err := os.ReadFile(*configFile)
	if err != nil {
		logger.Error("failed to read config file", "path", *configFile, "error", err)
		os.Exit(1)
	}

	var appConfig config.AppConfig
	if err := yaml.Unmarshal(raw, &appConfig); err != nil {
		logger.Error("failed to parse config file", "path", *configFile, "error", err)
		os.Exit(1)
	}

	if len(appConfig.Policies) == 0 {
		logger.Error("no policies found in config file", "path", *configFile)
		os.Exit(1)
	}

	endpoint := env.ResolveEnvOrExit(*otelEndpoint)
	if endpoint == "" {
		logger.Info("no OTel endpoint configured, metrics export disabled")
	} else {
		logger.Info("OTel metrics export enabled", "endpoint", endpoint, "period_seconds", *otelExportPeriod)
	}

	ctx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	manager := policy.NewManager(ctx, logger, endpoint, *otelExportPeriod)

	if err := manager.StartAll(appConfig.Policies); err != nil {
		logger.Error("failed to start policies", "error", err)
		os.Exit(1)
	}

	// Handle termination signals
	done := make(chan struct{})
	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		<-sigs
		logger.Info("shutdown signal received, stopping " + AppName)
		if err := manager.Stop(); err != nil {
			logger.Error("error stopping manager", "error", err)
		}
		cancelRoot()
		close(done)
	}()

	<-done
	logger.Info(AppName + " stopped")
}
