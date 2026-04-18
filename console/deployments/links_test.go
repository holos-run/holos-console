package deployments

import (
	"context"
	"encoding/json"
	"testing"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// makeOwnedResource produces an unstructured Kubernetes resource carrying
// the deployment ownership labels and the supplied annotations. Used to
// drive the link aggregator across multi-resource scenarios.
func makeOwnedResource(kind, namespace, name string, annotations map[string]string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetAPIVersion("v1")
	u.SetKind(kind)
	u.SetNamespace(namespace)
	u.SetName(name)
	u.SetAnnotations(annotations)
	return u
}

// TestAggregateLinksFromResources covers the dedup, sort, and primary-url
// promotion contract for the new aggregator helper. The helper takes the
// raw owned-resource set and returns the wire-shape link list plus the
// promoted primary URL — the values the caller will write into the cache
// annotation and surface on `DeploymentOutput`.
func TestAggregateLinksFromResources(t *testing.T) {
	t.Parallel()

	t.Run("empty resources returns nil links and empty primary", func(t *testing.T) {
		links, primary := aggregateLinksFromResources(context.Background(), "p", "d", nil)
		if links != nil {
			t.Errorf("expected nil links, got %v", links)
		}
		if primary != "" {
			t.Errorf("expected empty primary, got %q", primary)
		}
	})

	t.Run("single resource with one holos link surfaces", func(t *testing.T) {
		r := makeOwnedResource("Deployment", "ns", "web", map[string]string{
			v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://logs.example.com","title":"Logs"}`,
		})
		links, primary := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r})
		if len(links) != 1 || links[0].GetUrl() != "https://logs.example.com" {
			t.Fatalf("unexpected links: %+v", links)
		}
		if primary != "" {
			t.Errorf("expected no primary, got %q", primary)
		}
	})

	t.Run("links across multiple resources are aggregated and sorted", func(t *testing.T) {
		r1 := makeOwnedResource("Deployment", "ns", "web", map[string]string{
			v1alpha2.AnnotationExternalLinkPrefix + "zeta": `{"url":"https://zeta.example.com","title":"Zeta"}`,
		})
		r2 := makeOwnedResource("Service", "ns", "web", map[string]string{
			v1alpha2.AnnotationExternalLinkPrefix + "alpha": `{"url":"https://alpha.example.com","title":"Alpha"}`,
		})
		links, _ := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r1, r2})
		if len(links) != 2 {
			t.Fatalf("expected 2 links, got %d", len(links))
		}
		// Sorted by name (alpha < zeta) regardless of resource order.
		if links[0].GetName() != "alpha" || links[1].GetName() != "zeta" {
			t.Errorf("expected alpha before zeta, got %q then %q", links[0].GetName(), links[1].GetName())
		}
	})

	t.Run("duplicate (name, source) across resources is deduplicated first-wins", func(t *testing.T) {
		r1 := makeOwnedResource("Deployment", "ns", "web", map[string]string{
			v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://first.example.com","title":"First"}`,
		})
		r2 := makeOwnedResource("Service", "ns", "web", map[string]string{
			v1alpha2.AnnotationExternalLinkPrefix + "logs": `{"url":"https://second.example.com","title":"Second"}`,
		})
		links, _ := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r1, r2})
		if len(links) != 1 {
			t.Fatalf("expected 1 dedup'd link, got %d", len(links))
		}
		if links[0].GetUrl() != "https://first.example.com" {
			t.Errorf("expected first-wins, got %q", links[0].GetUrl())
		}
	})

	t.Run("primary-url annotation is promoted to primary", func(t *testing.T) {
		r := makeOwnedResource("Service", "ns", "web", map[string]string{
			v1alpha2.AnnotationPrimaryURL: `{"url":"https://app.example.com","title":"App"}`,
		})
		_, primary := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r})
		if primary != "https://app.example.com" {
			t.Errorf("primary: got %q, want %q", primary, "https://app.example.com")
		}
	})

	t.Run("multiple primary annotations: first wins", func(t *testing.T) {
		r1 := makeOwnedResource("Deployment", "ns", "first", map[string]string{
			v1alpha2.AnnotationPrimaryURL: `{"url":"https://first.example.com","title":"First"}`,
		})
		r2 := makeOwnedResource("Service", "ns", "second", map[string]string{
			v1alpha2.AnnotationPrimaryURL: `{"url":"https://second.example.com","title":"Second"}`,
		})
		_, primary := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r1, r2})
		// The first resource walked in slice order wins.
		if primary != "https://first.example.com" {
			t.Errorf("primary: got %q, want %q (first wins)", primary, "https://first.example.com")
		}
	})

	t.Run("argocd link surfaces with source=argocd", func(t *testing.T) {
		r := makeOwnedResource("Deployment", "ns", "web", map[string]string{
			v1alpha2.AnnotationArgoCDLinkPrefix + "grafana": "https://grafana.example.com",
		})
		links, _ := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r})
		if len(links) != 1 {
			t.Fatalf("expected 1 link, got %d", len(links))
		}
		if links[0].GetSource() != "argocd" {
			t.Errorf("source: got %q, want %q", links[0].GetSource(), "argocd")
		}
	})

	t.Run("resources without annotations are skipped without panic", func(t *testing.T) {
		r := makeOwnedResource("Deployment", "ns", "bare", nil)
		links, primary := aggregateLinksFromResources(context.Background(), "p", "d", []unstructured.Unstructured{r})
		if links != nil || primary != "" {
			t.Errorf("expected empty result for no-annotation resource, got links=%v primary=%q", links, primary)
		}
	})
}

