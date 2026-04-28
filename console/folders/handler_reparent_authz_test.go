// Tests for HOL-1067: parent-side owner authorization on UpdateFolder reparent.
//
// The reparent path checks Owner-level permission on both the source and the
// destination parent namespace whenever the request carries an impersonated
// client. Owner is proven via SSAR for `delete` on the namespace, matching
// requireNamespaceOwner. Tests stub the SSAR reactor so each axis can simulate
// allow/deny independently per parent namespace.
package folders

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	authzv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"

	"github.com/holos-run/holos-console/console/rpc"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// reparentAuthzCase captures one denial/allow scenario for the parent-side
// owner check on UpdateFolder reparent.
type reparentAuthzCase struct {
	name                string
	allowSrc            bool
	allowDest           bool
	wantPermissionError bool
}

// newReparentAuthzHandler builds a handler whose fake clientset answers SSARs
// for the `delete` verb on the source and destination parent namespaces with
// the supplied allow flags.
func newReparentAuthzHandler(t *testing.T, srcParentNs, destParentNs string, allowSrc, allowDest bool, namespaces ...*corev1.Namespace) (*Handler, context.Context) {
	t.Helper()
	objs := make([]runtime.Object, len(namespaces))
	for i, ns := range namespaces {
		objs[i] = ns
	}
	fakeClient := fake.NewClientset(objs...)
	fakeClient.PrependReactor("create", "selfsubjectaccessreviews", func(action clienttesting.Action) (bool, runtime.Object, error) {
		create, ok := action.(clienttesting.CreateAction)
		if !ok {
			return false, nil, nil
		}
		ssar := create.GetObject().(*authzv1.SelfSubjectAccessReview)
		attrs := ssar.Spec.ResourceAttributes
		if attrs == nil {
			ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: false}
			return true, ssar, nil
		}
		// Only the parent-owner gate uses verb=delete on namespaces. Any
		// other (verb,resource) combination defaults to deny so unrelated
		// SSAR call sites stay isolated from this test fixture.
		if attrs.Verb != "delete" || attrs.Resource != "namespaces" {
			ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: false}
			return true, ssar, nil
		}
		switch attrs.Name {
		case srcParentNs:
			ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: allowSrc}
		case destParentNs:
			ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: allowDest}
		default:
			ssar.Status = authzv1.SubjectAccessReviewStatus{Allowed: false}
		}
		return true, ssar, nil
	})
	k8s := NewK8sClient(fakeClient, testResolver())
	handler := NewHandler(k8s)
	ctx := contextWithClaims("alice@example.com")
	ctx = rpc.ContextWithImpersonatedClients(ctx, &rpc.ImpersonatedClients{Clientset: fakeClient})
	return handler, ctx
}

func TestUpdateFolder_Reparent_ImpersonatedOwnerAuthorization(t *testing.T) {
	cases := []reparentAuthzCase{
		{name: "deny on source only", allowSrc: false, allowDest: true, wantPermissionError: true},
		{name: "deny on destination only", allowSrc: true, allowDest: false, wantPermissionError: true},
		{name: "deny on both", allowSrc: false, allowDest: false, wantPermissionError: true},
		{name: "allow on both succeeds", allowSrc: true, allowDest: true, wantPermissionError: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Hierarchy:
			//   org acme
			//   ├── folder src-fld   (source parent — holds the moving folder)
			//   ├── folder dest-fld  (destination parent)
			//   └── folder moving    (initially under src-fld; reparented under dest-fld)
			orgNs := orgNSWithGrants("acme", `[{"principal":"alice@example.com","role":"owner"}]`)
			srcFolder := folderNSWithGrants("rp-authz-fld-src", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
			destFolder := folderNSWithGrants("rp-authz-fld-dest", "acme", "holos-org-acme", `[{"principal":"alice@example.com","role":"editor"}]`)
			moving := folderNSWithGrants("rp-authz-fld-mv", "acme", "holos-fld-rp-authz-fld-src", `[{"principal":"alice@example.com","role":"owner"}]`)

			handler, ctx := newReparentAuthzHandler(t,
				"holos-fld-rp-authz-fld-src",  // source parent ns
				"holos-fld-rp-authz-fld-dest", // destination parent ns
				tc.allowSrc, tc.allowDest,
				orgNs, srcFolder, destFolder, moving,
			)

			newParentType := consolev1.ParentType_PARENT_TYPE_FOLDER
			newParentName := "rp-authz-fld-dest"
			_, err := handler.UpdateFolder(ctx, connect.NewRequest(&consolev1.UpdateFolderRequest{
				Name:       "rp-authz-fld-mv",
				ParentType: &newParentType,
				ParentName: &newParentName,
			}))

			if tc.wantPermissionError {
				if err == nil {
					t.Fatalf("expected permission denied error, got nil")
				}
				connectErr, ok := err.(*connect.Error)
				if !ok {
					t.Fatalf("expected *connect.Error, got %T: %v", err, err)
				}
				if connectErr.Code() != connect.CodePermissionDenied {
					t.Fatalf("expected CodePermissionDenied, got %v: %v", connectErr.Code(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}
