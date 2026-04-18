package links

import (
	"strings"
	"testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// TestParseAnnotations exercises the full acceptance criteria for HOL-573:
// the parser must accept a raw annotation map, drop values that cannot be
// interpreted as Link proto messages, normalise source labels, sort the
// surviving entries deterministically, and separate the primary link from
// the rest of the collection. Table-driven coverage mirrors the ticket's
// AC bullets so regressions surface as a named subtest rather than a
// generic expectation failure.
func TestParseAnnotations(t *testing.T) {
	t.Parallel()

	type wantLink struct {
		url         string
		title       string
		description string
		source      string
		name        string
	}

	tests := []struct {
		name        string
		annotations map[string]string
		wantLinks   []wantLink
		wantPrimary *wantLink
	}{
		{
			name:        "nil map returns empty slice and nil primary",
			annotations: nil,
			wantLinks:   nil,
		},
		{
			name:        "empty map returns empty slice and nil primary",
			annotations: map[string]string{},
			wantLinks:   nil,
		},
		{
			name: "unrelated annotations are ignored",
			annotations: map[string]string{
				"example.com/something": "value",
				"app":                   "my-app",
			},
			wantLinks: nil,
		},
		{
			name: "single holos link",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com","title":"Logs","description":"App logs"}`,
			},
			wantLinks: []wantLink{
				{url: "https://logs.example.com", title: "Logs", description: "App logs", source: "holos", name: "logs"},
			},
		},
		{
			name: "multiple holos links sorted deterministically by name",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "zeta":  `{"url":"https://zeta.example.com","title":"Zeta"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "alpha": `{"url":"https://alpha.example.com","title":"Alpha"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "mid":   `{"url":"https://mid.example.com","title":"Mid"}`,
			},
			wantLinks: []wantLink{
				{url: "https://alpha.example.com", title: "Alpha", source: "holos", name: "alpha"},
				{url: "https://mid.example.com", title: "Mid", source: "holos", name: "mid"},
				{url: "https://zeta.example.com", title: "Zeta", source: "holos", name: "zeta"},
			},
		},
		{
			name: "argocd bare url picks up suffix as title",
			annotations: map[string]string{
				v1alpha2.AnnotationArgoCDLinkPrefix + "grafana": "https://grafana.example.com",
			},
			wantLinks: []wantLink{
				{url: "https://grafana.example.com", title: "grafana", source: "argocd", name: "grafana"},
			},
		},
		{
			name: "mixed holos and argocd links both surface, sorted by name then source",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "logs":  `{"url":"https://logs.example.com","title":"Logs"}`,
				v1alpha2.AnnotationArgoCDLinkPrefix + "grafana": "https://grafana.example.com",
				v1alpha2.AnnotationArgoCDLinkPrefix + "logs":    "https://argo-logs.example.com",
			},
			wantLinks: []wantLink{
				// name=grafana (argocd only)
				{url: "https://grafana.example.com", title: "grafana", source: "argocd", name: "grafana"},
				// name=logs, two sources, argocd sorts before holos
				{url: "https://argo-logs.example.com", title: "logs", source: "argocd", name: "logs"},
				{url: "https://logs.example.com", title: "Logs", source: "holos", name: "logs"},
			},
		},
		{
			name: "primary url annotation returned as primary, not in links",
			annotations: map[string]string{
				v1alpha2.AnnotationPrimaryURL:                  `{"url":"https://app.example.com","title":"App","description":"Main entrypoint"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com","title":"Logs"}`,
			},
			wantLinks: []wantLink{
				{url: "https://logs.example.com", title: "Logs", source: "holos", name: "logs"},
			},
			wantPrimary: &wantLink{
				url:         "https://app.example.com",
				title:       "App",
				description: "Main entrypoint",
				source:      "holos",
				name:        "primary",
			},
		},
		{
			name: "malformed holos JSON is skipped without aborting the others",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "broken": `{not json`,
				v1alpha2.AnnotationExternalLinkPrefix + "ok":     `{"url":"https://ok.example.com","title":"OK"}`,
			},
			wantLinks: []wantLink{
				{url: "https://ok.example.com", title: "OK", source: "holos", name: "ok"},
			},
		},
		{
			name: "malformed primary JSON is skipped, links still parse",
			annotations: map[string]string{
				v1alpha2.AnnotationPrimaryURL:                  `not-json`,
				v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com","title":"Logs"}`,
			},
			wantLinks: []wantLink{
				{url: "https://logs.example.com", title: "Logs", source: "holos", name: "logs"},
			},
		},
		{
			name: "holos link with empty url is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "empty": `{"url":"","title":"Empty"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "ok":    `{"url":"https://ok.example.com","title":"OK"}`,
			},
			wantLinks: []wantLink{
				{url: "https://ok.example.com", title: "OK", source: "holos", name: "ok"},
			},
		},
		{
			name: "argocd link with empty value is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationArgoCDLinkPrefix + "blank": "",
				v1alpha2.AnnotationArgoCDLinkPrefix + "ok":    "https://ok.example.com",
			},
			wantLinks: []wantLink{
				{url: "https://ok.example.com", title: "ok", source: "argocd", name: "ok"},
			},
		},
		{
			name: "holos link with non-http scheme is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "ftp":  `{"url":"ftp://example.com","title":"FTP"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "file": `{"url":"file:///etc/passwd","title":"File"}`,
				v1alpha2.AnnotationExternalLinkPrefix + "ok":   `{"url":"https://ok.example.com","title":"OK"}`,
			},
			wantLinks: []wantLink{
				{url: "https://ok.example.com", title: "OK", source: "holos", name: "ok"},
			},
		},
		{
			name: "argocd link with non-http scheme is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationArgoCDLinkPrefix + "ftp": "ftp://example.com",
				v1alpha2.AnnotationArgoCDLinkPrefix + "ok":  "http://ok.example.com",
			},
			wantLinks: []wantLink{
				{url: "http://ok.example.com", title: "ok", source: "argocd", name: "ok"},
			},
		},
		{
			name: "primary with non-http scheme is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationPrimaryURL: `{"url":"javascript:alert(1)","title":"bad"}`,
			},
			wantLinks: nil,
		},
		{
			name: "primary with empty url is skipped",
			annotations: map[string]string{
				v1alpha2.AnnotationPrimaryURL: `{"url":"","title":"bad"}`,
			},
			wantLinks: nil,
		},
		{
			name: "holos prefix with empty suffix is ignored",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix: `{"url":"https://example.com"}`,
			},
			wantLinks: nil,
		},
		{
			name: "argocd prefix with empty suffix is ignored",
			annotations: map[string]string{
				v1alpha2.AnnotationArgoCDLinkPrefix: "https://example.com",
			},
			wantLinks: nil,
		},
		{
			name: "http scheme is accepted",
			annotations: map[string]string{
				v1alpha2.AnnotationExternalLinkPrefix + "plain": `{"url":"http://plain.example.com","title":"Plain"}`,
			},
			wantLinks: []wantLink{
				{url: "http://plain.example.com", title: "Plain", source: "holos", name: "plain"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotLinks, gotPrimary := ParseAnnotations(tt.annotations)

			if got, want := len(gotLinks), len(tt.wantLinks); got != want {
				t.Fatalf("links count: got %d, want %d (got %+v)", got, want, linksToString(gotLinks))
			}
			for i, want := range tt.wantLinks {
				got := gotLinks[i]
				if got.GetUrl() != want.url {
					t.Errorf("links[%d].url: got %q, want %q", i, got.GetUrl(), want.url)
				}
				if got.GetTitle() != want.title {
					t.Errorf("links[%d].title: got %q, want %q", i, got.GetTitle(), want.title)
				}
				if got.GetDescription() != want.description {
					t.Errorf("links[%d].description: got %q, want %q", i, got.GetDescription(), want.description)
				}
				if got.GetSource() != want.source {
					t.Errorf("links[%d].source: got %q, want %q", i, got.GetSource(), want.source)
				}
				if got.GetName() != want.name {
					t.Errorf("links[%d].name: got %q, want %q", i, got.GetName(), want.name)
				}
			}

			if tt.wantPrimary == nil {
				if gotPrimary != nil {
					t.Errorf("primary: got %+v, want nil", gotPrimary)
				}
				return
			}
			if gotPrimary == nil {
				t.Fatalf("primary: got nil, want %+v", tt.wantPrimary)
			}
			if gotPrimary.GetUrl() != tt.wantPrimary.url {
				t.Errorf("primary.url: got %q, want %q", gotPrimary.GetUrl(), tt.wantPrimary.url)
			}
			if gotPrimary.GetTitle() != tt.wantPrimary.title {
				t.Errorf("primary.title: got %q, want %q", gotPrimary.GetTitle(), tt.wantPrimary.title)
			}
			if gotPrimary.GetDescription() != tt.wantPrimary.description {
				t.Errorf("primary.description: got %q, want %q", gotPrimary.GetDescription(), tt.wantPrimary.description)
			}
			if gotPrimary.GetSource() != tt.wantPrimary.source {
				t.Errorf("primary.source: got %q, want %q", gotPrimary.GetSource(), tt.wantPrimary.source)
			}
			if gotPrimary.GetName() != tt.wantPrimary.name {
				t.Errorf("primary.name: got %q, want %q", gotPrimary.GetName(), tt.wantPrimary.name)
			}
		})
	}
}

