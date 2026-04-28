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

package controller_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	controllerpkg "github.com/holos-run/holos-console/internal/controller"
	"github.com/holos-run/holos-console/internal/deploymentrender"
)

const deploymentControllerTemplate = `
	input: {
		name:  string
		image: string
		tag:   string
		env: [...#EnvVar] | *[]
	}

	platform: {
		project:   string
		namespace: string
		claims: email: string
	}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

_envSpec: [for e in input.env {
	name: e.name
	if e.value != _|_ {
		value: e.value
	}
	if e.secretKeyRef != _|_ {
		valueFrom: secretKeyRef: {
			name: e.secretKeyRef.name
			key:  e.secretKeyRef.key
		}
	}
	if e.configMapKeyRef != _|_ {
		valueFrom: configMapKeyRef: {
			name: e.configMapKeyRef.name
			key:  e.configMapKeyRef.key
		}
	}
}]

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
				metadata: {
					name:      input.name
					namespace: platform.namespace
					labels:    _labels
					annotations: "console.holos.run/deployer-email": platform.claims.email
				}
				spec: {
					selector: matchLabels: "app.kubernetes.io/name": input.name
					template: {
						metadata: labels: _labels
						spec: containers: [{
							name:  input.name
							image: input.image + ":" + input.tag
							if len(input.env) > 0 {
								env: _envSpec
							}
						}]
					}
				}
		}
	}
	clusterResources: {}
	}
	`

type recordingDriftRecorder struct {
	calls int
	refs  []*consolev1.LinkedTemplateRef
}

func (r *recordingDriftRecorder) RecordApplied(_ context.Context, _, _ string, refs []*consolev1.LinkedTemplateRef) error {
	r.calls++
	r.refs = refs
	return nil
}

type failingAncestorProvider struct{}

func (failingAncestorProvider) ListAncestorTemplateSources(context.Context, string, string) ([]string, []*consolev1.LinkedTemplateRef, error) {
	return nil, nil, fmt.Errorf("ancestor template missing: holos-org-platform/missing not found")
}

type staticAncestorProvider struct {
	refs []*consolev1.LinkedTemplateRef
}

func (p staticAncestorProvider) ListAncestorTemplateSources(context.Context, string, string) ([]string, []*consolev1.LinkedTemplateRef, error) {
	return nil, p.refs, nil
}

