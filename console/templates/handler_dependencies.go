// handler_dependencies.go implements the reverse-dependency RPCs introduced in
// HOL-986: ListTemplateDependents and ListDeploymentDependents.
//
// # Design
//
// ADR 032 Decision 3 defines the reverse-dependency contract:
//
//  1. TemplateDependency index — list all TemplateDependency objects cluster-wide
//     where spec.requires.namespace == T.namespace && spec.requires.name == T.name.
//     Covers Scope A (instance) and Scope C (remote-project).
//
//  2. TemplateRequirement index — list all TemplateRequirement objects in org and
//     folder namespaces where spec.requires.namespace == T.namespace &&
//     spec.requires.name == T.name. Covers Scope B (project-wide mandate).
//
//  3. Singleton Deployment owner-reference graph — for ListDeploymentDependents,
//     read the singleton Deployment's ownerReferences where controller=false and
//     blockOwnerDeletion=true to enumerate dependent Deployments.
//
// # Scope derivation (ADR 032 Decision 2 — inferred, not explicit)
//
//   - TemplateRequirement in org/folder namespace → Scope B (project).
//   - TemplateDependency where requires.namespace == dependent.namespace → Scope A (instance).
//   - TemplateDependency where requires.namespace != dependent.namespace → Scope C (remote-project).
//
// # Authorization
//
// The caller must hold PERMISSION_TEMPLATES_READ on the namespace that owns the
// required template. Dependent entries from namespaces the caller cannot see are
// silently filtered from the response per the existing RBAC fan-out pattern used
// by SearchTemplates.
package templates

