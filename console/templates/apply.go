package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/deployments"
	"github.com/holos-run/holos-console/console/rpc"
	corev1 "k8s.io/api/core/v1"
)

// ResourceApplier applies K8s resources using each resource's own namespace.
type ResourceApplier interface {
	Apply(ctx context.Context, project, deploymentName string, resources []unstructured.Unstructured) error
}

// HierarchyWalker walks the namespace hierarchy for the mandatory template applier.
type HierarchyWalker interface {
	WalkAncestors(ctx context.Context, startNs string) ([]*corev1.Namespace, error)
}

// MandatoryTemplateApplier renders and applies every ancestor template that
// a TemplatePolicy REQUIRE rule pins onto the project at project-creation
// time. The name is retained for backwards compatibility with project
// wiring; the mechanism changed in HOL-557 — the driver is now policy
// evaluation, not the deprecated `mandatory` annotation. A REQUIRE rule
// with `project_pattern=*` is the policy-era equivalent of the old
// mandatory flag.
//
// `policyLookup` reports the REQUIRE-injected templates that apply to a
// project at creation time. The projects handler passes a function backed by
// console/policyresolver.Resolver so this package does not have to import
// policyresolver directly (preserving the existing import hierarchy).
type MandatoryTemplateApplier struct {
	k8s          *K8sClient
	walker       HierarchyWalker
	renderer     *deployments.CueRenderer
	applier      ResourceApplier
	policyLookup PolicyRequireLookup
}

// PolicyRequireLookup returns the list of (scope, scopeName, name) tuples
// the TemplatePolicy resolver marks as REQUIRED for the given project.
// project-scoped callers pass TargetKindProjectTemplate and a target name
// of "*" — the current resolver treats that as project-level for the purpose
// of matching project_pattern. A nil lookup means "no REQUIRE rules apply,"
// which is the correct behavior when policies are disabled (tests, missing
// K8s cluster).
type PolicyRequireLookup func(ctx context.Context, project string) ([]PolicyRequiredTemplate, error)

// PolicyRequiredTemplate names an ancestor template that a REQUIRE rule
// pins onto a project.
type PolicyRequiredTemplate struct {
	// ScopeNamespace is the Kubernetes namespace where the ancestor template
	// lives (folder or organization namespace). The applier reads the
	// template ConfigMap from this namespace.
	ScopeNamespace string
	// TemplateName is the DNS label slug of the template ConfigMap.
	TemplateName string
}

// NewMandatoryTemplateApplier creates a MandatoryTemplateApplier.
func NewMandatoryTemplateApplier(k8s *K8sClient, walker HierarchyWalker, renderer *deployments.CueRenderer, applier ResourceApplier) *MandatoryTemplateApplier {
	return &MandatoryTemplateApplier{k8s: k8s, walker: walker, renderer: renderer, applier: applier}
}

// WithPolicyRequireLookup attaches the lookup used to discover
// REQUIRE-injected templates at project creation time. Without this lookup
// the applier is a no-op: the old mandatory annotation is gone (HOL-557) so
// there is nothing else to apply.
func (a *MandatoryTemplateApplier) WithPolicyRequireLookup(lookup PolicyRequireLookup) *MandatoryTemplateApplier {
	a.policyLookup = lookup
	return a
}

// ApplyMandatoryOrgTemplates satisfies the projects.MandatoryTemplateApplier
// interface. It asks the TemplatePolicy resolver which ancestor templates a
// REQUIRE rule pins onto the newly-created project and applies each one to
// the project namespace.
//
// After HOL-557 the `mandatory` annotation is gone; REQUIRE rules are the
// only driver. When no PolicyRequireLookup is configured (tests, offline
// environments) the applier is a no-op — every template the project opts
// into must come through an explicit link.
//
// If any template render or apply fails, an error is returned describing
// which template failed. The caller (CreateProject) is responsible for
// cleanup.
func (a *MandatoryTemplateApplier) ApplyMandatoryOrgTemplates(ctx context.Context, org, project, projectNamespace string, claims *rpc.Claims) error {
	if a.policyLookup == nil {
		// Policy lookup not configured — every template a project gets must
		// come through an explicit link. The old mandatory annotation used
		// to provide auto-inclusion here; that path was removed in HOL-557.
		return nil
	}
	required, err := a.policyLookup(ctx, project)
	if err != nil {
		slog.WarnContext(ctx, "TemplatePolicy REQUIRE lookup failed; no templates auto-applied to project",
			slog.String("org", org),
			slog.String("project", project),
			slog.Any("error", err),
		)
		return nil
	}
	for _, entry := range required {
		if err := a.applyTemplateFromNamespace(ctx, entry.ScopeNamespace, entry.TemplateName, project, projectNamespace, claims); err != nil {
			return err
		}
	}
	return nil
}

