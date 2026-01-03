package version

import (
	_ "embed"
	"strings"
)

var (
	//go:embed embedded/major
	major string
	//go:embed embedded/minor
	minor string
	//go:embed embedded/patch
	patch string

	// GitDescribe is set by ldflags at build time.
	GitDescribe string
	// GitCommit is set by ldflags at build time.
	GitCommit string
	// GitTreeState is set by ldflags at build time.
	GitTreeState string
	// BuildDate is set by ldflags at build time.
	BuildDate string
)

// GetVersion returns the semantic version string.
func GetVersion() string {
	if GitDescribe != "" {
		return GitDescribe
	}
	return strings.TrimSpace(major) + "." + strings.TrimSpace(minor) + "." + strings.TrimSpace(patch)
}
