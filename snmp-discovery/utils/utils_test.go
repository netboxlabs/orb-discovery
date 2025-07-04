package utils_test

import (
	"os"
	"testing"

	"github.com/netboxlabs/orb-discovery/snmp-discovery/utils"
	"github.com/stretchr/testify/assert"
)

func TestResolveEnv(t *testing.T) {
	t.Run("No environment variable substitution", func(t *testing.T) {
		result, err := utils.ResolveEnv("simple-value")
		assert.NoError(t, err)
		assert.Equal(t, "simple-value", result)
	})

	t.Run("Environment variable substitution - set", func(t *testing.T) {
		// Set test environment variable
		os.Setenv("TEST_VAR", "test-value")
		defer os.Unsetenv("TEST_VAR")

		result, err := utils.ResolveEnv("${TEST_VAR}")
		assert.NoError(t, err)
		assert.Equal(t, "test-value", result)
	})

	t.Run("Environment variable substitution - not set", func(t *testing.T) {
		// Ensure the environment variable is not set
		os.Unsetenv("NONEXISTENT_VAR")

		result, err := utils.ResolveEnv("${NONEXISTENT_VAR}")
		assert.Error(t, err)
		assert.Equal(t, "", result)
		assert.Contains(t, err.Error(), "environment variable NONEXISTENT_VAR is not set")
	})

	t.Run("Partial environment variable syntax", func(t *testing.T) {
		result, err := utils.ResolveEnv("${TEST_VAR")
		assert.NoError(t, err)
		assert.Equal(t, "${TEST_VAR", result)
	})

	t.Run("Partial environment variable syntax 2", func(t *testing.T) {
		result, err := utils.ResolveEnv("TEST_VAR}")
		assert.NoError(t, err)
		assert.Equal(t, "TEST_VAR}", result)
	})

	t.Run("Empty environment variable name", func(t *testing.T) {
		result, err := utils.ResolveEnv("${}")
		assert.NoError(t, err)
		assert.Equal(t, "${}", result)
	})

	t.Run("Mixed content with environment variable", func(t *testing.T) {
		os.Setenv("TEST_VAR", "test-value")
		defer os.Unsetenv("TEST_VAR")

		result, err := utils.ResolveEnv("prefix-${TEST_VAR}-suffix")
		assert.NoError(t, err)
		assert.Equal(t, "prefix-${TEST_VAR}-suffix", result)
	})

	t.Run("Multiple environment variables in same string", func(t *testing.T) {
		os.Setenv("VAR1", "value1")
		os.Setenv("VAR2", "value2")
		defer func() {
			os.Unsetenv("VAR1")
			os.Unsetenv("VAR2")
		}()

		// Current implementation only handles single environment variables
		// Multiple variables in one string are not supported and will return an error
		result, err := utils.ResolveEnv("${VAR1}-${VAR2}")
		assert.Error(t, err)
		assert.Equal(t, "", result)
		assert.Contains(t, err.Error(), "environment variable VAR1}-${VAR2 is not set")
	})

	t.Run("Environment variable with special characters", func(t *testing.T) {
		os.Setenv("TEST_VAR_123", "test-value-123")
		defer os.Unsetenv("TEST_VAR_123")

		result, err := utils.ResolveEnv("${TEST_VAR_123}")
		assert.NoError(t, err)
		assert.Equal(t, "test-value-123", result)
	})

	t.Run("Empty string", func(t *testing.T) {
		result, err := utils.ResolveEnv("")
		assert.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("Only braces", func(t *testing.T) {
		result, err := utils.ResolveEnv("{}")
		assert.NoError(t, err)
		assert.Equal(t, "{}", result)
	})
}

func TestResolveEnvOrExit(t *testing.T) {
	t.Run("No environment variable substitution", func(t *testing.T) {
		result := utils.ResolveEnvOrExit("simple-value")
		assert.Equal(t, "simple-value", result)
	})

	t.Run("Environment variable substitution - set", func(t *testing.T) {
		// Set test environment variable
		os.Setenv("TEST_VAR", "test-value")
		defer os.Unsetenv("TEST_VAR")

		result := utils.ResolveEnvOrExit("${TEST_VAR}")
		assert.Equal(t, "test-value", result)
	})

	// Note: Testing the case where environment variable is not set is not practical
	// because ResolveEnvOrExit calls os.Exit(1), which terminates the test process.
	// This behavior is expected and documented.
}
