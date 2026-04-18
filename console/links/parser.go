// Package links parses external-link annotations off a Kubernetes resource
// (or, more generally, any annotation map) into normalised
// *consolev1.Link proto messages.
//
// The package is deliberately pure-Go and framework-agnostic: it accepts a
// `map[string]string` and returns proto structs. That shape lets the
// aggregator (HOL-574) call the parser uniformly whether the annotations
// come from a `*unstructured.Unstructured`, a `*corev1.ConfigMap`, or the
// cached JSON blob maintained on the deployment ConfigMap itself.
//
// Two annotation families are recognised, mirroring the parent plan
// HOL-550:
//
//   - `console.holos.run/external-link.<name>` — JSON body
//     {url, title, description}. Source = "holos". Name = <name>.
//   - `console.holos.run/primary-url` — JSON body {url, title, description}.
//     Returned as the `primary` result, not inside `links`. Source =
//     "holos". Name = "primary".
//   - `link.argocd.argoproj.io/<suffix>` — bare URL value. Source =
//     "argocd". Name = <suffix>. Title defaults to <suffix>.
//
// Malformed JSON, empty URLs, and URLs whose scheme is not http or https
// are skipped with a warning — the request that gathered these annotations
// must not fail just because one resource authored a bad link.
package links

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strings"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

const (
	// sourceHolos tags links discovered through
	// `console.holos.run/external-link.*` (and the companion primary-url
	// annotation).
	sourceHolos = "holos"
	// sourceArgoCD tags links discovered through
	// `link.argocd.argoproj.io/*`.
	sourceArgoCD = "argocd"
	// primaryName is the synthetic `Link.name` assigned to the primary-url
	// annotation. The primary annotation has no suffix, but surfacing a
	// stable non-empty name keeps downstream de-duplication (by name plus
	// source) straightforward.
	primaryName = "primary"
)

// linkValue is the JSON shape of a `console.holos.run/external-link.<name>`
// annotation value (and of the primary-url annotation). Kept unexported
// because callers interact with *consolev1.Link, not this intermediate
// representation.
type linkValue struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ParseAnnotations walks an annotation map and returns the external links
// surfaced for the corresponding Kubernetes resource plus, when the
// `console.holos.run/primary-url` annotation is present and valid, the
// primary link. `links` is sorted deterministically by (name, source) so
// callers can rely on a stable ordering without re-sorting.
//
// Malformed entries (non-JSON where JSON is required, empty URLs, or URLs
// using a scheme other than http/https) are dropped with a warning; they
// never abort the overall parse. This matches the parent plan's
// "best-effort aggregation" contract: a single badly authored annotation
// cannot break the deployment detail RPC.
func ParseAnnotations(ann map[string]string) (links []*consolev1.Link, primary *consolev1.Link) {
	if len(ann) == 0 {
		return nil, nil
	}

	for key, value := range ann {
		switch {
		case key == v1alpha2.AnnotationPrimaryURL:
			if parsed := parseHolosJSON(key, primaryName, value); parsed != nil {
				primary = parsed
			}
		case strings.HasPrefix(key, v1alpha2.AnnotationExternalLinkPrefix):
			name := strings.TrimPrefix(key, v1alpha2.AnnotationExternalLinkPrefix)
			if name == "" {
				// `console.holos.run/external-link.` with no suffix
				// is ambiguous — drop it rather than guess an
				// identity for the link.
				slog.Warn("skipping holos external-link annotation with empty suffix",
					slog.String("annotation", key))
				continue
			}
			if parsed := parseHolosJSON(key, name, value); parsed != nil {
				links = append(links, parsed)
			}
		case strings.HasPrefix(key, v1alpha2.AnnotationArgoCDLinkPrefix):
			name := strings.TrimPrefix(key, v1alpha2.AnnotationArgoCDLinkPrefix)
			if name == "" {
				// `link.argocd.argoproj.io/` with no suffix is
				// likewise ambiguous.
				slog.Warn("skipping argocd link annotation with empty suffix",
					slog.String("annotation", key))
				continue
			}
			if parsed := parseArgoCDBareURL(key, name, value); parsed != nil {
				links = append(links, parsed)
			}
		}
	}

	sort.Slice(links, func(i, j int) bool {
		if links[i].GetName() != links[j].GetName() {
			return links[i].GetName() < links[j].GetName()
		}
		return links[i].GetSource() < links[j].GetSource()
	})

	return links, primary
}

// parseHolosJSON decodes a holos-authored annotation value (JSON-encoded
// linkValue) into a *consolev1.Link. Returns nil when the value is
// malformed, the url is empty, or the url scheme is not http/https — each
// failure mode is logged so operators can trace misauthored annotations.
func parseHolosJSON(annotationKey, name, value string) *consolev1.Link {
	var v linkValue
	if err := json.Unmarshal([]byte(value), &v); err != nil {
		slog.Warn("skipping holos link annotation with malformed JSON",
			slog.String("annotation", annotationKey),
			slog.String("error", err.Error()))
		return nil
	}
	if v.URL == "" {
		slog.Warn("skipping holos link annotation with empty url",
			slog.String("annotation", annotationKey))
		return nil
	}
	if !isHTTPScheme(v.URL) {
		slog.Warn("skipping holos link annotation with non-http scheme",
			slog.String("annotation", annotationKey),
			slog.String("url", v.URL))
		return nil
	}
	title := v.Title
	if title == "" {
		// Fall back to the annotation suffix so the UI always has
		// something to render. Documented contract on Link.title in
		// proto/holos/console/v1/deployments.proto (HOL-572).
		title = name
	}
	return &consolev1.Link{
		Url:         v.URL,
		Title:       title,
		Description: v.Description,
		Source:      sourceHolos,
		Name:        name,
	}
}

// parseArgoCDBareURL turns an `link.argocd.argoproj.io/<suffix>: <url>`
// annotation into a *consolev1.Link. ArgoCD's convention stores the URL
// directly in the annotation value (no JSON envelope), so the suffix is
// the only source of human-readable naming available; it doubles as the
// link's `name` and fallback `title`.
func parseArgoCDBareURL(annotationKey, name, value string) *consolev1.Link {
	url := strings.TrimSpace(value)
	if url == "" {
		slog.Warn("skipping argocd link annotation with empty url",
			slog.String("annotation", annotationKey))
		return nil
	}
	if !isHTTPScheme(url) {
		slog.Warn("skipping argocd link annotation with non-http scheme",
			slog.String("annotation", annotationKey),
			slog.String("url", url))
		return nil
	}
	return &consolev1.Link{
		Url:         url,
		Title:       name,
		Description: "",
		Source:      sourceArgoCD,
		Name:        name,
	}
}

// isHTTPScheme reports whether rawURL looks like an http:// or https:// URL.
// A plain prefix check is sufficient for our validation budget here:
// ParseAnnotations is a gatekeeper against obviously hostile schemes
// (javascript:, file:, data:) from leaking into the UI, not a URL parser.
// Callers further downstream (the frontend anchor renderer) are expected
// to treat the value as untrusted and may apply their own sanitisation.
func isHTTPScheme(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://") || strings.HasPrefix(rawURL, "http://")
}
