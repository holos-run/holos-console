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

package projectnspipeline

import (
	"context"

	templatesv1alpha1 "github.com/holos-run/holos-console/api/templates/v1alpha1"
)

// NewPolicyGetterAdapter wraps a function that looks up a TemplatePolicy
// CRD by (namespace, name) and returns the decoded [Policy] the pipeline
// needs. Production wiring in console/console.go passes the
// templatepolicies K8sClient's GetPolicy method — the adapter projects
// the CRD into the pipeline's minimal [Policy] shape so
// console/projects/projectnspipeline does not import the full
// templatepolicies package (which would create an import cycle via
// policyresolver → ancestor_bindings → templatepolicies).
type PolicyCRDGetter interface {
	GetPolicy(ctx context.Context, namespace, name string) (*templatesv1alpha1.TemplatePolicy, error)
}

// NewPolicyGetterAdapter returns a PolicyGetter that dereferences the
// CRD and keeps only the REQUIRE-rule template refs. EXCLUDE rules are
// discarded per ADR 034 Decision 3 — a ProjectNamespace binding always
// adds templates; there is no baseline set to subtract from at project
// creation time. A nil getter yields an adapter whose GetPolicy returns
// (nil, nil) — the pipeline treats that as "no templates contributed"
// (the fail-open branch in collectTemplateSources).
func NewPolicyGetterAdapter(g PolicyCRDGetter) PolicyGetter {
	return &crdPolicyGetter{g: g}
}

type crdPolicyGetter struct {
	g PolicyCRDGetter
}

func (a *crdPolicyGetter) GetPolicy(ctx context.Context, namespace, name string) (*Policy, error) {
	if a == nil || a.g == nil {
		return nil, nil
	}
	p, err := a.g.GetPolicy(ctx, namespace, name)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	out := &Policy{
		Namespace: p.Namespace,
		Name:      p.Name,
	}
	for _, r := range p.Spec.Rules {
		if r.Kind != templatesv1alpha1.TemplatePolicyKindRequire {
			continue
		}
		ref := r.Template
		if ref.Name == "" {
			continue
		}
		out.TemplateRefs = append(out.TemplateRefs, TemplateRef{
			Namespace:         ref.Namespace,
			Name:              ref.Name,
			VersionConstraint: ref.VersionConstraint,
		})
	}
	return out, nil
}

// TemplateCRDGetter is the subset of the templates K8sClient the
// pipeline's TemplateGetter needs. Defined locally to avoid a direct
// dependency on console/templates — the CRD type is owned by
// api/templates/v1alpha1 so the projection stays small.
type TemplateCRDGetter interface {
	GetTemplate(ctx context.Context, namespace, name string) (*templatesv1alpha1.Template, error)
}

// NewTemplateGetterAdapter wraps a typed templates getter and returns a
// [TemplateGetter] that yields the Template's raw CUE source string.
// A nil getter yields an adapter whose GetTemplateSource returns
// ("", nil) — the pipeline then skips the ref, consistent with the
// resolver's fail-open contract.
func NewTemplateGetterAdapter(g TemplateCRDGetter) TemplateGetter {
	return &crdTemplateGetter{g: g}
}

type crdTemplateGetter struct {
	g TemplateCRDGetter
}

func (a *crdTemplateGetter) GetTemplateSource(ctx context.Context, namespace, name string) (string, error) {
	if a == nil || a.g == nil {
		return "", nil
	}
	t, err := a.g.GetTemplate(ctx, namespace, name)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", nil
	}
	return t.Spec.CueTemplate, nil
}
