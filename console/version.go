package console

import (
	_ "embed"
	"strings"
)

var (
	//go:embed version/major
	major string
	//go:embed version/minor
	minor string
	//go:embed version/patch
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
		describe := strings.TrimSpace(GitDescribe)
		return strings.TrimPrefix(describe, "v")
	}
	return strings.TrimSpace(major) + "." + strings.TrimSpace(minor) + "." + strings.TrimSpace(patch)
}
