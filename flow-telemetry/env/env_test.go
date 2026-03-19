package env

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveEnv_PlainString(t *testing.T) {
	result, err := ResolveEnv("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestResolveEnv_EmptyString(t *testing.T) {
	result, err := ResolveEnv("")
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

func TestResolveEnv_EnvVar_Set(t *testing.T) {
	t.Setenv("TEST_FLOW_RESOLVE_VAR", "resolved-value")
	result, err := ResolveEnv("${TEST_FLOW_RESOLVE_VAR}")
	assert.NoError(t, err)
	assert.Equal(t, "resolved-value", result)
}

func TestResolveEnv_EnvVar_NotSet(t *testing.T) {
	os.Unsetenv("TEST_FLOW_MISSING_VAR")
	_, err := ResolveEnv("${TEST_FLOW_MISSING_VAR}")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TEST_FLOW_MISSING_VAR")
}

func TestResolveEnv_EmptyBraces(t *testing.T) {
	// ${} with no variable name — returns original value unchanged.
	result, err := ResolveEnv("${}")
	assert.NoError(t, err)
	assert.Equal(t, "${}", result)
}

func TestResolveEnv_PartialSyntax_OnlyDollar(t *testing.T) {
	// $VAR (no braces) is treated as a literal string.
	result, err := ResolveEnv("$VAR")
	assert.NoError(t, err)
	assert.Equal(t, "$VAR", result)
}

func TestResolveEnv_PartialSyntax_NoClosingBrace(t *testing.T) {
	// ${VAR without closing brace — not a substitution token.
	result, err := ResolveEnv("${VAR")
	assert.NoError(t, err)
	assert.Equal(t, "${VAR", result)
}

func TestResolveEnv_PartialSyntax_OnlyOpenBrace(t *testing.T) {
	result, err := ResolveEnv("{VAR}")
	assert.NoError(t, err)
	assert.Equal(t, "{VAR}", result)
}

func TestResolveEnvOrExit_PlainString(t *testing.T) {
	result := ResolveEnvOrExit("plain-value")
	assert.Equal(t, "plain-value", result)
}

func TestResolveEnvOrExit_EnvVar_Set(t *testing.T) {
	t.Setenv("TEST_FLOW_EXIT_VAR", "my-value")
	result := ResolveEnvOrExit("${TEST_FLOW_EXIT_VAR}")
	assert.Equal(t, "my-value", result)
}
