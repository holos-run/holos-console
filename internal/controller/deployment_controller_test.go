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
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
	controllerpkg "github.com/holos-run/holos-console/internal/controller"
	"github.com/holos-run/holos-console/internal/deploymentrender"
)

const deploymentControllerTemplate = `
input: {
	name:  string
	image: string
	tag:   string
}

platform: {
	project:   string
	namespace: string
}

_labels: {
	"app.kubernetes.io/name":       input.name
	"app.kubernetes.io/managed-by": "console.holos.run"
}

projectResources: {
	namespacedResources: (platform.namespace): {
		Deployment: (input.name): {
			apiVersion: "apps/v1"
			kind:       "Deployment"
			metadata: {
				name:      input.name
				namespace: platform.namespace
				labels:    _labels
			}
			spec: {
				selector: matchLabels: "app.kubernetes.io/name": input.name
				template: {
					metadata: labels: _labels
					spec: containers: [{
						name:  input.name
						image: input.image + ":" + input.tag
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