// TestParseAnnotations_LargeValueDoesNotPanic guards against callers handing
// the parser a pathologically large annotation value (for example, a
// misconfigured template stuffing a megabyte of JSON into a single link
// entry). The parser is expected to drop unparseable input and keep going,
// not panic and take the RPC handler with it.
func TestParseAnnotations_LargeValueDoesNotPanic(t *testing.T) {
	t.Parallel()

	// 1 MiB of filler; far larger than any realistic annotation but well
	// within a process's reachable heap. We intentionally exceed the
	// default Kubernetes 256 KiB annotation cap because the parser is a
	// pure Go function with no such cap of its own — if a caller manages
	// to feed it something huge, we want graceful degradation, not a
	// crash.
	huge := strings.Repeat("x", 1<<20)

	annotations := map[string]string{
		v1alpha2.AnnotationExternalLinkPrefix + "huge": huge,
		v1alpha2.AnnotationExternalLinkPrefix + "ok":   `{"url":"https://ok.example.com","title":"OK"}`,
		v1alpha2.AnnotationPrimaryURL:                  huge,
	}

	links, primary := ParseAnnotations(annotations)

	if len(links) != 1 {
		t.Fatalf("links count: got %d, want 1", len(links))
	}
	if links[0].GetName() != "ok" {
		t.Errorf("links[0].name: got %q, want %q", links[0].GetName(), "ok")
	}
	if primary != nil {
		t.Errorf("primary: got %+v, want nil (huge value must not parse as JSON)", primary)
	}
}

// TestParseAnnotations_HolosFallbackTitle verifies that a holos link whose
// JSON body omits `title` falls back to the annotation suffix — matching
// the contract of `Link.title` in the proto (see HOL-572), which documents
// the suffix as the fallback when the authoring annotation omitted a
// title.
func TestParseAnnotations_HolosFallbackTitle(t *testing.T) {
	t.Parallel()

	annotations := map[string]string{
		v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com"}`,
	}
	links, _ := ParseAnnotations(annotations)
	if len(links) != 1 {
		t.Fatalf("links count: got %d, want 1", len(links))
	}
	if got, want := links[0].GetTitle(), "logs"; got != want {
		t.Errorf("title fallback: got %q, want %q", got, want)
	}
}

// linksToString renders a slice of links in a stable, compact form so a
// failing subtest can show the full slice without drowning the reader in
// proto Stringer output.
func linksToString(links []*consolev1.Link) string {
	var b strings.Builder
	b.WriteString("[")
	for i, l := range links {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(l.GetName())
		b.WriteString("/")
		b.WriteString(l.GetSource())
		b.WriteString("=")
		b.WriteString(l.GetUrl())
	}
	b.WriteString("]")
	return b.String()
}
