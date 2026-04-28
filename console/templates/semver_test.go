package templates

import (
	"testing"

	"github.com/Masterminds/semver/v3"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantStr string
	}{
		{name: "valid basic", input: "1.2.3", wantStr: "1.2.3"},
		{name: "valid with v prefix", input: "v1.2.3", wantStr: "1.2.3"},
		{name: "valid zero", input: "0.0.0", wantStr: "0.0.0"},
		{name: "valid high numbers", input: "100.200.300", wantStr: "100.200.300"},
		{name: "invalid prerelease", input: "1.2.3-beta.1", wantErr: true},
		{name: "invalid build metadata", input: "1.2.3+meta", wantErr: true},
		{name: "invalid prerelease with v", input: "v1.0.0-rc1", wantErr: true},
		{name: "invalid empty", input: "", wantErr: true},
		{name: "invalid letters", input: "abc", wantErr: true},
		{name: "invalid partial", input: "1.2", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if v.String() != tt.wantStr {
				t.Errorf("expected version %q, got %q", tt.wantStr, v.String())
			}
		})
	}
}

func TestParseConstraint(t *testing.T) {
	tests := []struct {
		name       string
		constraint string
		wantErr    bool
		wantNil    bool
	}{
		{name: "empty returns nil", constraint: "", wantNil: true},
		{name: "whitespace returns nil", constraint: "   ", wantNil: true},
		{name: "valid range", constraint: ">=1.0.0 <2.0.0"},
		{name: "valid caret", constraint: "^1.2.3"},
		{name: "valid tilde", constraint: "~1.2.3"},
		{name: "valid exact", constraint: "1.2.3"},
		{name: "valid wildcard", constraint: ">=1.0.0"},
		{name: "invalid", constraint: "not-a-constraint", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for constraint %q", tt.constraint)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil && c != nil {
				t.Fatalf("expected nil constraint, got %v", c)
			}
			if !tt.wantNil && c == nil {
				t.Fatal("expected non-nil constraint")
			}
		})
	}
}

func TestMatchesConstraint(t *testing.T) {
	tests := []struct {
		name       string
		version    string
		constraint string
		want       bool
	}{
		{name: "nil constraint matches all", version: "1.2.3", constraint: "", want: true},
		{name: "exact match", version: "1.2.3", constraint: "1.2.3", want: true},
		{name: "range match lower bound", version: "1.0.0", constraint: ">=1.0.0 <2.0.0", want: true},
		{name: "range match mid", version: "1.5.0", constraint: ">=1.0.0 <2.0.0", want: true},
		{name: "range no match upper bound", version: "2.0.0", constraint: ">=1.0.0 <2.0.0", want: false},
		{name: "range no match below", version: "0.9.0", constraint: ">=1.0.0 <2.0.0", want: false},
		{name: "caret match", version: "1.2.5", constraint: "^1.2.3", want: true},
		{name: "caret no match major", version: "2.0.0", constraint: "^1.2.3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseVersion(tt.version)
			if err != nil {
				t.Fatalf("failed to parse version: %v", err)
			}
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("failed to parse constraint: %v", err)
			}
			got := MatchesConstraint(v, c)
			if got != tt.want {
				t.Errorf("MatchesConstraint(%q, %q) = %v, want %v", tt.version, tt.constraint, got, tt.want)
			}
		})
	}
}

func TestSortVersionsDesc(t *testing.T) {
	versions := make([]*semver.Version, 0)
	for _, s := range []string{"1.0.0", "3.0.0", "2.0.0", "2.1.0", "1.0.1"} {
		v, _ := ParseVersion(s)
		versions = append(versions, v)
	}
	SortVersionsDesc(versions)

	expected := []string{"3.0.0", "2.1.0", "2.0.0", "1.0.1", "1.0.0"}
	for i, v := range versions {
		if v.String() != expected[i] {
			t.Errorf("index %d: expected %q, got %q", i, expected[i], v.String())
		}
	}
}

func TestReleaseObjectName(t *testing.T) {
	tests := []struct {
		name         string
		templateName string
		version      string
		want         string
	}{
		{name: "basic", templateName: "my-template", version: "1.2.3", want: "my-template--v1-2-3"},
		{name: "zero version", templateName: "base", version: "0.0.0", want: "base--v0-0-0"},
		{name: "large numbers", templateName: "svc", version: "10.20.30", want: "svc--v10-20-30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := ParseVersion(tt.version)
			got := ReleaseObjectName(tt.templateName, v)
			if got != tt.want {
				t.Errorf("ReleaseObjectName(%q, %q) = %q, want %q", tt.templateName, tt.version, got, tt.want)
			}
		})
	}
}

func TestLatestMatchingVersion(t *testing.T) {
	mkVersions := func(strs ...string) []*semver.Version {
		var result []*semver.Version
		for _, s := range strs {
			v, _ := ParseVersion(s)
			result = append(result, v)
		}
		return result
	}

	tests := []struct {
		name       string
		versions   []*semver.Version
		constraint string
		want       string // empty means nil
	}{
		{
			name:       "nil constraint returns latest",
			versions:   mkVersions("1.0.0", "2.0.0", "1.5.0"),
			constraint: "",
			want:       "2.0.0",
		},
		{
			name:       "constraint filters to compatible",
			versions:   mkVersions("1.0.0", "2.0.0", "1.5.0", "1.9.0"),
			constraint: ">=1.0.0 <2.0.0",
			want:       "1.9.0",
		},
		{
			name:       "no matching version",
			versions:   mkVersions("3.0.0", "4.0.0"),
			constraint: ">=1.0.0 <2.0.0",
			want:       "",
		},
		{
			name:       "empty versions list",
			versions:   nil,
			constraint: "",
			want:       "",
		},
		{
			name:       "single matching version",
			versions:   mkVersions("1.2.3"),
			constraint: "^1.0.0",
			want:       "1.2.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := ParseConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("failed to parse constraint: %v", err)
			}
			got := LatestMatchingVersion(tt.versions, c)
			if tt.want == "" {
				if got != nil {
					t.Errorf("expected nil, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected %q, got nil", tt.want)
			}
			if got.String() != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got.String())
			}
		})
	}
}
