package deployments

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/links"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// aggregatedLinksCache is the on-disk JSON shape of the
// `console.holos.run/links` annotation written to the deployment ConfigMap
// by the Create/UpdateDeployment write paths. It carries both the de-
// duplicated link set and the promoted primary URL so a single annotation
// reload restores everything ListDeployments / GetDeployment need to fill
// the wire DeploymentOutput. The shape is internal to the handler — the
// authoritative links still live on the resources themselves and are
// re-derivable on every GetDeployment refresh.
type aggregatedLinksCache struct {
	Links      []*consolev1.Link `json:"links,omitempty"`
	PrimaryURL string            `json:"primary_url,omitempty"`
}

// aggregateLinksFromResources walks every owned Kubernetes resource for a
// deployment, runs the phase-2 link annotation parser against each, and
// returns the merged result.
//
// De-duplication: links are keyed by (name, source). The first occurrence
// of a given key wins — subsequent duplicates are dropped silently. This
// keeps the wire shape stable when the same link annotation is stamped on
// more than one owned resource (for example a Deployment + its Service
// both carrying `console.holos.run/external-link.logs`).
//
// Primary promotion: if any owned resource carries the
// `console.holos.run/primary-url` annotation, the first one wins (in scan
// order — which is non-deterministic across kinds, hence the warning) and
// its URL becomes primaryURL. Subsequent primary annotations log a warn
// so operators can spot the conflict; they are otherwise ignored. An empty
// primaryURL means no resource published a primary URL.
//
// The returned link list is sorted deterministically by (name, source) so
// callers can compare two aggregations byte-for-byte to detect drift
// between the cached annotation and a fresh scan.
func aggregateLinksFromResources(ctx context.Context, project, deployment string, resources []unstructured.Unstructured) (aggregated []*consolev1.Link, primaryURL string) {
	type dedupKey struct {
		name   string
		source string
	}
	seen := make(map[dedupKey]struct{})
	var primarySource string

	for i := range resources {
		r := &resources[i]
		ann := r.GetAnnotations()
		if len(ann) == 0 {
			continue
		}
		parsedLinks, parsedPrimary := links.ParseAnnotations(ann)
		for _, l := range parsedLinks {
			k := dedupKey{name: l.GetName(), source: l.GetSource()}
			if _, ok := seen[k]; ok {
				// Duplicate across resources — drop without
				// noise. The contract is that annotations on
				// any resource may surface, so the same logical
				// link appearing twice is expected (Service +
				// Ingress, etc.).
				continue
			}
			seen[k] = struct{}{}
			aggregated = append(aggregated, l)
		}
		if parsedPrimary != nil && parsedPrimary.GetUrl() != "" {
			origin := r.GetKind() + "/" + r.GetName()
			if primaryURL == "" {
				primaryURL = parsedPrimary.GetUrl()
				primarySource = origin
			} else {
				slog.WarnContext(ctx, "multiple primary-url annotations found for deployment; keeping first",
					slog.String("project", project),
					slog.String("deployment", deployment),
					slog.String("kept", primarySource),
					slog.String("ignored", origin),
					slog.String("ignored_url", parsedPrimary.GetUrl()),
				)
			}
		}
	}

	sort.Slice(aggregated, func(i, j int) bool {
		if aggregated[i].GetName() != aggregated[j].GetName() {
			return aggregated[i].GetName() < aggregated[j].GetName()
		}
		return aggregated[i].GetSource() < aggregated[j].GetSource()
	})
	return aggregated, primaryURL
}

// serializeAggregatedLinks produces the JSON payload stored on the
// deployment ConfigMap as `console.holos.run/links`. Returns an empty
// string when both the link list and the primary URL are empty so the
// caller can distinguish "have something to cache" from "clear the
// annotation" without re-checking field-by-field. A marshal failure
// returns the empty string with a warning rather than failing the whole
// write path because the cache is best-effort by design.
func serializeAggregatedLinks(ctx context.Context, project, deployment string, aggregated []*consolev1.Link, primaryURL string) string {
	if len(aggregated) == 0 && primaryURL == "" {
		return ""
	}
	payload, err := json.Marshal(aggregatedLinksCache{Links: aggregated, PrimaryURL: primaryURL})
	if err != nil {
		slog.WarnContext(ctx, "failed to marshal aggregated links cache",
			slog.String("project", project),
			slog.String("deployment", deployment),
			slog.Any("error", err),
		)
		return ""
	}
	return string(payload)
}

