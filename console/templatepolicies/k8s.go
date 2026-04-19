// Package templatepolicies — K8sClient storage layer.
//
// HOL-662 rewrote this file to type the TemplatePolicy CRUD surface against
// the templates.holos.run/v1alpha1 TemplatePolicy CRD and read/write through
// a controller-runtime client.Client. Reads hit the informer cache the
// controller manager populates; writes fall through to the API server and
// the cache observes them on the next watch event.
//
// Signature shape: every method takes a Kubernetes namespace and a resource
// name. The namespace is the authoritative identifier per HOL-619; callers
// that still think in terms of (scope, scopeName) compute the namespace via
// the package-level resolver shim in the handler.
//
// ProjectNamespaceError is gone — the CEL ValidatingAdmissionPolicy shipped
// alongside the CRDs (HOL-618) rejects creation in a project-labelled
// namespace at admission time, so the handler's extractPolicyScope is the
// only defense-in-depth guard the client-side code needs to keep.
package templatepolicies

import (
	"context"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
	"github.com/holos-run/holos-console/console/scopeshim"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// K8sClient wraps TemplatePolicy CRUD operations against the CRD.
//
// Reads hit the controller-runtime cache — the informer keeps one watch
// against the cluster and serves every List/Get out of local memory, so
// ListPolicies does not pay a round-trip per call. Writes fall through to
// the API server; the cache learns about them on the next watch event.
type K8sClient struct {
	client   ctrlclient.Client
	Resolver *resolver.Resolver
}

// NewK8sClient returns a K8sClient bound to a controller-runtime client.Client
// and a resolver. Production wiring passes the cache-backed client from the
// embedded controller manager; tests may pass a fake ctrlclient or a direct
// envtest-backed client.
func NewK8sClient(client ctrlclient.Client, r *resolver.Resolver) *K8sClient {
	return &K8sClient{client: client, Resolver: r}
}

// ListPolicies returns every TemplatePolicy in the given namespace.
func (k *K8sClient) ListPolicies(ctx context.Context, namespace string) ([]templatesv1alpha1.TemplatePolicy, error) {
	slog.DebugContext(ctx, "listing template policies from kubernetes",
		slog.String("namespace", namespace),
	)
	var list templatesv1alpha1.TemplatePolicyList
	if err := k.client.List(ctx, &list, ctrlclient.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("listing template policies in %q: %w", namespace, err)
	}
	return list.Items, nil
}

// ListPoliciesInNamespace is a pointer-slice adapter over ListPolicies used
// by the policyresolver ancestor walker (HOL-567). HOL-622 converted the
// signature to a pointer slice so the resolver can pass each cached CRD
// pointer through without an index-address dance; this helper rewraps the
// value slice the controller-runtime List returns.
//
// ListPolicies itself retains the value-slice signature because the local
// templatepolicies handler converts entries to proto via an index-addressed
// loop that benefits from slice locality. The policyresolver consumers
// always want pointers.
func (k *K8sClient) ListPoliciesInNamespace(ctx context.Context, namespace string) ([]*templatesv1alpha1.TemplatePolicy, error) {
	if namespace == "" {
		return nil, fmt.Errorf("namespace is required")
	}
	items, err := k.ListPolicies(ctx, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]*templatesv1alpha1.TemplatePolicy, 0, len(items))
	for i := range items {
		out = append(out, &items[i])
	}
	return out, nil
}