func waitForObjectAbsent(t *testing.T, c client.Client, key types.NamespacedName, obj client.Object) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		err := c.Get(context.Background(), key, obj)
		if apierrors.IsNotFound(err) {
			return
		}
		if err != nil {
			t.Fatalf("get %T %s: %v", obj, key, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%T %s still exists", obj, key)
}

func waitForDeploymentCondition(
	t *testing.T,
	c client.Client,
	key types.NamespacedName,
	condType string,
	want metav1.ConditionStatus,
) *deploymentsv1alpha1.Deployment {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	var last *metav1.Condition
	for time.Now().Before(deadline) {
		var obj deploymentsv1alpha1.Deployment
		if err := c.Get(context.Background(), key, &obj); err != nil {
			if !apierrors.IsNotFound(err) {
				t.Fatalf("get Deployment %s: %v", key, err)
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if cond := meta.FindStatusCondition(obj.Status.Conditions, condType); cond != nil {
			last = cond
			if cond.Status == want {
				return &obj
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if last == nil {
		t.Fatalf("condition %q never appeared on Deployment %s", condType, key)
	}
	t.Fatalf("condition %q on Deployment %s did not reach %s; last=%+v", condType, key, want, last)
	return nil
}

func startManagerWithDeploymentPipeline(
	t *testing.T,
	e *env,
	pipeline *deploymentrender.Pipeline,
	recorder controllerpkg.DeploymentPolicyDriftRecorder,
) (*controllerpkg.Manager, context.CancelFunc, <-chan error) {
	t.Helper()

	m, err := controllerpkg.NewManager(e.cfg, nil, controllerpkg.Options{
		CacheSyncTimeout:             30 * time.Second,
		SkipControllerNameValidation: true,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	m.ConfigureDeploymentReconciler(pipeline, recorder, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- m.Start(ctx)
	}()

	deadline := time.Now().Add(30 * time.Second)
	for !m.Ready() {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("manager did not become ready within deadline")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return m, cancel, errCh
}

func newDeploymentPipeline(t *testing.T, e *env) *deploymentrender.Pipeline {
	t.Helper()
	dyn, err := dynamic.NewForConfig(e.cfg)
	if err != nil {
		t.Fatalf("dynamic client: %v", err)
	}
	return deploymentrender.NewPipeline(e.client, staticProjectNamespaceResolver{}, &deploymentrender.CueRenderer{}, deploymentrender.NewApplier(dyn))
}

type staticProjectNamespaceResolver struct{}

func (staticProjectNamespaceResolver) ProjectNamespace(project string) string {
	return "holos-prj-" + project
}

func createDeploymentTemplate(t *testing.T, c client.Client, ns, name string) {
	t.Helper()
	tmpl := &templatesv1alpha1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Spec: templatesv1alpha1.TemplateSpec{
			Enabled:     true,
			CueTemplate: deploymentControllerTemplate,
		},
	}
	if err := c.Create(context.Background(), tmpl); err != nil {
		t.Fatalf("create Template: %v", err)
	}
}

func createDeploymentConfigMap(t *testing.T, c client.Client, ns, name string, env []v1alpha2.EnvVar, claims v1alpha2.Claims) {
	t.Helper()
	envJSON, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal env: %v", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Data: map[string]string{
			"env":    string(envJSON),
			"claims": string(claimsJSON),
		},
	}
	if err := c.Create(context.Background(), cm); err != nil {
		t.Fatalf("create deployment ConfigMap inputs: %v", err)
	}
}

func TestDeploymentReconciler_RenderApplyHappyPath(t *testing.T) {
	e := startEnv(t)
	recorder := &recordingDriftRecorder{}
	wantRefs := []*consolev1.LinkedTemplateRef{{Namespace: "holos-org-platform", Name: "baseline"}}
	pipeline := newDeploymentPipeline(t, e).WithAncestorTemplateProvider(staticAncestorProvider{refs: wantRefs})
	_, cancel, errCh := startManagerWithDeploymentPipeline(t, e, pipeline, recorder)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-deployment-reconcile"
	mustCreateNamespace(t, e.client, ns, "project")
	createDeploymentTemplate(t, e.client, ns, "httpbin")
	createDeploymentConfigMap(t, e.client, ns, "web",
		[]v1alpha2.EnvVar{
			{Name: "PLAIN", Value: "kept"},
			{Name: "FROM_SECRET", SecretKeyRef: &v1alpha2.KeyRef{Name: "app-secret", Key: "token"}},
		},
		v1alpha2.Claims{Sub: "user-1", Email: "alice@example.com"},
	)
	stale := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "web-stale",
			Labels: map[string]string{
				v1alpha2.LabelProject:         "deployment-reconcile",
				v1alpha2.AnnotationDeployment: "web",
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80}},
		},
	}
	if err := e.client.Create(context.Background(), stale); err != nil {
		t.Fatalf("create stale service: %v", err)
	}

	dep := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "web",
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "deployment-reconcile",
			Image:       "nginx",
			Tag:         "1.25",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: ns,
				Name:      "httpbin",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep); err != nil {
		t.Fatalf("create Deployment: %v", err)
	}

	got := waitForDeploymentCondition(
		t,
		e.client,
		client.ObjectKeyFromObject(dep),
		deploymentsv1alpha1.ConditionTypeApplied,
		metav1.ConditionTrue,
	)
	if got.Status.ObservedGeneration != got.Generation {
		t.Fatalf("observedGeneration=%d want %d", got.Status.ObservedGeneration, got.Generation)
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, deploymentsv1alpha1.ConditionTypeApplied)
	if cond == nil || cond.Reason != deploymentsv1alpha1.DeploymentReasonApplySucceeded {
		t.Fatalf("Applied condition=%+v, want reason %q", cond, deploymentsv1alpha1.DeploymentReasonApplySucceeded)
	}
	if recorder.calls != 1 {
		t.Fatalf("RecordApplied calls=%d want 1", recorder.calls)
	}
	if len(recorder.refs) != len(wantRefs) || recorder.refs[0].GetName() != wantRefs[0].GetName() {
		t.Fatalf("RecordApplied refs=%v want %v", recorder.refs, wantRefs)
	}

	var rendered appsv1.Deployment
	if err := e.client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "web"}, &rendered); err != nil {
		t.Fatalf("get rendered apps/v1 Deployment: %v", err)
	}
	if got := rendered.Annotations["console.holos.run/deployer-email"]; got != "alice@example.com" {
		t.Fatalf("deployer annotation=%q want alice@example.com", got)
	}
	containers := rendered.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("containers=%d want 1", len(containers))
	}
	if got := containers[0].Env; len(got) != 2 || got[0].Name != "PLAIN" || got[0].Value != "kept" || got[1].ValueFrom == nil || got[1].ValueFrom.SecretKeyRef == nil {
		t.Fatalf("rendered env=%+v, want literal and secret refs from ConfigMap input", got)
	}
	waitForObjectAbsent(t, e.client, types.NamespacedName{Namespace: ns, Name: "web-stale"}, &corev1.Service{})

	if err := e.client.Delete(context.Background(), got); err != nil {
		t.Fatalf("delete Deployment CR: %v", err)
	}
	waitForObjectAbsent(t, e.client, client.ObjectKeyFromObject(dep), &deploymentsv1alpha1.Deployment{})
	waitForObjectAbsent(t, e.client, types.NamespacedName{Namespace: ns, Name: "web"}, &appsv1.Deployment{})
}

