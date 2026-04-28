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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymentsv1alpha1 "github.com/holos-run/holos-console/api/deployments/v1alpha1"
)

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
			if !errors.IsNotFound(err) {
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

func TestDeploymentReconciler_AcceptsDeployment(t *testing.T) {
	e := startEnv(t)
	_, cancel, errCh := startManager(t, e.cfg)
	t.Cleanup(func() { stopManager(t, cancel, errCh) })

	ns := "holos-prj-deployment-reconcile"
	mustCreateNamespace(t, e.client, ns, "project")

	dep := &deploymentsv1alpha1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ns,
			Name:      "web",
		},
		Spec: deploymentsv1alpha1.DeploymentSpec{
			ProjectName: "deployment-reconcile",
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
		deploymentsv1alpha1.ConditionTypeAccepted,
		metav1.ConditionTrue,
	)
	if got.Status.ObservedGeneration != got.Generation {
		t.Fatalf("observedGeneration=%d want %d", got.Status.ObservedGeneration, got.Generation)
	}
}
