package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
	"github.com/netboxlabs/orb-discovery/network-discovery/policy"
	"github.com/netboxlabs/orb-discovery/network-discovery/server"
	"gopkg.in/yaml.v3"
)

func newLogger(logLevel string, logFormat string) *slog.Logger {
	var l slog.Level
	switch strings.ToUpper(logLevel) {
	case "DEBUG":
		l = slog.LevelDebug
	case "INFO":
		l = slog.LevelInfo
	case "WARN":
		l = slog.LevelWarn
	case "ERROR":
		l = slog.LevelError
	default:
		l = slog.LevelDebug
	}

	var h slog.Handler
	switch strings.ToUpper(logFormat) {
	case "TEXT":
		h = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: l, AddSource: false})
	case "JSON":
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l, AddSource: false})
	default:
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l, AddSource: false})
	}

	return slog.New(h)
}

func main() {

	configPath := flag.String("config", "", "path to the configuration file (required)")

	flag.Parse()

	if *configPath == "" {
		fmt.Fprintf(os.Stderr, "Usage of network-discovery:\n")
		flag.PrintDefaults()
		os.Exit(1)
	}
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		fmt.Printf("configuration file '%s' does not exist\n", *configPath)
		os.Exit(1)
	}

	yamlData, err := os.ReadFile(*configPath)
	if err != nil {
		fmt.Printf("error reading configuration file: %v\n", err)
		os.Exit(1)
	}

	config := config.Config{
		Network: config.Network{
			Config: config.StartupConfig{
				Host: "localhost",
				Port: 8073,
			}},
	}

	if err = yaml.Unmarshal(yamlData, &config); err != nil {
		fmt.Printf("error parsing configuration file: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	logger := newLogger("INFO", "TEXT")

	policyManager := policy.Manager{}
	err = policyManager.Configure(ctx, logger, config.Network.Config)
	if err != nil {
		logger.Error("policy manager configuration error", slog.Any("error", err))
		os.Exit(1)
	}

	server := server.Server{}
	server.Configure(logger, &policyManager, config.Network.Config)

	// handle signals
	done := make(chan bool, 1)
	rootCtx, cancelFunc := context.WithCancel(context.Background())

	go func() {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)
		for {
			select {
			case <-sigs:
				logger.Warn("stop signal received, stopping network-discovery")
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
