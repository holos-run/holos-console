package deployments

import (
	"context"
	"testing"
)

// TestDeploymentOutputFromJSON verifies that the render-preview helper
// preserves both the primary `url` and the additive `links` list declared
// on a template's `output` section. This is the guardrail for HOL-572: the
// proto gained `repeated Link links = 2` but the helper must pass the value
// through on the render-preview path so older templates and newer
// links-aware templates both round-trip correctly.
func TestDeploymentOutputFromJSON(t *testing.T) {
	t.Parallel()

	ptr := func(s string) *string { return &s }

	tests := []struct {
		name       string
		input      *string
		wantNil    bool
		wantURL    string
		wantLinks  int
		checkLinks func(t *testing.T, out any)
	}{
		{
			name:    "nil pointer returns nil",
			input:   nil,
			wantNil: true,
		},
		{
			name:    "empty string returns nil",
			input:   ptr(""),
			wantNil: true,
		},
		{
			name:    "whitespace only returns nil",
			input:   ptr("   \n  "),
			wantNil: true,
		},
		{
			name:    "malformed JSON returns nil",
			input:   ptr("{not json"),
			wantNil: true,
		},
		{
			name:      "empty object returns empty DeploymentOutput",
			input:     ptr("{}"),
			wantURL:   "",
			wantLinks: 0,
		},
		{
			name:      "url only — legacy shape",
			input:     ptr(`{"url":"https://example.com"}`),
			wantURL:   "https://example.com",
			wantLinks: 0,
		},
		{
			name:      "url plus single link preserved",
			input:     ptr(`{"url":"https://example.com","links":[{"url":"https://logs.example.com","title":"Logs","description":"App logs","source":"holos","name":"logs"}]}`),
			wantURL:   "https://example.com",
			wantLinks: 1,
		},
		{
			name:      "links only, url empty",
			input:     ptr(`{"links":[{"url":"https://a.example.com","name":"a"},{"url":"https://b.example.com","name":"b"}]}`),
			wantURL:   "",
			wantLinks: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := deploymentOutputFromJSON(context.Background(), "proj", "name", tt.input)

			if tt.wantNil {
				if got != nil {
					t.Fatalf("expected nil, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected non-nil DeploymentOutput, got nil")
			}
			if got.GetUrl() != tt.wantURL {
				t.Errorf("url: got %q, want %q", got.GetUrl(), tt.wantURL)
			}
			if len(got.GetLinks()) != tt.wantLinks {
				t.Errorf("links count: got %d, want %d", len(got.GetLinks()), tt.wantLinks)
			}
		})
	}
}

// TestDeploymentOutputFromJSON_LinkFieldsRoundTrip verifies every Link field
// (url, title, description, source, name) survives the JSON → proto
// translation. This covers the HOL-572 acceptance criterion that all five
// Link fields are expressible and preserved on the wire.
func TestDeploymentOutputFromJSON_LinkFieldsRoundTrip(t *testing.T) {
	t.Parallel()

	raw := `{"url":"https://primary.example.com","links":[{"url":"https://logs.example.com","title":"Logs","description":"Application logs","source":"holos","name":"logs"},{"url":"https://argocd.example.com","title":"ArgoCD","description":"","source":"argocd","name":"argocd"}]}`
	out := deploymentOutputFromJSON(context.Background(), "proj", "name", &raw)
	if out == nil {
		t.Fatalf("expected non-nil DeploymentOutput")
	}
	if out.GetUrl() != "https://primary.example.com" {
		t.Errorf("url: got %q, want %q", out.GetUrl(), "https://primary.example.com")
	}
	if got, want := len(out.GetLinks()), 2; got != want {
		t.Fatalf("links count: got %d, want %d", got, want)
	}

	first := out.GetLinks()[0]
	if first.GetUrl() != "https://logs.example.com" {
		t.Errorf("links[0].url: got %q", first.GetUrl())
	}
	if first.GetTitle() != "Logs" {
		t.Errorf("links[0].title: got %q", first.GetTitle())
	}
	if first.GetDescription() != "Application logs" {
		t.Errorf("links[0].description: got %q", first.GetDescription())
	}
	if first.GetSource() != "holos" {
		t.Errorf("links[0].source: got %q", first.GetSource())
	}
	if first.GetName() != "logs" {
		t.Errorf("links[0].name: got %q", first.GetName())
	}

	second := out.GetLinks()[1]
	if second.GetSource() != "argocd" {
		t.Errorf("links[1].source: got %q", second.GetSource())
	}
	if second.GetName() != "argocd" {
		t.Errorf("links[1].name: got %q", second.GetName())
	}
}
