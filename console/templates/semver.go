package templates

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// ParseVersion parses and validates a semver version string. It accepts
// versions with or without a "v" prefix (e.g. "1.2.3" or "v1.2.3").
// The version must be a strict MAJOR.MINOR.PATCH triple with no prerelease
// or build metadata (e.g. "1.2.3-beta.1" and "1.2.3+meta" are rejected).
func ParseVersion(version string) (*semver.Version, error) {
	raw := strings.TrimPrefix(version, "v")
	v, err := semver.StrictNewVersion(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid semver version %q: %w", version, err)
	}
	if v.Prerelease() != "" {
		return nil, fmt.Errorf("invalid semver version %q: prerelease versions are not supported", version)
	}
	if v.Metadata() != "" {
		return nil, fmt.Errorf("invalid semver version %q: build metadata is not supported", version)
	}
	return v, nil
}

// ParseConstraint parses a semver constraint string (e.g. ">=1.0.0 <2.0.0").
// An empty constraint matches all versions.
func ParseConstraint(constraint string) (*semver.Constraints, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil, nil // nil constraint matches everything
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	return c, nil
}

// MatchesConstraint checks whether a version satisfies a constraint. A nil
// constraint matches all versions.
func MatchesConstraint(v *semver.Version, c *semver.Constraints) bool {
	if c == nil {
		return true
	}
	return c.Check(v)
}

// SortVersionsDesc sorts a slice of semver versions in descending order
// (newest first).
func SortVersionsDesc(versions []*semver.Version) {
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].GreaterThan(versions[j])
	})
}

// ReleaseConfigMapName returns the DNS-label-safe ConfigMap name for a release:
// {template-name}--v{major}-{minor}-{patch}
func ReleaseConfigMapName(templateName string, version *semver.Version) string {
	return fmt.Sprintf("%s--v%d-%d-%d", templateName, version.Major(), version.Minor(), version.Patch())
}

// LatestMatchingVersion returns the highest version from the given list that
// satisfies the constraint. Returns nil if no version matches. The input slice
// is not modified.
func LatestMatchingVersion(versions []*semver.Version, c *semver.Constraints) *semver.Version {
	// Make a copy to avoid mutating the caller's slice.
	sorted := make([]*semver.Version, len(versions))
	copy(sorted, versions)
	SortVersionsDesc(sorted)

	for _, v := range sorted {
		if MatchesConstraint(v, c) {
			return v
		}
	}
	return nil
}

// OldestMatchingVersion returns the lowest version from the given list that
// satisfies the constraint. Returns nil if no version matches. The input slice
// is not modified.
func OldestMatchingVersion(versions []*semver.Version, c *semver.Constraints) *semver.Version {
	// Make a copy to avoid mutating the caller's slice.
	sorted := make([]*semver.Version, len(versions))
	copy(sorted, versions)
	SortVersionsDesc(sorted)

	var oldest *semver.Version
	for _, v := range sorted {
		if MatchesConstraint(v, c) {
			oldest = v
		}
	}
	return oldest
}
