package version

import (
	_ "embed"
	"strings"
)

//go:embed BUILD_VERSION.txt
var buildVersion string

//go:embed BUILD_COMMIT.txt
var buildCommit string

// GetBuildVersion returns the build version of flow-telemetry.
func GetBuildVersion() string {
	return strings.TrimSpace(buildVersion)
}

// GetBuildCommit returns the build commit of flow-telemetry.
func GetBuildCommit() string {
	return strings.TrimSpace(buildCommit)
}
