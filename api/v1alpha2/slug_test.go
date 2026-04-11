package v1alpha2

import (
	"context"
	"fmt"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple lowercase", input: "frontend", want: "frontend"},
		{name: "mixed case", input: "My Folder", want: "my-folder"},
		{name: "parens and version", input: "Engineering (v2)", want: "engineering-v2"},
		{name: "multiple spaces", input: "hello   world", want: "hello-world"},
		{name: "leading trailing spaces", input: "  padded  ", want: "padded"},
		{name: "special characters", input: "foo@bar#baz!", want: "foo-bar-baz"},
		{name: "already slugified", input: "my-project", want: "my-project"},
		{name: "numbers", input: "project-42", want: "project-42"},
		{name: "underscores converted", input: "my_project_name", want: "my-project-name"},
		{name: "dots converted", input: "api.v2.service", want: "api-v2-service"},
		{name: "empty string", input: "", want: ""},
		{name: "only special chars", input: "!!!@@@", want: ""},
		{name: "unicode", input: "caf\u00e9 latt\u00e9", want: "caf-latt"},
		{name: "consecutive hyphens collapse", input: "a---b", want: "a-b"},
		{name: "leading/trailing hyphens stripped", input: "-hello-", want: "hello"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Slugify(tc.input)
			if got != tc.want {
				t.Errorf("Slugify(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsValidSlug(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"frontend", true},
		{"my-project", true},
		{"project-42", true},
		{"a", true},
		{"", false},
		{"My Folder", false},
		{"UPPERCASE", false},
		{"has spaces", false},
		{"trailing-", false},
		{"-leading", false},
		{"double--hyphen", false},
		{"special@chars", false},
		{"under_score", false},
		{"dot.name", false},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("%q", tc.input), func(t *testing.T) {
			got := IsValidSlug(tc.input)
			if got != tc.want {
				t.Errorf("IsValidSlug(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestGenerateIdentifier(t *testing.T) {
	ctx := context.Background()

	t.Run("available slug returns plain slug", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return false, nil
		}
		got, err := GenerateIdentifier(ctx, "My Folder", "holos-fld-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "my-folder" {
			t.Errorf("got %q, want %q", got, "my-folder")
		}
	})

	t.Run("collision appends suffix", func(t *testing.T) {
		callCount := 0
		exists := func(_ context.Context, nsName string) (bool, error) {
			callCount++
			// First call: plain slug is taken
			if callCount == 1 {
				return true, nil
			}
			// Second call: suffixed slug is available
			return false, nil
		}
		got, err := GenerateIdentifier(ctx, "My Folder", "holos-fld-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have a 6-digit suffix
		if len(got) < len("my-folder-000000") {
			t.Errorf("expected suffixed slug, got %q", got)
		}
		if got[:len("my-folder-")] != "my-folder-" {
			t.Errorf("expected prefix 'my-folder-', got %q", got)
		}
	})

	t.Run("exhausted retries returns error", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return true, nil // everything is taken
		}
		_, err := GenerateIdentifier(ctx, "My Folder", "holos-fld-", exists)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("exists error propagates", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return false, fmt.Errorf("k8s unavailable")
		}
		_, err := GenerateIdentifier(ctx, "My Folder", "holos-fld-", exists)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("prefix is used for exists check", func(t *testing.T) {
		var checkedNames []string
		callCount := 0
		exists := func(_ context.Context, nsName string) (bool, error) {
			checkedNames = append(checkedNames, nsName)
			callCount++
			if callCount == 1 {
				return true, nil // plain slug taken
			}
			return false, nil // suffixed slug available
		}
		_, err := GenerateIdentifier(ctx, "frontend", "holos-prj-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// First check should be for the prefixed plain slug
		if len(checkedNames) < 1 {
			t.Fatal("expected at least 1 check")
		}
		if checkedNames[0] != "holos-prj-frontend" {
			t.Errorf("first check should be %q, got %q", "holos-prj-frontend", checkedNames[0])
		}
		// Second check should be for the prefixed suffixed slug
		if len(checkedNames) < 2 {
			t.Fatal("expected at least 2 checks")
		}
		if len(checkedNames[1]) < len("holos-prj-frontend-") {
			t.Errorf("second check should be prefixed suffixed slug, got %q", checkedNames[1])
		}
	})
}

func TestCheckIdentifier(t *testing.T) {
	ctx := context.Background()

	t.Run("valid slug available", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return false, nil
		}
		result, err := CheckIdentifier(ctx, "frontend", "holos-prj-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Available {
			t.Error("expected available=true")
		}
		if result.SuggestedIdentifier != "frontend" {
			t.Errorf("expected suggested 'frontend', got %q", result.SuggestedIdentifier)
		}
	})

	t.Run("valid slug taken returns suffixed suggestion", func(t *testing.T) {
		callCount := 0
		exists := func(_ context.Context, _ string) (bool, error) {
			callCount++
			if callCount <= 2 {
				return true, nil // plain slug taken (once by CheckIdentifier, once by GenerateIdentifier)
			}
			return false, nil // suffixed slug available
		}
		result, err := CheckIdentifier(ctx, "frontend", "holos-prj-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Available {
			t.Error("expected available=false")
		}
		if len(result.SuggestedIdentifier) < len("frontend-000000") {
			t.Errorf("expected suffixed suggestion, got %q", result.SuggestedIdentifier)
		}
	})

	t.Run("non-slug input returns available=false with slugified suggestion", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return false, nil // slugified form is available
		}
		result, err := CheckIdentifier(ctx, "My Project", "holos-prj-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Available {
			t.Error("expected available=false for non-slug input")
		}
		if result.SuggestedIdentifier != "my-project" {
			t.Errorf("expected suggested 'my-project', got %q", result.SuggestedIdentifier)
		}
	})

	t.Run("non-slug input with taken suggestion returns suffixed", func(t *testing.T) {
		callCount := 0
		exists := func(_ context.Context, _ string) (bool, error) {
			callCount++
			if callCount == 1 {
				return true, nil // "my-project" is taken
			}
			return false, nil // suffixed is available
		}
		result, err := CheckIdentifier(ctx, "My Project", "holos-prj-", exists)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Available {
			t.Error("expected available=false for non-slug input")
		}
		if len(result.SuggestedIdentifier) < len("my-project-000000") {
			t.Errorf("expected suffixed suggestion, got %q", result.SuggestedIdentifier)
		}
	})

	t.Run("exists error propagates", func(t *testing.T) {
		exists := func(_ context.Context, _ string) (bool, error) {
			return false, fmt.Errorf("k8s unavailable")
		}
		_, err := CheckIdentifier(ctx, "frontend", "holos-prj-", exists)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
