package v1alpha1

import (
	"encoding/json"
	"testing"
)

// TestResourceSetJSONRoundTrip verifies that a fully-populated ResourceSet
// survives a JSON marshal/unmarshal cycle with all fields intact.
func TestResourceSetJSONRoundTrip(t *testing.T) {
	original := ResourceSet{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindResourceSet,
		},
		Metadata: Metadata{
			Name: "my-deployment",
			Annotations: map[string]string{
				AnnotationDisplayName: "My Deployment",
				AnnotationDescription: "A test deployment",
			},
		},
		Spec: ResourceSetSpec{
			PlatformInput: PlatformInput{
				Project:          "frontend",
				Namespace:        "holos-prj-frontend",
				GatewayNamespace: "istio-ingress",
				Organization:     "acme",
				Claims: Claims{
					Iss:           "https://dex.example.com",
					Sub:           "user-123",
					Aud:           "holos-console",
					Exp:           1700000000,
					Iat:           1699990000,
					Email:         "alice@example.com",
					EmailVerified: true,
					Name:          "Alice",
					Groups:        []string{"engineering", "platform"},
				},
			},
			ProjectInput: ProjectInput{
				Name:    "my-app",
				Image:   "ghcr.io/example/app",
				Tag:     "v1.2.3",
				Command: []string{"/bin/app"},
				Args:    []string{"--port", "8080"},
				Env: []EnvVar{
					{Name: "DB_HOST", Value: "postgres.default.svc"},
					{Name: "DB_PASSWORD", SecretKeyRef: &KeyRef{Name: "db-creds", Key: "password"}},
					{Name: "CONFIG_FILE", ConfigMapKeyRef: &KeyRef{Name: "app-config", Key: "config.yaml"}},
				},
				Port: 8080,
			},
			PlatformResources: PlatformResources{
				NamespacedResources: map[string]map[string]map[string]Resource{
					"istio-ingress": {
						"HTTPRoute": {
							"my-app": Resource{
								"apiVersion": "gateway.networking.k8s.io/v1",
								"kind":       "HTTPRoute",
								"metadata": map[string]interface{}{
									"name":      "my-app",
									"namespace": "istio-ingress",
								},
							},
						},
					},
				},
				ClusterResources: map[string]map[string]Resource{
					"ClusterRole": {
						"my-app-reader": Resource{
							"apiVersion": "rbac.authorization.k8s.io/v1",
							"kind":       "ClusterRole",
						},
					},
				},
			},
			ProjectResources: ProjectResources{
				NamespacedResources: map[string]map[string]map[string]Resource{
					"holos-prj-frontend": {
						"Deployment": {
							"my-app": Resource{
								"apiVersion": "apps/v1",
								"kind":       "Deployment",
								"metadata": map[string]interface{}{
									"name":      "my-app",
									"namespace": "holos-prj-frontend",
								},
							},
						},
						"Service": {
							"my-app": Resource{
								"apiVersion": "v1",
								"kind":       "Service",
							},
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundTripped ResourceSet
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify TypeMeta
	if roundTripped.APIVersion != APIVersion {
		t.Errorf("APIVersion = %q, want %q", roundTripped.APIVersion, APIVersion)
	}
	if roundTripped.Kind != KindResourceSet {
		t.Errorf("Kind = %q, want %q", roundTripped.Kind, KindResourceSet)
	}

	// Verify Metadata
	if roundTripped.Metadata.Name != "my-deployment" {
		t.Errorf("Metadata.Name = %q, want %q", roundTripped.Metadata.Name, "my-deployment")
	}
	if roundTripped.Metadata.Annotations[AnnotationDisplayName] != "My Deployment" {
		t.Errorf("Metadata.Annotations[DisplayName] = %q, want %q",
			roundTripped.Metadata.Annotations[AnnotationDisplayName], "My Deployment")
	}

	// Verify PlatformInput
	pi := roundTripped.Spec.PlatformInput
	if pi.Project != "frontend" {
		t.Errorf("PlatformInput.Project = %q, want %q", pi.Project, "frontend")
	}
	if pi.Organization != "acme" {
		t.Errorf("PlatformInput.Organization = %q, want %q", pi.Organization, "acme")
	}
	if pi.GatewayNamespace != "istio-ingress" {
		t.Errorf("PlatformInput.GatewayNamespace = %q, want %q", pi.GatewayNamespace, "istio-ingress")
	}
	if pi.Claims.Email != "alice@example.com" {
		t.Errorf("Claims.Email = %q, want %q", pi.Claims.Email, "alice@example.com")
	}
	if !pi.Claims.EmailVerified {
		t.Error("Claims.EmailVerified = false, want true")
	}
	if len(pi.Claims.Groups) != 2 || pi.Claims.Groups[0] != "engineering" {
		t.Errorf("Claims.Groups = %v, want [engineering platform]", pi.Claims.Groups)
	}

	// Verify ProjectInput
	inp := roundTripped.Spec.ProjectInput
	if inp.Name != "my-app" {
		t.Errorf("ProjectInput.Name = %q, want %q", inp.Name, "my-app")
	}
	if inp.Port != 8080 {
		t.Errorf("ProjectInput.Port = %d, want %d", inp.Port, 8080)
	}
	if len(inp.Command) != 1 || inp.Command[0] != "/bin/app" {
		t.Errorf("ProjectInput.Command = %v, want [/bin/app]", inp.Command)
	}
	if len(inp.Env) != 3 {
		t.Fatalf("ProjectInput.Env length = %d, want 3", len(inp.Env))
	}
	if inp.Env[1].SecretKeyRef == nil || inp.Env[1].SecretKeyRef.Key != "password" {
		t.Errorf("Env[1].SecretKeyRef = %+v, want Key=password", inp.Env[1].SecretKeyRef)
	}
	if inp.Env[2].ConfigMapKeyRef == nil || inp.Env[2].ConfigMapKeyRef.Name != "app-config" {
		t.Errorf("Env[2].ConfigMapKeyRef = %+v, want Name=app-config", inp.Env[2].ConfigMapKeyRef)
	}

	// Verify PlatformResources
	httpRoute, ok := roundTripped.Spec.PlatformResources.NamespacedResources["istio-ingress"]["HTTPRoute"]["my-app"]
	if !ok {
		t.Fatal("PlatformResources.NamespacedResources[istio-ingress][HTTPRoute][my-app] not found")
	}
	if httpRoute["kind"] != "HTTPRoute" {
		t.Errorf("HTTPRoute kind = %v, want HTTPRoute", httpRoute["kind"])
	}

	// Verify ProjectResources
	dep, ok := roundTripped.Spec.ProjectResources.NamespacedResources["holos-prj-frontend"]["Deployment"]["my-app"]
	if !ok {
		t.Fatal("ProjectResources.NamespacedResources[holos-prj-frontend][Deployment][my-app] not found")
	}
	if dep["kind"] != "Deployment" {
		t.Errorf("Deployment kind = %v, want Deployment", dep["kind"])
	}
}

// TestTypeMetaInlineEmbedding verifies that apiVersion and kind appear at the
// top level of JSON output, not nested under a "TypeMeta" key.
func TestTypeMetaInlineEmbedding(t *testing.T) {
	rs := ResourceSet{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindResourceSet,
		},
		Metadata: Metadata{Name: "test"},
	}

	data, err := json.Marshal(rs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	// apiVersion and kind must be top-level keys
	if _, ok := raw["apiVersion"]; !ok {
		t.Error("apiVersion not found at top level")
	}
	if _, ok := raw["kind"]; !ok {
		t.Error("kind not found at top level")
	}
	// TypeMeta must NOT appear as a key
	if _, ok := raw["TypeMeta"]; ok {
		t.Error("TypeMeta found as a nested key; embedding is broken")
	}
}

// TestOmitemptyBehavior verifies that optional fields with zero values are
// omitted from JSON output, while required fields are present even when zero.
func TestOmitemptyBehavior(t *testing.T) {
	// ProjectInput with zero-value optional fields (Command, Args, Env)
	// and zero-value required fields (Port)
	inp := ProjectInput{
		Name:  "test",
		Image: "nginx",
		Tag:   "latest",
		Port:  0, // no omitempty, must appear
	}

	data, err := json.Marshal(inp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	// Required fields must be present even when zero
	if _, ok := raw["port"]; !ok {
		t.Error("port should be present even when zero (no omitempty)")
	}

	// Optional fields should be omitted when zero/nil
	if _, ok := raw["command"]; ok {
		t.Error("command should be omitted when nil")
	}
	if _, ok := raw["args"]; ok {
		t.Error("args should be omitted when nil")
	}
	if _, ok := raw["env"]; ok {
		t.Error("env should be omitted when nil")
	}

	// Verify PlatformInput.GatewayNamespace is present even when empty (no omitempty)
	pi := PlatformInput{
		GatewayNamespace: "", // no omitempty, must appear
	}
	data, err = json.Marshal(pi)
	if err != nil {
		t.Fatalf("Marshal PlatformInput: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal PlatformInput: %v", err)
	}
	if _, ok := raw["gatewayNamespace"]; !ok {
		t.Error("gatewayNamespace should be present even when empty (no omitempty)")
	}

	// Verify Metadata.Annotations is omitted when nil
	m := Metadata{Name: "test"}
	data, err = json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal Metadata: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal Metadata: %v", err)
	}
	if _, ok := raw["annotations"]; ok {
		t.Error("annotations should be omitted when nil")
	}
}

// TestResourceMapRoundTrip verifies that a Resource with nested Kubernetes
// manifest structure survives a JSON round-trip.
func TestResourceMapRoundTrip(t *testing.T) {
	original := Resource{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":      "my-app",
			"namespace": "default",
			"labels": map[string]interface{}{
				"app.kubernetes.io/name":       "my-app",
				"app.kubernetes.io/managed-by": ManagedByValue,
			},
		},
		"spec": map[string]interface{}{
			"replicas": float64(3), // JSON numbers are float64
			"selector": map[string]interface{}{
				"matchLabels": map[string]interface{}{
					"app.kubernetes.io/name": "my-app",
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundTripped Resource
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	// Verify top-level fields
	if roundTripped["kind"] != "Deployment" {
		t.Errorf("kind = %v, want Deployment", roundTripped["kind"])
	}

	// Verify nested structure
	meta, ok := roundTripped["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata is not a map")
	}
	labels, ok := meta["labels"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata.labels is not a map")
	}
	if labels["app.kubernetes.io/managed-by"] != ManagedByValue {
		t.Errorf("managed-by label = %v, want %q", labels["app.kubernetes.io/managed-by"], ManagedByValue)
	}

	// Verify numeric fidelity
	spec, ok := roundTripped["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("spec is not a map")
	}
	if spec["replicas"] != float64(3) {
		t.Errorf("replicas = %v, want 3", spec["replicas"])
	}
}

// TestResourceCollectionsRoundTrip verifies the three-level map structure
// (namespace -> kind -> name -> resource) round-trips correctly for both
// PlatformResources and ProjectResources.
func TestResourceCollectionsRoundTrip(t *testing.T) {
	pr := PlatformResources{
		NamespacedResources: map[string]map[string]map[string]Resource{
			"istio-ingress": {
				"HTTPRoute": {
					"app-route": Resource{"kind": "HTTPRoute"},
				},
				"ReferenceGrant": {
					"app-grant": Resource{"kind": "ReferenceGrant"},
				},
			},
			"monitoring": {
				"ServiceMonitor": {
					"app-monitor": Resource{"kind": "ServiceMonitor"},
				},
			},
		},
		ClusterResources: map[string]map[string]Resource{
			"ClusterRole": {
				"platform-reader": Resource{"kind": "ClusterRole"},
			},
		},
	}

	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatalf("Marshal PlatformResources: %v", err)
	}

	var roundTripped PlatformResources
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal PlatformResources: %v", err)
	}

	// Verify namespaced resources survive
	if _, ok := roundTripped.NamespacedResources["istio-ingress"]["HTTPRoute"]["app-route"]; !ok {
		t.Error("istio-ingress/HTTPRoute/app-route not found after round-trip")
	}
	if _, ok := roundTripped.NamespacedResources["istio-ingress"]["ReferenceGrant"]["app-grant"]; !ok {
		t.Error("istio-ingress/ReferenceGrant/app-grant not found after round-trip")
	}
	if _, ok := roundTripped.NamespacedResources["monitoring"]["ServiceMonitor"]["app-monitor"]; !ok {
		t.Error("monitoring/ServiceMonitor/app-monitor not found after round-trip")
	}

	// Verify cluster resources survive
	if _, ok := roundTripped.ClusterResources["ClusterRole"]["platform-reader"]; !ok {
		t.Error("ClusterRole/platform-reader not found after round-trip")
	}

	// Also test ProjectResources (identical structure, different type)
	projR := ProjectResources{
		NamespacedResources: map[string]map[string]map[string]Resource{
			"my-namespace": {
				"Deployment": {
					"app": Resource{"kind": "Deployment"},
				},
			},
		},
	}

	data, err = json.Marshal(projR)
	if err != nil {
		t.Fatalf("Marshal ProjectResources: %v", err)
	}

	var projRoundTripped ProjectResources
	if err := json.Unmarshal(data, &projRoundTripped); err != nil {
		t.Fatalf("Unmarshal ProjectResources: %v", err)
	}

	if _, ok := projRoundTripped.NamespacedResources["my-namespace"]["Deployment"]["app"]; !ok {
		t.Error("my-namespace/Deployment/app not found after round-trip")
	}
}

// TestOrganizationAndProjectJSONRoundTrip verifies that Organization and
// Project types round-trip through JSON correctly.
func TestOrganizationAndProjectJSONRoundTrip(t *testing.T) {
	org := Organization{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindOrganization,
		},
		Metadata: Metadata{
			Name: "acme",
			Annotations: map[string]string{
				AnnotationDisplayName: "Acme Corp",
			},
		},
	}

	data, err := json.Marshal(org)
	if err != nil {
		t.Fatalf("Marshal Organization: %v", err)
	}

	var orgRT Organization
	if err := json.Unmarshal(data, &orgRT); err != nil {
		t.Fatalf("Unmarshal Organization: %v", err)
	}
	if orgRT.Kind != KindOrganization {
		t.Errorf("Organization.Kind = %q, want %q", orgRT.Kind, KindOrganization)
	}
	if orgRT.Metadata.Name != "acme" {
		t.Errorf("Organization.Metadata.Name = %q, want %q", orgRT.Metadata.Name, "acme")
	}

	proj := Project{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindProject,
		},
		Metadata: Metadata{Name: "frontend"},
		Spec:     ProjectSpec{Organization: "acme"},
	}

	data, err = json.Marshal(proj)
	if err != nil {
		t.Fatalf("Marshal Project: %v", err)
	}

	// Verify inline embedding for Project too
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal Project to map: %v", err)
	}
	if raw["apiVersion"] != APIVersion {
		t.Errorf("Project apiVersion = %v, want %q", raw["apiVersion"], APIVersion)
	}

	var projRT Project
	if err := json.Unmarshal(data, &projRT); err != nil {
		t.Fatalf("Unmarshal Project: %v", err)
	}
	if projRT.Spec.Organization != "acme" {
		t.Errorf("Project.Spec.Organization = %q, want %q", projRT.Spec.Organization, "acme")
	}
}

// TestClaimsOmitemptyBehavior verifies that Claims optional fields (Aud, Name,
// Groups) are omitted when zero, while required fields are present.
func TestClaimsOmitemptyBehavior(t *testing.T) {
	c := Claims{
		Iss:           "https://dex.example.com",
		Sub:           "user-1",
		Exp:           1700000000,
		Iat:           1699990000,
		Email:         "test@example.com",
		EmailVerified: false, // no omitempty, must appear
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	// Required fields must be present
	if _, ok := raw["email_verified"]; !ok {
		t.Error("email_verified should be present even when false (no omitempty)")
	}
	if _, ok := raw["iss"]; !ok {
		t.Error("iss should be present")
	}

	// Optional fields should be omitted
	if _, ok := raw["aud"]; ok {
		t.Error("aud should be omitted when empty")
	}
	if _, ok := raw["name"]; ok {
		t.Error("name should be omitted when empty")
	}
	if _, ok := raw["groups"]; ok {
		t.Error("groups should be omitted when nil")
	}
}

// TestEnvVarVariants verifies that all three EnvVar forms (literal, secretKeyRef,
// configMapKeyRef) serialize correctly.
func TestEnvVarVariants(t *testing.T) {
	envs := []EnvVar{
		{Name: "LITERAL", Value: "hello"},
		{Name: "SECRET", SecretKeyRef: &KeyRef{Name: "my-secret", Key: "password"}},
		{Name: "CONFIGMAP", ConfigMapKeyRef: &KeyRef{Name: "my-config", Key: "setting"}},
	}

	data, err := json.Marshal(envs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var roundTripped []EnvVar
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if len(roundTripped) != 3 {
		t.Fatalf("len = %d, want 3", len(roundTripped))
	}

	// Literal value: Value set, refs nil
	if roundTripped[0].Value != "hello" {
		t.Errorf("LITERAL.Value = %q, want %q", roundTripped[0].Value, "hello")
	}
	if roundTripped[0].SecretKeyRef != nil {
		t.Error("LITERAL.SecretKeyRef should be nil")
	}

	// Secret ref: SecretKeyRef set, Value empty
	if roundTripped[1].SecretKeyRef == nil {
		t.Fatal("SECRET.SecretKeyRef is nil")
	}
	if roundTripped[1].SecretKeyRef.Name != "my-secret" {
		t.Errorf("SECRET.SecretKeyRef.Name = %q, want %q", roundTripped[1].SecretKeyRef.Name, "my-secret")
	}

	// ConfigMap ref: ConfigMapKeyRef set
	if roundTripped[2].ConfigMapKeyRef == nil {
		t.Fatal("CONFIGMAP.ConfigMapKeyRef is nil")
	}
	if roundTripped[2].ConfigMapKeyRef.Key != "setting" {
		t.Errorf("CONFIGMAP.ConfigMapKeyRef.Key = %q, want %q", roundTripped[2].ConfigMapKeyRef.Key, "setting")
	}
}

// TestConstants verifies that annotation and label constants have expected values.
func TestConstants(t *testing.T) {
	if APIVersion != "console.holos.run/v1alpha1" {
		t.Errorf("APIVersion = %q, want %q", APIVersion, "console.holos.run/v1alpha1")
	}
	if KindResourceSet != "ResourceSet" {
		t.Errorf("KindResourceSet = %q, want %q", KindResourceSet, "ResourceSet")
	}
	if LabelManagedBy != "app.kubernetes.io/managed-by" {
		t.Errorf("LabelManagedBy = %q, want %q", LabelManagedBy, "app.kubernetes.io/managed-by")
	}
	if ManagedByValue != "console.holos.run" {
		t.Errorf("ManagedByValue = %q, want %q", ManagedByValue, "console.holos.run")
	}
}