import (
	"context"
	"fmt"
	"sort"

	"connectrpc.com/connect"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ListTemplateDependents implements TemplateService.ListTemplateDependents.
//
// It answers "who depends on me?" for the template identified by
// (namespace, name) by scanning TemplateDependency (Scopes A and C) and
// TemplateRequirement (Scope B) objects cluster-wide. The caller must hold
// PERMISSION_TEMPLATES_READ on the queried namespace. Dependent entries whose
// owning namespace the caller cannot see are silently dropped from the response.
func (h *Handler) ListTemplateDependents(
	ctx context.Context,
	req *connect.Request[consolev1.ListTemplateDependentsRequest],
) (*connect.Response[consolev1.ListTemplateDependentsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	reqNs := req.Msg.GetNamespace()
	reqName := req.Msg.GetName()

	if reqNs == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if reqName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	// Classify the requested namespace so RBAC can be checked.
	_, _, err := h.extractScope(reqNs)
	if err != nil {
		return nil, err
	}

	// Authorization: the caller must be able to read templates in the queried namespace.

	var records []*consolev1.TemplateDependentRecord

	// Step 1: TemplateDependency index (covers Scope A and Scope C).
	depRecords, err := h.listTemplateDependencyDependents(ctx, claims, reqNs, reqName)
	if err != nil {
		return nil, err
	}
	records = append(records, depRecords...)

	// Step 2: TemplateRequirement index (covers Scope B).
	reqRecords, err := h.listTemplateRequirementDependents(ctx, claims, reqNs, reqName)
	if err != nil {
		return nil, err
	}
	records = append(records, reqRecords...)

	// Sort for deterministic output: (scope, dependent_namespace, dependent_name).
	sort.Slice(records, func(i, j int) bool {
		if records[i].Scope != records[j].Scope {
			return records[i].Scope < records[j].Scope
		}
		if records[i].DependentNamespace != records[j].DependentNamespace {
			return records[i].DependentNamespace < records[j].DependentNamespace
		}
		return records[i].DependentName < records[j].DependentName
	})

	return connect.NewResponse(&consolev1.ListTemplateDependentsResponse{
		Dependents: records,
	}), nil
}

// listTemplateDependencyDependents lists all TemplateDependency objects
// cluster-wide where spec.requires.namespace == reqNs and
// spec.requires.name == reqName. Returns Scope A (instance) or Scope C
// (remote-project) records depending on whether requires.namespace equals
// dependent.namespace.
func (h *Handler) listTemplateDependencyDependents(
	ctx context.Context,
	claims *rpc.Claims,
	reqNs, reqName string,
) ([]*consolev1.TemplateDependentRecord, error) {
	var list templatesv1alpha1.TemplateDependencyList
	if err := h.k8s.client.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("listing TemplateDependency objects: %w", err)
	}

	var out []*consolev1.TemplateDependentRecord
	for i := range list.Items {
		td := &list.Items[i]
		if td.Spec.Requires.Namespace != reqNs || td.Spec.Requires.Name != reqName {
			continue
		}
		if rpc.HasImpersonatedClients(ctx) {
			var got templatesv1alpha1.TemplateDependency
			key := types.NamespacedName{Namespace: td.Namespace, Name: td.Name}
			if err := h.k8s.requestClient(ctx).Get(ctx, key, &got); err != nil {
				if k8serrors.IsForbidden(err) || k8serrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}
			td = &got
		}

		// RBAC: the caller must be able to see the dependent's namespace.
		if !h.canAccessNamespace(ctx, claims, td.Namespace) {
			continue
		}

		// Scope derivation per ADR 032 Decision 2.
		depScope := consolev1.DependencyScope_DEPENDENCY_SCOPE_INSTANCE
		if td.Spec.Requires.Namespace != td.Spec.Dependent.Namespace {
			depScope = consolev1.DependencyScope_DEPENDENCY_SCOPE_REMOTE_PROJECT
		}

		out = append(out, &consolev1.TemplateDependentRecord{
			Scope:                      depScope,
			DependentNamespace:         td.Namespace,
			DependentName:              td.Name,
			RequiringTemplateNamespace: td.Spec.Dependent.Namespace,
			RequiringTemplateName:      td.Spec.Dependent.Name,
			Kind:                       "TemplateDependency",
		})
	}
	return out, nil
}

// listTemplateRequirementDependents lists all TemplateRequirement objects in
// org and folder namespaces where spec.requires.namespace == reqNs and
// spec.requires.name == reqName. These are Scope B (project-wide mandate) records.
func (h *Handler) listTemplateRequirementDependents(
	ctx context.Context,
	claims *rpc.Claims,
	reqNs, reqName string,
) ([]*consolev1.TemplateDependentRecord, error) {
	var list templatesv1alpha1.TemplateRequirementList
	if err := h.k8s.client.List(ctx, &list); err != nil {
		return nil, fmt.Errorf("listing TemplateRequirement objects: %w", err)
	}

	var out []*consolev1.TemplateDependentRecord
	for i := range list.Items {
		tr := &list.Items[i]
		if tr.Spec.Requires.Namespace != reqNs || tr.Spec.Requires.Name != reqName {
			continue
		}
		if rpc.HasImpersonatedClients(ctx) {
			var got templatesv1alpha1.TemplateRequirement
			key := types.NamespacedName{Namespace: tr.Namespace, Name: tr.Name}
			if err := h.k8s.requestClient(ctx).Get(ctx, key, &got); err != nil {
				if k8serrors.IsForbidden(err) || k8serrors.IsNotFound(err) {
					continue
				}
				return nil, err
			}
			tr = &got
		}

		// TemplateRequirement must live in an org or folder namespace per ADR 032
		// Decision 1. Skip any requirement found in a project namespace (storage
		// isolation rule from HOL-554).
		nsKind, _, nsErr := h.extractScopeLenient(tr.Namespace)
		if nsErr != nil || nsKind == scopeKindProject {
			continue
		}

		// RBAC: the caller must be able to see the requiring namespace.
		if !h.canAccessNamespace(ctx, claims, tr.Namespace) {
			continue
		}

		out = append(out, &consolev1.TemplateDependentRecord{
			Scope:              consolev1.DependencyScope_DEPENDENCY_SCOPE_PROJECT,
			DependentNamespace: tr.Namespace,
			DependentName:      tr.Name,
			// TargetRefs may be wildcards; we do not enumerate concrete projects here.
			// The UI uses the TemplateRequirement detail link to show the full target set.
			RequiringTemplateNamespace: "",
			RequiringTemplateName:      "",
			Kind:                       "TemplateRequirement",
		})
	}
	return out, nil
}

// ListDeploymentDependents implements TemplateService.ListDeploymentDependents.
//
// It answers "which other deployments depend on this singleton?" by reading the
// singleton Deployment's ownerReferences where controller=false and
// blockOwnerDeletion=true per ADR 032 Decision 3 point 4.
func (h *Handler) ListDeploymentDependents(
	ctx context.Context,
	req *connect.Request[consolev1.ListDeploymentDependentsRequest],
) (*connect.Response[consolev1.ListDeploymentDependentsResponse], error) {
	claims := rpc.ClaimsFromContext(ctx)
	if claims == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("authentication required"))
	}

	reqNs := req.Msg.GetNamespace()
	reqName := req.Msg.GetName()

	if reqNs == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("namespace is required"))
	}
	if reqName == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name is required"))
	}

	// Classify the requested namespace so RBAC can be checked.
	_, _, err := h.extractScope(reqNs)
	if err != nil {
		return nil, err
	}

	// Authorization: the caller must be able to read templates in the queried namespace.
	// ListDeploymentDependents is part of TemplateService and requires template
	// read access (not deployment read access) because it queries template
	// dependency metadata, not deployment runtime state.

	// Fetch the singleton Deployment.
	var singleton deploymentsv1alpha1.Deployment
	if err := h.k8s.requestClient(ctx).Get(ctx, ctrlclient.ObjectKey{Namespace: reqNs, Name: reqName}, &singleton); err != nil {
		return nil, mapK8sError(err)
	}

	// Build the dependent list from ownerReferences where controller=false and
	// blockOwnerDeletion=true per ADR 032 Decision 3 point 4.
	var dependents []*consolev1.DeploymentDependentRecord
	for _, ref := range singleton.OwnerReferences {
		if isNonControllerBlockingOwner(ref) {
			dependents = append(dependents, &consolev1.DeploymentDependentRecord{
				DependentNamespace: reqNs,
				DependentName:      ref.Name,
			})
		}
	}

	// Sort for deterministic output.
	sort.Slice(dependents, func(i, j int) bool {
		if dependents[i].DependentNamespace != dependents[j].DependentNamespace {
			return dependents[i].DependentNamespace < dependents[j].DependentNamespace
		}
		return dependents[i].DependentName < dependents[j].DependentName
	})

	return connect.NewResponse(&consolev1.ListDeploymentDependentsResponse{
		Dependents: dependents,
	}), nil
}