func TestDeploymentReconciler_MissingAncestorTemplateSetsRenderedFalse(t *testing.T) {
	e := startEnv(t)
	pipeline := newDeploymentPipeline(t, e).
		WithAncestorTemplateProvider(failingAncestorProvider{}).
		WithStrictAncestorResolution()
	_, cancel, errCh := startManagerWithDeploymentPipeline(t, e, pipeline, nil)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-missing-ancestor"
	mustCreateNamespace(t, e.client, ns, "project")
	createDeploymentTemplate(t, e.client, ns, "httpbin")

	dep := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "web",
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "missing-ancestor",
			Image:       "nginx",
			Tag:         "1.25",
			TemplateRef: deploymentsv1alpha1.DeploymentTemplateRef{
				Namespace: ns,
				Name:      "httpbin",
			},
		},
	}
	if err := e.client.Create(context.Background(), dep); err != nil {
		t.Fatalf("create Deployment: %v", err)
	}

	got := waitForDeploymentCondition(
		t,
		e.client,
		client.ObjectKeyFromObject(dep),
		deploymentsv1alpha1.ConditionTypeRendered,
		metav1.ConditionFalse,
	)
	cond := meta.FindStatusCondition(got.Status.Conditions, deploymentsv1alpha1.ConditionTypeRendered)
	if cond == nil || cond.Reason != deploymentsv1alpha1.DeploymentReasonAncestorTemplateMissing {
		t.Fatalf("Rendered condition=%+v, want reason %q", cond, deploymentsv1alpha1.DeploymentReasonAncestorTemplateMissing)
	}

	var rendered appsv1.Deployment
	err := e.client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "web"}, &rendered)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("rendered apps/v1 Deployment err=%v, want NotFound", err)
	}
}
