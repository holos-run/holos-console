package v1alpha2

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
)

var (
	slugNonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]+`)
	slugMultipleHyphens = regexp.MustCompile(`-+`)
)

// Slugify converts a display name to a URL-safe slug.
// "My Folder" -> "my-folder", "Engineering (v2)" -> "engineering-v2"
func Slugify(displayName string) string {
	s := strings.ToLower(displayName)
	s = slugNonAlphanumeric.ReplaceAllString(s, "-")
	s = slugMultipleHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// IsValidSlug reports whether s is already a valid slug — lowercase alphanumeric
// with single hyphens as separators, no leading or trailing hyphens. An empty
// string is not a valid slug.
func IsValidSlug(s string) bool {
	return s != "" && Slugify(s) == s
}

// GenerateIdentifier produces a globally unique identifier for a folder or project.
// It slugifies the display name, then checks availability using the exists function.
// If the plain slug is taken, it appends a random 6-digit suffix and retries up to
// 10 times.
//
// The prefix parameter (e.g. "holos-fld-") is prepended to the slug when calling
// exists, but the returned identifier is the slug only (without prefix).
//
// The suggested_identifier is NOT reserved -- the Create RPC handles the race with
// retry logic.
func GenerateIdentifier(ctx context.Context, displayName, prefix string, exists func(ctx context.Context, namespaceName string) (bool, error)) (string, error) {
	slug := Slugify(displayName)
	candidate := prefix + slug
	taken, err := exists(ctx, candidate)
	if err != nil {
		return "", err
	}
	if !taken {
		return slug, nil
	}

	for i := 0; i < 10; i++ {
		suffix := fmt.Sprintf("%06d", rand.Intn(1000000))
		candidate = prefix + slug + "-" + suffix
		taken, err = exists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !taken {
			return slug + "-" + suffix, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique identifier after 10 attempts")
}

// CheckIdentifierResult holds the outcome of a CheckIdentifier call.
type CheckIdentifierResult struct {
	Available           bool
	SuggestedIdentifier string
}

// CheckIdentifier validates and checks availability of a user-supplied identifier.
// Unlike GenerateIdentifier (which slugifies a display name), this function validates
// that the input is already a valid slug. If not, it returns available=false with the
// slugified form as the suggestion (and checks that slug's availability).
func CheckIdentifier(ctx context.Context, identifier, prefix string, exists func(ctx context.Context, namespaceName string) (bool, error)) (*CheckIdentifierResult, error) {
	if !IsValidSlug(identifier) {
		// Input is not a valid slug — suggest the slugified form.
		suggested, err := GenerateIdentifier(ctx, identifier, prefix, exists)
		if err != nil {
			return nil, err
		}
		return &CheckIdentifierResult{
			Available:           false,
			SuggestedIdentifier: suggested,
		}, nil
	}

	// Input is a valid slug — check existence directly.
	taken, err := exists(ctx, prefix+identifier)
	if err != nil {
		return nil, err
	}
	if !taken {
		return &CheckIdentifierResult{
			Available:           true,
			SuggestedIdentifier: identifier,
		}, nil
	}

	// Taken — generate a suffixed alternative.
	suggested, err := GenerateIdentifier(ctx, identifier, prefix, exists)
	if err != nil {
		return nil, err
	}
	return &CheckIdentifierResult{
		Available:           false,
		SuggestedIdentifier: suggested,
	}, nil
}
