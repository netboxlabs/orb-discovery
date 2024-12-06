package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/netboxlabs/diode-sdk-go/diode"
	"gopkg.in/yaml.v3"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
	"github.com/netboxlabs/orb-discovery/network-discovery/policy"
	"github.com/netboxlabs/orb-discovery/network-discovery/server"
)

// DefaultAppName is the default application name
const DefaultAppName = "network-discovery"

// set via ldflags -X option at build time
var version = "unknown"

func main() {

	fileData, err := config.RequireConfig()
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	c := config.Config{
		Network: config.Network{
			Config: config.StartupConfig{
				Host:      "0.0.0.0",
				Port:      8073,
				LogLevel:  "INFO",
				LogFormat: "TEXT",
			}},
	}

	if err = yaml.Unmarshal(fileData, &c); err != nil {
		fmt.Printf("error parsing configuration file: %v\n", err)
		os.Exit(1)
	}

	client, err := diode.NewClient(
		c.Network.Config.Target,
		DefaultAppName,
		version,
		diode.WithAPIKey(c.Network.Config.APIKey),
	)
	if err != nil {
		fmt.Printf("error creating diode client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	logger := config.NewLogger(c.Network.Config.LogLevel, c.Network.Config.LogFormat)

	policyManager := policy.Manager{}
	err = policyManager.Configure(ctx, logger, client)
	if err != nil {
		logger.Error("policy manager configuration error", slog.Any("error", err))
		os.Exit(1)
	}

	server := server.Server{}
	server.Configure(logger, &policyManager, version, c.Network.Config)

	// handle signals
	done := make(chan bool, 1)
	rootCtx, cancelFunc := context.WithCancel(context.Background())

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
		for {
			select {
			case <-sigs:
				logger.Warn("stop signal received, stopping network-discovery")
				server.Stop()
				cancelFunc()
			case <-rootCtx.Done():
				logger.Warn("main context cancelled")
				done <- true
				return
			}
		}
	}()

	err = server.Start()
	if err != nil {
		logger.Error("network-discovery startup error")
		os.Exit(1)
	}

	<-done
}