// applyTemplateFromNamespace renders and applies a single ancestor template
// identified by (namespace, name). Mirrors the old per-template loop body
// but is driven by an explicit lookup rather than by scanning annotations.
func (a *MandatoryTemplateApplier) applyTemplateFromNamespace(ctx context.Context, ancestorNs, templateName, project, projectNamespace string, claims *rpc.Claims) error {
	cm, err := a.k8s.client.CoreV1().ConfigMaps(ancestorNs).Get(ctx, templateName, metav1.GetOptions{})
	if err != nil {
		slog.WarnContext(ctx, "REQUIRE-injected template not found, skipping",
			slog.String("namespace", ancestorNs),
			slog.String("template", templateName),
			slog.Any("error", err),
		)
		return nil
	}
	enabled, _ := strconv.ParseBool(cm.Annotations[v1alpha2.AnnotationEnabled])
	if !enabled {
		slog.WarnContext(ctx, "REQUIRE-injected template is disabled, skipping",
			slog.String("namespace", ancestorNs),
			slog.String("template", templateName),
		)
		return nil
	}
	cueSource := cm.Data[CueTemplateKey]
	if cueSource == "" {
		return nil
	}

	platformInput := v1alpha2.PlatformInput{
		Project:          project,
		Namespace:        projectNamespace,
		GatewayNamespace: deployments.DefaultGatewayNamespace,
	}
	if claims != nil {
		platformInput.Claims = v1alpha2.Claims{
			Iss:           claims.Iss,
			Sub:           claims.Sub,
			Exp:           claims.Exp,
			Iat:           claims.Iat,
			Email:         claims.Email,
			EmailVerified: claims.EmailVerified,
			Name:          claims.Name,
			Groups:        claims.Roles,
		}
	}
	platformJSON, err := json.Marshal(platformInput)
	if err != nil {
		return fmt.Errorf("encoding platform input for REQUIRE template %q in %q: %w", templateName, ancestorNs, err)
	}
	userJSON, err := json.Marshal(mandatoryTemplateProjectInput{})
	if err != nil {
		return fmt.Errorf("encoding user input for REQUIRE template %q in %q: %w", templateName, ancestorNs, err)
	}
	combinedCUE := fmt.Sprintf("platform: %s\ninput: %s", string(platformJSON), string(userJSON))

	resources, err := a.renderer.RenderWithCueInput(ctx, cueSource, combinedCUE)
	if err != nil {
		return fmt.Errorf("rendering REQUIRE template %q from %q for project %q: %w", templateName, ancestorNs, project, err)
	}
	if a.applier == nil {
		return nil
	}
	if err := a.applier.Apply(ctx, project, templateName, resources); err != nil {
		return fmt.Errorf("applying REQUIRE template %q from %q to project %q: %w", templateName, ancestorNs, project, err)
	}
	slog.InfoContext(ctx, "REQUIRE template applied",
		slog.String("ancestorNs", ancestorNs),
		slog.String("template", templateName),
		slog.String("project", project),
		slog.String("projectNamespace", projectNamespace),
		slog.Int("resources", len(resources)),
	)
	return nil
}

// mandatoryTemplateProjectInput carries the user-configurable input for
// REQUIRE-injected templates applied at project creation time. The fields
// must match the CUE #Input struct field names in the template.
type mandatoryTemplateProjectInput struct{}
