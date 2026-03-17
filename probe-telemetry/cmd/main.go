package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/netboxlabs/orb-discovery/probe-telemetry/config"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/env"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/policy"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/server"
	"github.com/netboxlabs/orb-discovery/probe-telemetry/version"
)

// AppName is the application name
const AppName = "probe-telemetry"

func main() {
	host := flag.String("host", "0.0.0.0", "server host")
	port := flag.Int("port", 8075, "server port")
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

	logger := config.NewLogger(*logLevel, *logFormat)
	logger.Info("starting "+AppName, "version", version.GetBuildVersion())

	endpoint := env.ResolveEnvOrExit(*otelEndpoint)
	if endpoint == "" {
		logger.Info("no OTel endpoint configured, metrics export disabled")
	} else {
		logger.Info("OTel metrics export enabled", "endpoint", endpoint, "period_seconds", *otelExportPeriod)
	}

	ctx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	manager := policy.NewManager(ctx, logger, endpoint, *otelExportPeriod)
	srv := server.NewServer(*host, *port, logger, manager, version.GetBuildVersion())

	// Handle termination signals
	done := make(chan bool, 1)
	rootCtx, cancelFunc := context.WithCancel(ctx)

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		for {
			select {
			case <-sigs:
				logger.Info("shutdown signal received, stopping " + AppName)
				srv.Stop()
				cancelFunc()
			case <-rootCtx.Done():
				logger.Info(AppName + " stopped")
				done <- true
				return
			}
		}
	}()

	serverErrCh := srv.Start()

	go func() {
		if err, ok := <-serverErrCh; ok && err != nil {
			logger.Error(AppName+" server encountered an error", "error", err)
			srv.Stop()
			cancelFunc()
		}
	}()

	<-done
}
