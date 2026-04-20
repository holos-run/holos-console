/*
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	v1alpha1 "github.com/holos-run/holos-console/api/secrets/v1alpha1"
	envtesthelpers "github.com/holos-run/holos-console/internal/envtest"
)

// Each admission policy has at least one negative path (the scope-escape
// the policy was written to close) and one positive path (the legitimate
// request shape). Every rejected-error log line prints the server-side
// message so a CEL regression surfaces the exact guard that fired.

type secretsEnvtestSuite struct {
	env    *envtest.Environment
	client client.Client
}

// setupSecretsEnvTest boots an envtest API server with the four
// secrets.holos.run CRDs plus all nine ValidatingAdmissionPolicies
// under config/secret-injector/admission. Skips (not fails) when
// envtest binaries are not available so developers and CI jobs that do
// not pre-install envtest do not see a spurious failure; the skip path
// is intentionally noisy so a missing envtest setup is not mistaken for
// a passing test.
func setupSecretsEnvTest(t *testing.T) *secretsEnvtestSuite {
	t.Helper()

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if assets := envtesthelpers.DetectAssets(); assets != "" {
			t.Setenv("KUBEBUILDER_ASSETS", assets)
		} else {
			t.Skip("envtest binaries not found; set KUBEBUILDER_ASSETS or run `setup-envtest use` to download")
		}
	}

	repoRoot, err := envtesthelpers.FindRepoRoot()
	if err != nil {
		t.Fatalf("finding repo root: %v", err)
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(repoRoot, "config", "secret-injector", "crd")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("starting envtest: %v", err)
	}
	t.Cleanup(func() {
		if stopErr := env.Stop(); stopErr != nil {
			t.Logf("stopping envtest: %v", stopErr)
		}
	})

	if err := v1alpha1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("registering secrets v1alpha1 scheme: %v", err)
	}

	c, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		t.Fatalf("constructing controller-runtime client: %v", err)
	}

	ctx := context.Background()
	admissionDir := filepath.Join(repoRoot, "config", "secret-injector", "admission")
	if err := envtesthelpers.ApplyYAMLFilesInDir(ctx, c, admissionDir); err != nil {
		t.Fatalf("applying admission policies: %v", err)
	}

	// Block until every VAP this suite asserts against has landed in
	// the API server's policy cache. Envtest does not synchronise VAP
	// registration with Start(), so racing a Create ahead of the guard
	// produces flaky false-negative rejections that look like unrelated
	// bugs.
	for _, name := range []string{
		"credential-authn-type-apikey-only",
		"credential-upstreamref-same-namespace",
		"namespace-scope-label-immutable",
		"secretinjectionpolicy-authn-type-apikey-only",
		"secretinjectionpolicy-folder-or-org-only",
		"secretinjectionpolicybinding-folder-or-org-only",
		"secretinjectionpolicybinding-policyref-same-namespace-or-ancestor",
		"upstreamsecret-project-only",
		"upstreamsecret-valuetemplate-no-control-chars",
	} {
		envtesthelpers.WaitForAdmissionPolicy(t, ctx, c, name)
	}

	// Functional readiness probe. VAP + binding existence is necessary
	// but not sufficient: the API server still needs to compile the CEL
	// programs and wire them into the admission plugin. Issue a known-bad
	// namespace UPDATE and poll until the server actually rejects it —
	// only then has the policy cache observably activated. Without this,
	// the first few admission-denying tests race the compile step and
	// pass through, producing flaky "expected rejection, got nil".
	waitAdmissionActive(t, ctx, c)

	return &secretsEnvtestSuite{env: env, client: c}
}

// waitAdmissionActive creates a throwaway namespace labeled "project",
// then attempts to mutate the resource-type label. The namespace-scope
// -label-immutable policy rejects this; once we see the rejection, the
// API server has observably wired the admission plugin. Polls up to
// 30s, cleans up the probe namespace before returning.
func waitAdmissionActive(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	const probe = "holos-admission-probe"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   probe,
			Labels: map[string]string{"console.holos.run/resource-type": "project"},
		},
	}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("admission-probe create namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = c.Delete(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: probe}})
	})

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := &corev1.Namespace{}
		if err := c.Get(ctx, types.NamespacedName{Name: probe}, got); err != nil {
			t.Fatalf("admission-probe get: %v", err)
		}
		got.Labels["console.holos.run/resource-type"] = "folder"
		err := c.Update(ctx, got)
		if err != nil && (apierrors.IsInvalid(err) || apierrors.IsForbidden(err)) {
			return
		}
		// Not yet active — reset the label and retry.
		if err == nil {
			got.Labels["console.holos.run/resource-type"] = "project"
			_ = c.Update(ctx, got)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("admission policies did not become active within deadline")
}

// makeNamespace writes a namespace with the console.holos.run/resource-type
// label set to resourceType. Several admission policies key off that label;
// having a single builder keeps the per-test setup terse and uniform. Extra
// labels land via the labels map — the hierarchy (parent, organization)
// labels attach here in the cross-tenant policyRef tests.
func makeNamespace(t *testing.T, ctx context.Context, c client.Client, name, resourceType string, extra map[string]string) {
	t.Helper()
	labels := map[string]string{"console.holos.run/resource-type": resourceType}
	for k, v := range extra {
		labels[k] = v
	}
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
	if err := c.Create(ctx, ns); err != nil {
		t.Fatalf("creating namespace %q: %v", name, err)
	}
}

// checkAdmission asserts err is (a) non-nil and Invalid/Forbidden when
// wantRejected, or (b) nil when !wantRejected. On rejection it t.Logs
// the server message so the CEL branch that fired is visible in the
// test output — the AC calls this out explicitly as a debuggability
// requirement for policy regressions.
func checkAdmission(t *testing.T, err error, wantRejected bool) {
	t.Helper()
	if wantRejected {
		if err == nil {
			t.Fatalf("expected admission rejection, got nil")
		}
		if !apierrors.IsInvalid(err) && !apierrors.IsForbidden(err) {
			t.Fatalf("expected Invalid/Forbidden admission error, got %T: %v", err, err)
		}
		t.Logf("admission rejection (expected): %v", err)
		return
	}
	if err != nil {
		t.Fatalf("expected admission to accept, got %T: %v", err, err)
	}
}

// TestAdmission_SecretInjectionPolicy_FolderOrOrgOnly covers the
// secretinjectionpolicy-folder-or-org-only policy: a SecretInjectionPolicy
// must live in a namespace whose console.holos.run/resource-type label is
// NOT "project". The CEL expression short-circuits on missing/absent
// labels, so the two real-world failure modes (project-labeled namespace)
// and the success modes (folder- and org-labeled namespaces) are what
// this regression guards.
func TestAdmission_SecretInjectionPolicy_FolderOrOrgOnly(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		nsName       string
		nsType       string
		wantRejected bool
	}{
		{"project-rejected", "holos-prj-sip-rej", "project", true},
		{"folder-accepted", "holos-fld-sip-acc", "folder", false},
		{"organization-accepted", "holos-org-sip-acc", "organization", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			makeNamespace(t, ctx, s.client, tc.nsName, tc.nsType, nil)
			sip := &v1alpha1.SecretInjectionPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: tc.nsName},
				Spec: v1alpha1.SecretInjectionPolicySpec{
					Direction: v1alpha1.DirectionEgress,
					CallerAuth: v1alpha1.CallerAuth{
						Type: v1alpha1.AuthenticationTypeAPIKey,
					},
					UpstreamRef: v1alpha1.UpstreamRef{
						Scope:     v1alpha1.UpstreamScopeProject,
						ScopeName: "p1",
						Name:      "u1",
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, sip), tc.wantRejected)
		})
	}
}

// TestAdmission_SecretInjectionPolicyBinding_FolderOrOrgOnly covers the
// secretinjectionpolicybinding-folder-or-org-only policy: SIPB shares
// the folder/org scope restriction with SIP.
func TestAdmission_SecretInjectionPolicyBinding_FolderOrOrgOnly(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		nsName       string
		nsType       string
		wantRejected bool
	}{
		{"project-rejected", "holos-prj-sipb-rej", "project", true},
		{"folder-accepted", "holos-fld-sipb-acc", "folder", false},
		{"organization-accepted", "holos-org-sipb-acc", "organization", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			makeNamespace(t, ctx, s.client, tc.nsName, tc.nsType, nil)
			sipb := &v1alpha1.SecretInjectionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: tc.nsName},
				Spec: v1alpha1.SecretInjectionPolicyBindingSpec{
					PolicyRef: v1alpha1.PolicyRef{
						// Self-reference keeps the policyRef check
						// trivially satisfied; the other guard under
						// test here is purely the ns-scope label.
						Scope:     v1alpha1.PolicyRefScopeFolder,
						Namespace: tc.nsName,
						Name:      "p1",
					},
					TargetRefs: []v1alpha1.TargetRef{{
						Kind:      v1alpha1.TargetRefKindServiceAccount,
						Namespace: tc.nsName,
						Name:      "sa1",
					}},
				},
			}
			checkAdmission(t, s.client.Create(ctx, sipb), tc.wantRejected)
		})
	}
}

// TestAdmission_SecretInjectionPolicyBinding_PolicyRefSameNamespaceOrAncestor
// covers the secretinjectionpolicybinding-policyref-same-namespace-or-ancestor
// policy. The binding lives in a folder namespace; policyRef.namespace must
// be the binding's own namespace (covered), the value of the namespace's
// console.holos.run/parent label (covered), the value of
// console.holos.run/organization projected through the default "holos-org-"
// prefix (covered), or a cross-tenant namespace (rejected).
func TestAdmission_SecretInjectionPolicyBinding_PolicyRefScope(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	bindingNS := "holos-fld-policyref-scope"
	parentNS := "holos-fld-policyref-parent"
	orgShort := "acme"
	orgNS := "holos-org-" + orgShort
	crossTenantNS := "holos-fld-cross-tenant"

	// Pre-stage namespaces referenced by the accept-path subtests so the
	// hierarchy labels are in place before the test body runs.
	makeNamespace(t, ctx, s.client, parentNS, "folder", nil)
	makeNamespace(t, ctx, s.client, orgNS, "organization", nil)
	makeNamespace(t, ctx, s.client, crossTenantNS, "folder", nil)
	makeNamespace(t, ctx, s.client, bindingNS, "folder", map[string]string{
		"console.holos.run/parent":       parentNS,
		"console.holos.run/organization": orgShort,
	})

	tests := []struct {
		name            string
		policyNamespace string
		wantRejected    bool
	}{
		{"same-namespace-accepted", bindingNS, false},
		{"parent-accepted", parentNS, false},
		{"organization-accepted", orgNS, false},
		{"cross-tenant-rejected", crossTenantNS, true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sipb := &v1alpha1.SecretInjectionPolicyBinding{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "b" + string(rune('a'+i)),
					Namespace: bindingNS,
				},
				Spec: v1alpha1.SecretInjectionPolicyBindingSpec{
					PolicyRef: v1alpha1.PolicyRef{
						Scope:     v1alpha1.PolicyRefScopeFolder,
						Namespace: tc.policyNamespace,
						Name:      "p1",
					},
					TargetRefs: []v1alpha1.TargetRef{{
						Kind:      v1alpha1.TargetRefKindServiceAccount,
						Namespace: bindingNS,
						Name:      "sa1",
					}},
				},
			}
			checkAdmission(t, s.client.Create(ctx, sipb), tc.wantRejected)
		})
	}
}

// TestAdmission_UpstreamSecret_ProjectOnly covers the
// upstreamsecret-project-only policy: UpstreamSecret must live in a
// project-labeled namespace. The inverse of the SIP/SIPB folder/org
// restriction — folder or org namespaces reject the create.
func TestAdmission_UpstreamSecret_ProjectOnly(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	tests := []struct {
		name         string
		nsName       string
		nsType       string
		wantRejected bool
	}{
		{"project-accepted", "holos-prj-us-acc", "project", false},
		{"folder-rejected", "holos-fld-us-rej", "folder", true},
		{"organization-rejected", "holos-org-us-rej", "organization", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			makeNamespace(t, ctx, s.client, tc.nsName, tc.nsType, nil)
			us := &v1alpha1.UpstreamSecret{
				ObjectMeta: metav1.ObjectMeta{Name: "u", Namespace: tc.nsName},
				Spec: v1alpha1.UpstreamSecretSpec{
					SecretRef: v1alpha1.SecretKeyReference{Name: "src", Key: "k"},
					Upstream: v1alpha1.Upstream{
						Host:   "example.test",
						Scheme: "https",
					},
					Injection: v1alpha1.Injection{
						Header:        "Authorization",
						ValueTemplate: "Bearer {{.Value}}",
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, us), tc.wantRejected)
		})
	}
}

// TestAdmission_SecretInjectionPolicy_AuthnAPIKeyOnly covers the
// secretinjectionpolicy-authn-type-apikey-only policy: callerAuth.type
// must be APIKey in v1alpha1; OIDC is rejected until the M2 data-plane
// path ships.
func TestAdmission_SecretInjectionPolicy_AuthnAPIKeyOnly(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	nsName := "holos-fld-sip-authn"
	makeNamespace(t, ctx, s.client, nsName, "folder", nil)

	tests := []struct {
		name         string
		authnType    v1alpha1.AuthenticationType
		wantRejected bool
	}{
		{"apikey-accepted", v1alpha1.AuthenticationTypeAPIKey, false},
		{"oidc-rejected", v1alpha1.AuthenticationTypeOIDC, true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sip := &v1alpha1.SecretInjectionPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "p" + string(rune('a'+i)),
					Namespace: nsName,
				},
				Spec: v1alpha1.SecretInjectionPolicySpec{
					Direction:  v1alpha1.DirectionEgress,
					CallerAuth: v1alpha1.CallerAuth{Type: tc.authnType},
					UpstreamRef: v1alpha1.UpstreamRef{
						Scope:     v1alpha1.UpstreamScopeProject,
						ScopeName: "p1",
						Name:      "u1",
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, sip), tc.wantRejected)
		})
	}
}

// TestAdmission_Credential_UpstreamRefSameNamespace covers the
// credential-upstreamref-same-namespace policy: the optional
// upstreamSecretRef.namespace must be either empty or the Credential's
// own namespace — cross-namespace refs would cross the RBAC boundary
// the backing v1.Secret relies on.
func TestAdmission_Credential_UpstreamRefSameNamespace(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	nsName := "holos-prj-cred-upstream"
	otherNS := "holos-prj-cred-other"
	makeNamespace(t, ctx, s.client, nsName, "project", nil)
	makeNamespace(t, ctx, s.client, otherNS, "project", nil)

	tests := []struct {
		name         string
		refNamespace string
		wantRejected bool
	}{
		{"omitted-accepted", "", false},
		{"same-namespace-accepted", nsName, false},
		{"cross-namespace-rejected", otherNS, true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cred := &v1alpha1.Credential{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "c" + string(rune('a'+i)),
					Namespace: nsName,
				},
				Spec: v1alpha1.CredentialSpec{
					Authentication: v1alpha1.Authentication{
						Type:   v1alpha1.AuthenticationTypeAPIKey,
						APIKey: &v1alpha1.APIKeySettings{HeaderName: "X-API-Key"},
					},
					UpstreamSecretRef: v1alpha1.NamespacedSecretKeyReference{
						Namespace: tc.refNamespace,
						Name:      "src",
						Key:       "k",
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, cred), tc.wantRejected)
		})
	}
}

// TestAdmission_Credential_AuthnAPIKeyOnly covers the
// credential-authn-type-apikey-only policy.
func TestAdmission_Credential_AuthnAPIKeyOnly(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	nsName := "holos-prj-cred-authn"
	makeNamespace(t, ctx, s.client, nsName, "project", nil)

	tests := []struct {
		name         string
		authnType    v1alpha1.AuthenticationType
		wantRejected bool
	}{
		{"apikey-accepted", v1alpha1.AuthenticationTypeAPIKey, false},
		{"oidc-rejected", v1alpha1.AuthenticationTypeOIDC, true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cred := &v1alpha1.Credential{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "c" + string(rune('a'+i)),
					Namespace: nsName,
				},
				Spec: v1alpha1.CredentialSpec{
					Authentication: v1alpha1.Authentication{
						Type:   tc.authnType,
						APIKey: &v1alpha1.APIKeySettings{HeaderName: "X-API-Key"},
					},
					UpstreamSecretRef: v1alpha1.NamespacedSecretKeyReference{
						Name: "src",
						Key:  "k",
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, cred), tc.wantRejected)
		})
	}
}

// TestAdmission_UpstreamSecret_ValueTemplateNoControlChars covers the
// upstreamsecret-valuetemplate-no-control-chars policy. The valueTemplate
// must not contain control characters or a colon — either would give a
// malicious template author an HTTP header smuggling primitive.
func TestAdmission_UpstreamSecret_ValueTemplateNoControlChars(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	nsName := "holos-prj-us-template"
	makeNamespace(t, ctx, s.client, nsName, "project", nil)

	tests := []struct {
		name         string
		valueTpl     string
		wantRejected bool
	}{
		{"clean-accepted", "Bearer {{.Value}}", false},
		{"crlf-rejected", "Bearer {{.Value}}\r\nX-Evil: x", true},
		{"colon-rejected", "Bearer: {{.Value}}", true},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			us := &v1alpha1.UpstreamSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "u" + string(rune('a'+i)),
					Namespace: nsName,
				},
				Spec: v1alpha1.UpstreamSecretSpec{
					SecretRef: v1alpha1.SecretKeyReference{Name: "src", Key: "k"},
					Upstream: v1alpha1.Upstream{
						Host:   "example.test",
						Scheme: "https",
					},
					Injection: v1alpha1.Injection{
						Header:        "Authorization",
						ValueTemplate: tc.valueTpl,
					},
				},
			}
			checkAdmission(t, s.client.Create(ctx, us), tc.wantRejected)
		})
	}
}

// TestAdmission_NamespaceScopeLabelImmutable covers the
// namespace-scope-label-immutable policy. The test client runs as
// cluster admin (NOT the platform-controller SA), so every label
// transition is rejected; a same-label update is accepted. The
// platform-controller exemption path is not exercised here because
// envtest does not wire RBAC impersonation into the default client —
// the accept-on-unchanged-label case is the observable guarantee this
// test locks in.
func TestAdmission_NamespaceScopeLabelImmutable(t *testing.T) {
	s := setupSecretsEnvTest(t)
	ctx := context.Background()

	build := func(name, label string) *corev1.Namespace {
		labels := map[string]string{}
		if label != "" {
			labels["console.holos.run/resource-type"] = label
		}
		return &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		}
	}

	updateNamespace := func(t *testing.T, name string, mutate func(ns *corev1.Namespace)) error {
		t.Helper()
		got := &corev1.Namespace{}
		if err := s.client.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
			t.Fatalf("get namespace %q: %v", name, err)
		}
		mutate(got)
		return s.client.Update(ctx, got)
	}

	tests := []struct {
		name         string
		setup        func(t *testing.T) string // returns the namespace name
		mutate       func(ns *corev1.Namespace)
		wantRejected bool
	}{
		{
			name: "change-label-rejected",
			setup: func(t *testing.T) string {
				n := "holos-imm-change"
				if err := s.client.Create(ctx, build(n, "project")); err != nil {
					t.Fatalf("create: %v", err)
				}
				return n
			},
			mutate: func(ns *corev1.Namespace) {
				ns.Labels["console.holos.run/resource-type"] = "folder"
			},
			wantRejected: true,
		},
		{
			name: "set-label-from-unset-rejected",
			setup: func(t *testing.T) string {
				n := "holos-imm-set"
				if err := s.client.Create(ctx, build(n, "")); err != nil {
					t.Fatalf("create: %v", err)
				}
				return n
			},
			mutate: func(ns *corev1.Namespace) {
				if ns.Labels == nil {
					ns.Labels = map[string]string{}
				}
				ns.Labels["console.holos.run/resource-type"] = "project"
			},
			wantRejected: true,
		},
		{
			name: "unset-label-rejected",
			setup: func(t *testing.T) string {
				n := "holos-imm-unset"
				if err := s.client.Create(ctx, build(n, "project")); err != nil {
					t.Fatalf("create: %v", err)
				}
				return n
			},
			mutate: func(ns *corev1.Namespace) {
				delete(ns.Labels, "console.holos.run/resource-type")
			},
			wantRejected: true,
		},
		{
			name: "unchanged-label-accepted",
			setup: func(t *testing.T) string {
				n := "holos-imm-unchanged"
				if err := s.client.Create(ctx, build(n, "project")); err != nil {
					t.Fatalf("create: %v", err)
				}
				return n
			},
			mutate: func(ns *corev1.Namespace) {
				// Touch a different label so the UPDATE is meaningful.
				ns.Labels["unrelated.example.test/touched"] = "yes"
			},
			wantRejected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := tc.setup(t)
			checkAdmission(t, updateNamespace(t, name, tc.mutate), tc.wantRejected)
		})
	}
}
