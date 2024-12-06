package config_test

import (
	"flag"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netboxlabs/orb-discovery/network-discovery/config"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		desc          string
		loggingLevel  string
		loggingFormat string
	}{
		{
			desc:          "with debug level and json format",
			loggingLevel:  "debug",
			loggingFormat: "json",
		},
		{
			desc:          "with debug level and text format",
			loggingLevel:  "debug",
			loggingFormat: "text",
		},
		{
			desc:          "with info level and json format",
			loggingLevel:  "info",
			loggingFormat: "json",
		},
		{
			desc:          "with info level and text format",
			loggingLevel:  "warn",
			loggingFormat: "json",
		},
		{
			desc:          "with error level and text format",
			loggingLevel:  "error",
			loggingFormat: "text",
		},
		{
			desc:          "with error level and empty format",
			loggingLevel:  "error",
			loggingFormat: "",
		},
		{
			desc:          "with empty level and text format",
			loggingLevel:  "",
			loggingFormat: "text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			log := config.NewLogger(tt.loggingLevel, tt.loggingFormat)
			require.NotNil(t, log)

			handlerOK := false
			if tt.loggingFormat == "text" {
				_, handlerOK = log.Handler().(*slog.TextHandler)
			} else {
				_, handlerOK = log.Handler().(*slog.JSONHandler)
			}
			assert.True(t, handlerOK)
		})
	}
}

func TestRequireConfig(t *testing.T) {
	t.Run("No Config Path Provided", func(t *testing.T) {
		// Simulate no flags passed
		os.Args = []string{"network-discovery"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags

		data, err := config.RequireConfig()
		assert.Nil(t, data)
		assert.EqualError(t, err, "")
	})

	t.Run("Config File Does Not Exist", func(t *testing.T) {
		// Simulate a non-existent file
		os.Args = []string{"network-discovery", "-config", "/non/existent/path"}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags

		data, err := config.RequireConfig()
		assert.Nil(t, data)
		assert.EqualError(t, err, "configuration file '/non/existent/path' does not exist")
	})

	t.Run("Error Reading Config File", func(t *testing.T) {
		// Create a file and simulate an error by removing it before reading
		tmpFile, err := os.CreateTemp("", "test-config")
		assert.NoError(t, err)
		tmpFilePath := tmpFile.Name()
		err = tmpFile.Close()
		assert.NoError(t, err)

		// Remove the file to simulate read error
		err = os.Remove(tmpFilePath)
		assert.NoError(t, err)

		os.Args = []string{"network-discovery", "-config", tmpFilePath}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags

		data, err := config.RequireConfig()
		assert.Nil(t, data)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("Valid Config File", func(t *testing.T) {
		// Create a temporary file with valid content
		tmpFile, err := os.CreateTemp("", "test-config")
		assert.NoError(t, err)
		defer func() {
			err = os.Remove(tmpFile.Name())
			assert.NoError(t, err)
		}()

		// Write YAML content to the file
		content := "network:\n  policies:\n    discovery_1:\n      config:\n        schedule: '* * * * *'"
		_, err = tmpFile.WriteString(content)
		assert.NoError(t, err)
		err = tmpFile.Close()
		assert.NoError(t, err)

		// Pass the file path as a flag
		os.Args = []string{"network-discovery", "-config", tmpFile.Name()}
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError) // Reset flags

		data, err := config.RequireConfig()
		assert.NoError(t, err)
		assert.Equal(t, content, string(data))
	})
}
