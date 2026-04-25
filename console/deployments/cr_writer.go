// Package deployments — CR dual-write layer.
//
// # Design: server-side apply with a unique field manager
//
// CRWriter mirrors every ConfigMap proto-store write to the
// deployments.holos.run/v1alpha1.Deployment CRD using server-side apply
// (SSA) with field manager "holos-console-deployment-writer".
//
// SSA is the right tool here because the dependency reconcilers introduced in
// Phases 5–6 (HOL-959, HOL-960) will write non-controller ownerReference
// entries onto the Deployment CR. A regular Update/Patch call would overwrite
// the metadata.ownerReferences field and destroy those entries; SSA with a
// distinct field manager lets each actor own only its own fields — the
// reconciler owns ownerReferences, the dual-writer owns spec.* — so the two
// paths compose without conflict.
//
// Lazy creation: if a Deployment already exists in the proto-store but its CR
// is absent, the next Create or Update call via this writer issues an SSA
// apply which creates the CR in-place (Apply is idempotent and create-or-update
// by definition). No migration step is required (ADR Decision 12).
//
// Delete propagation: DeleteCR deletes the CR via the typed client.Delete call.
// This is intentionally a hard delete rather than an SSA no-op: when the
// operator removes a deployment from the proto-store the corresponding CR
// should be garbage-collected immediately.
package deployments

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	"github.com/holos-run/holos-console/console/resolver"
)

const (
	// crFieldManager is the SSA field manager identity for the dual-writer.
	// It must be stable across calls so SSA can track owned fields across
	// subsequent apply cycles without conflict errors.
	crFieldManager = "holos-console-deployment-writer"
)

// CRWriter writes Deployment CRs alongside the existing ConfigMap proto-store.
// A nil CRWriter is valid; all methods become no-ops so local/dev wiring
// without a controller-runtime client remains unchanged.
type CRWriter struct {
	client   ctrlclient.Client
	resolver *resolver.Resolver
}

// NewCRWriter constructs a CRWriter. Both client and resolver are required;
// callers that cannot supply a controller-runtime client should leave the
// CRWriter field on K8sClient nil instead of calling this function.
func NewCRWriter(cl ctrlclient.Client, r *resolver.Resolver) *CRWriter {
	return &CRWriter{client: cl, resolver: r}
}

// applyDeploymentCR builds the desired Deployment CR state and applies it via
// SSA. The SSA field manager is crFieldManager; Force=true so this writer
// always wins on its own fields (spec.*) without conflicting with other field
// managers (e.g. dependency reconcilers owning ownerReferences).
//
// "Apply" is create-or-update: the call creates the CR when it does not exist
// yet (lazy creation path) and updates it when it does (update path).
//
// ownerReferences are intentionally absent from the SSA payload — SSA merges
// the desired state for each field manager in isolation, so omitting
// ownerReferences means the reconciler-written entries are never touched by
// this writer.
func (w *CRWriter) applyDeploymentCR(
	ctx context.Context,
	project, name, image, tag, templateName, displayName, description string,
	command, args []string,
	port int32,
) error {
	if w == nil {
		return nil
	}
	ns := w.resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "applying deployment CR via SSA",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)

	// Build the SSA payload as a partially-typed object. We set only the
	// fields owned by this field manager (spec.*) so SSA does not touch
	// ownerReferences, status, or any other field owned by another actor.
	desired := &deploymentsv1alpha1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: deploymentsv1alpha1.GroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
			Labels: map[string]string{
				v1alpha2.LabelManagedBy:    v1alpha2.ManagedByValue,
				v1alpha2.LabelResourceType: v1alpha2.ResourceTypeDeployment,
			},
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName:  project,
			DisplayName:  displayName,
			Description:  description,
			Image:        image,
			Tag:          tag,
			Command:      command,
			Args:         args,
			Port:         port,
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: ns,
				Name:      templateName,
			},
		},
	}

	// Encode as JSON for the SSA patch. Using raw JSON patch rather than
	// ctrlclient.Apply (the Apply helper in controller-runtime v0.14+ uses
	// the same underlying server-side apply path but works directly with
	// typed objects). We use the dynamic patch approach to stay consistent
	// with how other SSA patches are done in the codebase (projectapply,
	// deployments/apply.go).
	data, err := json.Marshal(desired)
	if err != nil {
		return fmt.Errorf("marshaling deployment CR for SSA: %w", err)
	}

	force := true
	return w.client.Patch(ctx, desired, ctrlclient.RawPatch(types.ApplyPatchType, data), &ctrlclient.PatchOptions{
		FieldManager: crFieldManager,
		Force:        &force,
	})
}

// ApplyOnCreate writes the Deployment CR after a successful proto-store create.
// Parameters mirror K8sClient.CreateDeployment. The env parameter is accepted
// for API symmetry with CreateDeployment but is intentionally not written to
// the CR: DeploymentSpec carries no Env field (env vars are proto-side only,
// stored in the ConfigMap and surfaced via the ConnectRPC surface).
func (w *CRWriter) ApplyOnCreate(
	ctx context.Context,
	project, name, image, tag, templateName, displayName, description string,
	command, args []string,
	_ []v1alpha2.EnvVar, // env — proto-store only; not reflected in DeploymentSpec
	port int32,
) error {
	if w == nil {
		return nil
	}
	return w.applyDeploymentCR(ctx, project, name, image, tag, templateName, displayName, description, command, args, port)
}

// ApplyOnUpdate writes the Deployment CR after a successful proto-store update.
// Callers pass the post-update ConfigMap values so the CR stays in sync with
// the proto-store.
func (w *CRWriter) ApplyOnUpdate(
	ctx context.Context,
	project, name, image, tag, templateName, displayName, description string,
	command, args []string,
	port int32,
) error {
	if w == nil {
		return nil
	}
	return w.applyDeploymentCR(ctx, project, name, image, tag, templateName, displayName, description, command, args, port)
}

// DeleteCR removes the Deployment CR when the proto-store record is deleted.
// A NotFound error is silenced — delete is idempotent (per the lazy-creation
// guarantee the CR may never have been written for old records). All other
// errors are returned to the caller.
func (w *CRWriter) DeleteCR(ctx context.Context, project, name string) error {
	if w == nil {
		return nil
	}
	ns := w.resolver.ProjectNamespace(project)
	slog.DebugContext(ctx, "deleting deployment CR",
		slog.String("project", project),
		slog.String("namespace", ns),
		slog.String("name", name),
	)
	obj := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
	}
	err := w.client.Delete(ctx, obj)
	if ctrlclient.IgnoreNotFound(err) != nil {
		return fmt.Errorf("deleting deployment CR: %w", err)
	}
	return nil
}
