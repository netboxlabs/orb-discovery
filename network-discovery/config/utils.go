package config

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// NewLogger creates a configured slog
func NewLogger(logLevel string, logFormat string) *slog.Logger {
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

// ReadConfigFile reads required config file
func ReadConfigFile() ([]byte, error) {

	configPath := flag.String("config", "", "path to the configuration file (required)")

	flag.Parse()

	if *configPath == "" {
		fmt.Fprintf(os.Stderr, "Usage of network-discovery:\n")
		flag.PrintDefaults()
		return nil, fmt.Errorf("")

	}
	if _, err := os.Stat(*configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("configuration file '%s' does not exist", *configPath)
	}

	fileData, err := os.ReadFile(*configPath)
	if err != nil {
		return nil, fmt.Errorf("error reading configuration file: %v", err)
	}

	return fileData, nil
}
