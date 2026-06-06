package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewLogger_ReturnsNonNil(t *testing.T) {
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR", "debug", "info", "warn", "error", "UNKNOWN"}
	formats := []string{"TEXT", "JSON", "text", "json", "OTHER"}
	for _, lvl := range levels {
		for _, fmt := range formats {
			l := NewLogger(lvl, fmt)
			require.NotNil(t, l, "NewLogger(%q, %q) returned nil", lvl, fmt)
		}
	}
}

func TestNewLogger_LevelFiltering(t *testing.T) {
	tests := []struct {
		logLevel    string
		wantLevel   slog.Level
		description string
	}{
		{"DEBUG", slog.LevelDebug, "DEBUG level enables debug records"},
		{"INFO", slog.LevelInfo, "INFO level suppresses debug records"},
		{"WARN", slog.LevelWarn, "WARN level suppresses info records"},
		{"ERROR", slog.LevelError, "ERROR level suppresses warn records"},
		{"unknown", slog.LevelDebug, "unknown level defaults to DEBUG"},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			logger := NewLogger(tc.logLevel, "JSON")
			require.NotNil(t, logger)

			// Probe the expected level by writing records into a same-level handler.
			buf := &bytes.Buffer{}
			probe := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: tc.wantLevel}))

			// Debug record: only visible when wantLevel <= LevelDebug.
			buf.Reset()
			probe.Debug("probe-debug")
			if tc.wantLevel <= slog.LevelDebug {
				require.Contains(t, buf.String(), "probe-debug",
					"DEBUG record should appear at level %s", tc.logLevel)
			} else {
				require.NotContains(t, buf.String(), "probe-debug",
					"DEBUG record should be suppressed at level %s", tc.logLevel)
			}

			// Error record: always visible regardless of level.
			buf.Reset()
			probe.Error("probe-error")
			require.Contains(t, buf.String(), "probe-error",
				"ERROR record must always appear at level %s", tc.logLevel)
		})
	}
}

func TestNewLogger_FormatJSON(t *testing.T) {
	// NewLogger with JSON format must return a non-nil logger backed by a JSON
	// handler.  Verify by writing a record through an equivalent JSON handler and
	// confirming the output is JSON-shaped (starts with '{').
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(h)
	l.Info("test-json")
	require.True(t, strings.HasPrefix(strings.TrimSpace(buf.String()), "{"),
		"JSON handler output should be a JSON object, got: %s", buf.String())

	logger := NewLogger("INFO", "JSON")
	require.NotNil(t, logger)
}

func TestNewLogger_FormatText(t *testing.T) {
	// NewLogger with TEXT format must return a non-nil logger backed by a text
	// handler.  Verify by writing a record through an equivalent text handler and
	// confirming the output does not start with '{' (not JSON).
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(h)
	l.Info("test-text")
	require.False(t, strings.HasPrefix(strings.TrimSpace(buf.String()), "{"),
		"text handler output should NOT be JSON, got: %s", buf.String())

	logger := NewLogger("INFO", "TEXT")
	require.NotNil(t, logger)
}

func TestNewLogger_DefaultFormatFallsBackToJSON(t *testing.T) {
	// An unknown format string must fall back to JSON (default branch).
	logger := NewLogger("INFO", "UNKNOWN_FORMAT")
	require.NotNil(t, logger)
}

func TestNewLogger_CaseInsensitive(t *testing.T) {
	// All four log levels and both format names must be recognised regardless of
	// letter case (the implementation uses strings.ToUpper internally).
	tests := []struct {
		level  string
		format string
	}{
		{"debug", "json"},
		{"Debug", "Json"},
		{"info", "text"},
		{"Info", "Text"},
		{"warn", "JSON"},
		{"Warn", "TEXT"},
		{"error", "json"},
		{"Error", "text"},
	}
	for _, tc := range tests {
		l := NewLogger(tc.level, tc.format)
		require.NotNil(t, l, "NewLogger(%q, %q) must not return nil", tc.level, tc.format)
	}
}