// deserializeAggregatedLinks reads the cached `console.holos.run/links`
// annotation off a deployment ConfigMap. Returns (nil, "") on a missing
// annotation, on an empty payload, or on a parse error — the cache is
// authoritative-at-time-of-render but never load-bearing: a malformed blob
// just means the next write will overwrite it. A parse error is logged at
// warn so misauthored caches are visible to operators.
func deserializeAggregatedLinks(cm *corev1.ConfigMap) ([]*consolev1.Link, string) {
	if cm == nil || cm.Annotations == nil {
		return nil, ""
	}
	raw := cm.Annotations[v1alpha2.AnnotationAggregatedLinks]
	if raw == "" {
		return nil, ""
	}
	var cache aggregatedLinksCache
	if err := json.Unmarshal([]byte(raw), &cache); err != nil {
		slog.Warn("failed to unmarshal aggregated links cache",
			slog.String("name", cm.Name),
			slog.String("namespace", cm.Namespace),
			slog.Any("error", err),
		)
		return nil, ""
	}
	return cache.Links, cache.PrimaryURL
}

// applyAggregatedLinks mirrors mergeOutputURLAnnotation but for the link
// aggregator: it populates summary.Output.Links from the aggregated set and
// promotes a non-empty primary URL into summary.Output.Url. The aggregated
// list is the authoritative source for `Output.Links` — if it is empty
// (a deployment that never had link annotations or one whose annotations
// were just removed) the field is reset to nil so a previously-served
// link list cannot leak across requests when the caller reuses a
// `DeploymentStatusSummary` pointer (HOL-574 review round 3 P1).
//
// URL precedence: a non-empty primaryURL ALWAYS wins over whatever Url
// the prior caller (e.g. mergeOutputURLAnnotation) set, because the
// `primary-url` annotation is a deliberate per-resource override.
// An empty primaryURL preserves the existing Url so the legacy
// `OutputURLAnnotation` cache continues to work for templates that have
// not adopted `primary-url`. Callers that need to clear a stale
// promoted URL must do so before invoking this helper (the legacy URL
// has multiple sources, so a per-call clear here would wipe legitimate
// `OutputURLAnnotation` content).
//
// If no Output exists yet on the summary, one is allocated only when
// there is something to populate; a nil summary or no aggregated links
// AND no primaryURL on a summary with no existing Output is a no-op so
// callers do not have to special-case the "nothing to do" path.
func applyAggregatedLinks(summary *consolev1.DeploymentStatusSummary, aggregated []*consolev1.Link, primaryURL string) {
	if summary == nil {
		return
	}
	// Nothing to write and nothing to clear — leave the summary
	// entirely untouched. This avoids allocating an empty Output
	// just to record "no links" for a deployment that never had any.
	if len(aggregated) == 0 && primaryURL == "" && summary.Output == nil {
		return
	}
	if summary.Output == nil {
		summary.Output = &consolev1.DeploymentOutput{}
	}
	// Links: aggregated is authoritative — assign even when empty so
	// a previously-promoted list does not persist across calls.
	summary.Output.Links = aggregated
	if primaryURL != "" {
		summary.Output.Url = primaryURL
	}
}

// linksEqual reports whether two link slices represent the same wire
// content. Links from the parser arrive sorted by (name, source) and the
// cache stores them in the same order, so the comparison is a straight
// element-by-element walk. Used by GetDeployment to decide whether a
// fresh scan agrees with the cached annotation; on disagreement the fresh
// result wins and the cache is updated.
func linksEqual(a, b []*consolev1.Link) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].GetUrl() != b[i].GetUrl() ||
			a[i].GetTitle() != b[i].GetTitle() ||
			a[i].GetDescription() != b[i].GetDescription() ||
			a[i].GetSource() != b[i].GetSource() ||
			a[i].GetName() != b[i].GetName() {
			return false
		}
	}
	return true
}