// TestSerializeAndDeserializeAggregatedLinks asserts the JSON shape stored
// on the `console.holos.run/links` annotation round-trips through the
// public API: write produces an empty payload when there is nothing to
// cache, read returns nil/empty when the annotation is missing, and a
// non-empty cache survives a serialize/deserialize cycle byte-equivalent.
func TestSerializeAndDeserializeAggregatedLinks(t *testing.T) {
	t.Parallel()

	t.Run("empty aggregation returns empty payload", func(t *testing.T) {
		got := serializeAggregatedLinks(context.Background(), "p", "d", nil, "")
		if got != "" {
			t.Errorf("expected empty payload for empty aggregation, got %q", got)
		}
	})

	t.Run("non-empty aggregation round-trips", func(t *testing.T) {
		links := []*consolev1.Link{
			{Url: "https://logs.example.com", Title: "Logs", Source: "holos", Name: "logs"},
		}
		payload := serializeAggregatedLinks(context.Background(), "p", "d", links, "https://app.example.com")
		if payload == "" {
			t.Fatal("expected non-empty payload")
		}
		// Deserialize via a real ConfigMap so the function under test
		// uses its annotation-key contract.
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{v1alpha2.AnnotationAggregatedLinks: payload}},
		}
		gotLinks, gotPrimary := deserializeAggregatedLinks(cm)
		if len(gotLinks) != 1 || gotLinks[0].GetUrl() != "https://logs.example.com" {
			t.Errorf("links round-trip mismatch: %+v", gotLinks)
		}
		if gotPrimary != "https://app.example.com" {
			t.Errorf("primary round-trip: got %q, want %q", gotPrimary, "https://app.example.com")
		}
	})

	t.Run("missing annotation returns nil/empty", func(t *testing.T) {
		links, primary := deserializeAggregatedLinks(&corev1.ConfigMap{})
		if links != nil || primary != "" {
			t.Errorf("expected nil/empty for missing annotation, got %+v / %q", links, primary)
		}
	})

	t.Run("malformed payload is dropped without panic", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{v1alpha2.AnnotationAggregatedLinks: `{`}},
		}
		links, primary := deserializeAggregatedLinks(cm)
		if links != nil || primary != "" {
			t.Errorf("expected nil/empty for malformed payload, got %+v / %q", links, primary)
		}
	})

	t.Run("payload shape matches the documented JSON keys", func(t *testing.T) {
		links := []*consolev1.Link{{Url: "https://x.example.com", Name: "x", Source: "holos"}}
		payload := serializeAggregatedLinks(context.Background(), "p", "d", links, "https://primary.example.com")
		var raw map[string]any
		if err := json.Unmarshal([]byte(payload), &raw); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if _, ok := raw["links"]; !ok {
			t.Errorf("expected 'links' key in cache payload, got keys: %v", keys(raw))
		}
		if raw["primary_url"] != "https://primary.example.com" {
			t.Errorf("primary_url key: got %v", raw["primary_url"])
		}
	})
}