// GetPolicy retrieves a single TemplatePolicy by name.
func (k *K8sClient) GetPolicy(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplatePolicy, error) {
	slog.DebugContext(ctx, "getting template policy from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	var p templatesv1alpha1.TemplatePolicy
	if err := k.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePolicy creates a new TemplatePolicy CRD. Rules are stored as
// structured spec fields on the CRD directly; HOL-618 removed the JSON
// annotation wire format. creatorEmail is not persisted at the CRD level —
// the handler audits it separately.
func (k *K8sClient) CreatePolicy(
	ctx context.Context,
	namespace, name, displayName, description, creatorEmail string,
	rules []*consolev1.TemplatePolicyRule,
) (*templatesv1alpha1.TemplatePolicy, error) {
	slog.DebugContext(ctx, "creating template policy in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	p := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeTemplatePolicy,
			},
			Annotations: map[string]string{
				v1alpha2.AnnotationCreatorEmail: creatorEmail,
			},
		},
		Spec: templatesv1alpha1.TemplatePolicySpec{
			DisplayName: displayName,
			Description: description,
			Rules:       protoRulesToCRD(rules),
		},
	}
	if err := k.client.Create(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// UpdatePolicy mutates the addressable spec fields of an existing
// TemplatePolicy. displayName/description follow nil-for-"leave alone"
// semantics; rules are always replaced when updateRules is true.
func (k *K8sClient) UpdatePolicy(
	ctx context.Context,
	namespace, name string,
	displayName, description *string,
	rules []*consolev1.TemplatePolicyRule,
	updateRules bool,
) (*templatesv1alpha1.TemplatePolicy, error) {
	slog.DebugContext(ctx, "updating template policy in kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	p, err := k.GetPolicy(ctx, namespace, name)
	if err != nil {
		return nil, fmt.Errorf("getting template policy for update: %w", err)
	}
	if displayName != nil {
		p.Spec.DisplayName = *displayName
	}
	if description != nil {
		p.Spec.Description = *description
	}
	if updateRules {
		p.Spec.Rules = protoRulesToCRD(rules)
	}
	if err := k.client.Update(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// DeletePolicy deletes a TemplatePolicy by name.
func (k *K8sClient) DeletePolicy(ctx context.Context, namespace, name string) error {
	slog.DebugContext(ctx, "deleting template policy from kubernetes",
		slog.String("namespace", namespace),
		slog.String("name", name),
	)
	p := &templatesv1alpha1.TemplatePolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	return k.client.Delete(ctx, p)
}

// protoRulesToCRD converts proto rules into the CRD spec shape. Kind values
// are mapped from the proto's TEMPLATE_POLICY_KIND_{REQUIRE,EXCLUDE} enums
// to the CRD's Require / Exclude TemplatePolicyKind strings.
func protoRulesToCRD(rules []*consolev1.TemplatePolicyRule) []templatesv1alpha1.TemplatePolicyRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]templatesv1alpha1.TemplatePolicyRule, 0, len(rules))
	for _, r := range rules {
		if r == nil {
			continue
		}
		tmpl := r.GetTemplate()
		rule := templatesv1alpha1.TemplatePolicyRule{
			Kind: protoKindToCRD(r.GetKind()),
		}
		if tmpl != nil {
			rule.Template = templatesv1alpha1.LinkedTemplateRef{
				Scope:             templateScopeLabel(scopeshim.RefScope(tmpl)),
				ScopeName:         scopeshim.RefScopeName(tmpl),
				Name:              tmpl.GetName(),
				VersionConstraint: tmpl.GetVersionConstraint(),
			}
		}
		out = append(out, rule)
	}
	return out
}

// CRDRulesToProto converts CRD spec rules back into their proto representation.
// Exported because ancestor_policies.go imports this package via a typed
// interface — HOL-663 may fold this into a policyresolver helper.
func CRDRulesToProto(rules []templatesv1alpha1.TemplatePolicyRule) []*consolev1.TemplatePolicyRule {
	if len(rules) == 0 {
		return nil
	}
	out := make([]*consolev1.TemplatePolicyRule, 0, len(rules))
	for i := range rules {
		r := &rules[i]
		rule := &consolev1.TemplatePolicyRule{
			Kind: crdKindToProto(r.Kind),
			Template: scopeshim.NewLinkedTemplateRef(
				scopeFromTemplateLabel(r.Template.Scope),
				r.Template.ScopeName,
				r.Template.Name,
				r.Template.VersionConstraint,
			),
		}
		out = append(out, rule)
	}
	return out
}

// protoKindToCRD maps the proto enum to the CRD's string kind.
func protoKindToCRD(k consolev1.TemplatePolicyKind) templatesv1alpha1.TemplatePolicyKind {
	switch k {
	case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE:
		return templatesv1alpha1.TemplatePolicyKindRequire
	case consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE:
		return templatesv1alpha1.TemplatePolicyKindExclude
	default:
		return ""
	}
}

// crdKindToProto is the inverse of protoKindToCRD.
func crdKindToProto(k templatesv1alpha1.TemplatePolicyKind) consolev1.TemplatePolicyKind {
	switch k {
	case templatesv1alpha1.TemplatePolicyKindRequire:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_REQUIRE
	case templatesv1alpha1.TemplatePolicyKindExclude:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_EXCLUDE
	default:
		return consolev1.TemplatePolicyKind_TEMPLATE_POLICY_KIND_UNSPECIFIED
	}
}

// templateScopeLabel mirrors templatepolicies.templateScopeLabel but lives
// here so this package does not import console/templates (avoiding a
// dependency cycle with the render-time resolver wiring in HOL-567).
func templateScopeLabel(scope scopeshim.Scope) string {
	switch scope {
	case scopeshim.ScopeOrganization:
		return v1alpha2.TemplateScopeOrganization
	case scopeshim.ScopeFolder:
		return v1alpha2.TemplateScopeFolder
	case scopeshim.ScopeProject:
		return v1alpha2.TemplateScopeProject
	default:
		return ""
	}
}

// scopeFromTemplateLabel is the inverse of templateScopeLabel.
func scopeFromTemplateLabel(label string) scopeshim.Scope {
	switch label {
	case v1alpha2.TemplateScopeOrganization:
		return scopeshim.ScopeOrganization
	case v1alpha2.TemplateScopeFolder:
		return scopeshim.ScopeFolder
	case v1alpha2.TemplateScopeProject:
		return scopeshim.ScopeProject
	default:
		return scopeshim.ScopeUnspecified
	}
}