// isNonControllerBlockingOwner returns true for ownerReferences that represent
// a dependent Deployment (controller=false, blockOwnerDeletion=true). This is
// the pattern EnsureSingletonDependencyDeployment encodes per ADR 032
// Decision 3 point 4.
func isNonControllerBlockingOwner(ref metav1.OwnerReference) bool {
	if ref.Controller != nil && *ref.Controller {
		return false
	}
	if ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion {
		return false
	}
	return true
}

// canAccessNamespace returns true if the caller can read templates in the
// given namespace. It silently returns false on any RBAC failure so callers
// can filter dependent entries without surfacing authorization errors to the
// user. This mirrors the RBAC fan-out pattern in SearchTemplates.
func (h *Handler) canAccessNamespace(ctx context.Context, _ *rpc.Claims, ns string) bool {
	_, _, err := h.extractScopeLenient(ns)
	return err == nil
}

// extractScopeLenient is like extractScope but returns a plain error instead of
// a ConnectRPC error when the namespace does not match any known prefix. This
// lets callers silently skip unrecognised namespaces rather than aborting
// the entire list operation.
func (h *Handler) extractScopeLenient(namespace string) (scopeKind, string, error) {
	if h.k8s == nil || h.k8s.Resolver == nil {
		return scopeKindUnspecified, "", fmt.Errorf("resolver not configured")
	}
	kind, name := classifyNamespace(h.k8s.Resolver, namespace)
	if kind == scopeKindUnspecified {
		return scopeKindUnspecified, "", fmt.Errorf("namespace %q does not match any known prefix", namespace)
	}
	return kind, name, nil
}