// TestApplyAggregatedLinks asserts the merge contract used by both
// ListDeployments and GetDeployment: empty inputs are no-ops, links
// populate `Output.Links`, primary URL promotes into `Output.Url`, and an
// existing Url is overwritten by a non-empty primary because the
// `primary-url` annotation is a deliberate authoring override.
func TestApplyAggregatedLinks(t *testing.T) {
	t.Parallel()

	t.Run("nil summary is a no-op", func(t *testing.T) {
		applyAggregatedLinks(nil, []*consolev1.Link{{Url: "x"}}, "y")
		// reaching here without a panic is the assertion
	})

	t.Run("empty inputs leave summary unchanged", func(t *testing.T) {
		s := &consolev1.DeploymentStatusSummary{}
		applyAggregatedLinks(s, nil, "")
		if s.Output != nil {
			t.Errorf("expected Output to remain nil, got %+v", s.Output)
		}
	})

	t.Run("links populate Output.Links", func(t *testing.T) {
		s := &consolev1.DeploymentStatusSummary{}
		applyAggregatedLinks(s, []*consolev1.Link{{Url: "https://l.example.com", Name: "l"}}, "")
		if s.Output == nil || len(s.Output.Links) != 1 {
			t.Fatalf("expected Output.Links populated, got %+v", s.Output)
		}
	})

	t.Run("primary URL overrides existing Output.Url", func(t *testing.T) {
		s := &consolev1.DeploymentStatusSummary{Output: &consolev1.DeploymentOutput{Url: "https://old.example.com"}}
		applyAggregatedLinks(s, nil, "https://primary.example.com")
		if s.Output.Url != "https://primary.example.com" {
			t.Errorf("expected primary to override, got %q", s.Output.Url)
		}
	})

	t.Run("empty primary preserves existing Output.Url", func(t *testing.T) {
		s := &consolev1.DeploymentStatusSummary{Output: &consolev1.DeploymentOutput{Url: "https://old.example.com"}}
		applyAggregatedLinks(s, []*consolev1.Link{{Url: "https://l.example.com", Name: "l"}}, "")
		if s.Output.Url != "https://old.example.com" {
			t.Errorf("expected existing URL to survive, got %q", s.Output.Url)
		}
	})

	t.Run("empty aggregated clears previously-set Links on shared summary", func(t *testing.T) {
		// Regression for HOL-574 review round 3 P1: callers that
		// reuse a *DeploymentStatusSummary across requests (test
		// fakes, future cache implementations) MUST observe a fresh
		// empty link set instead of a stale list lingering from a
		// prior call.
		s := &consolev1.DeploymentStatusSummary{
			Output: &consolev1.DeploymentOutput{
				Url:   "https://output-url.example.com",
				Links: []*consolev1.Link{{Url: "https://stale.example.com", Name: "stale"}},
			},
		}
		applyAggregatedLinks(s, nil, "")
		if len(s.Output.Links) != 0 {
			t.Errorf("expected stale Links cleared, got %+v", s.Output.Links)
		}
		// Existing Url preserved (legacy OutputURLAnnotation source).
		if s.Output.Url != "https://output-url.example.com" {
			t.Errorf("expected legacy Url preserved, got %q", s.Output.Url)
		}
	})
}

// TestLinksEqual exercises the comparison used by the GetDeployment
// refresh loop to decide whether the cached annotation matches a fresh
// scan — drift between the two triggers a cache rewrite.
func TestLinksEqual(t *testing.T) {
	t.Parallel()

	a := []*consolev1.Link{{Url: "u", Title: "t", Source: "holos", Name: "n"}}
	b := []*consolev1.Link{{Url: "u", Title: "t", Source: "holos", Name: "n"}}
	if !linksEqual(a, b) {
		t.Error("identical link slices should compare equal")
	}

	c := []*consolev1.Link{{Url: "u", Title: "different", Source: "holos", Name: "n"}}
	if linksEqual(a, c) {
		t.Error("title-different slices should compare unequal")
	}

	if linksEqual(a, nil) {
		t.Error("non-empty vs nil should compare unequal")
	}
	if !linksEqual(nil, nil) {
		t.Error("two nil slices should compare equal")
	}
}

// keys returns the sorted key set of m for stable test output.
func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
