package version

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetBuildVersion(t *testing.T) {
	// Returns the embedded build version with surrounding whitespace trimmed.
	assert.Equal(t, strings.TrimSpace(buildVersion), GetBuildVersion())
	assert.Equal(t, GetBuildVersion(), strings.TrimSpace(GetBuildVersion()), "must be trimmed")
}

func TestGetBuildCommit(t *testing.T) {
	assert.Equal(t, strings.TrimSpace(buildCommit), GetBuildCommit())
	assert.Equal(t, GetBuildCommit(), strings.TrimSpace(GetBuildCommit()), "must be trimmed")
}
