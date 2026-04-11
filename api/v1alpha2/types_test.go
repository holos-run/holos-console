package v1alpha2

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
				Folders: []FolderInfo{
					{Name: "payments", Namespace: "holos-fld-a4b9c1-payments"},
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
			},
			ProjectResources: ProjectResources{
				NamespacedResources: map[string]map[string]map[string]Resource{
					"holos-prj-frontend": {
						"Deployment": {
							"my-app": Resource{
								"apiVersion": "apps/v1",
								"kind":       "Deployment",
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

	// Verify PlatformInput
	pi := roundTripped.Spec.PlatformInput
	if pi.Organization != "acme" {
		t.Errorf("PlatformInput.Organization = %q, want %q", pi.Organization, "acme")
	}
	if len(pi.Folders) != 1 {
		t.Fatalf("PlatformInput.Folders length = %d, want 1", len(pi.Folders))
	}
	if pi.Folders[0].Name != "payments" {
		t.Errorf("Folders[0].Name = %q, want %q", pi.Folders[0].Name, "payments")
	}
	if pi.Folders[0].Namespace != "holos-fld-a4b9c1-payments" {
		t.Errorf("Folders[0].Namespace = %q, want %q", pi.Folders[0].Namespace, "holos-fld-a4b9c1-payments")
	}
}

// TestFolderJSONRoundTrip verifies that a Folder survives a JSON round-trip.
func TestFolderJSONRoundTrip(t *testing.T) {
	folder := Folder{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindFolder,
		},
		Metadata: Metadata{
			Name: "payments",
			Annotations: map[string]string{
				AnnotationDisplayName: "Payments",
			},
		},
		Spec: FolderSpec{
			DisplayName:  "Payments",
			Organization: "acme",
		},
	}

	data, err := json.Marshal(folder)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var rt Folder
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if rt.Kind != KindFolder {
		t.Errorf("Kind = %q, want %q", rt.Kind, KindFolder)
	}
	if rt.Metadata.Name != "payments" {
		t.Errorf("Metadata.Name = %q, want %q", rt.Metadata.Name, "payments")
	}
	if rt.Spec.Organization != "acme" {
		t.Errorf("Spec.Organization = %q, want %q", rt.Spec.Organization, "acme")
	}
	if rt.Spec.Parent != "" {
		t.Errorf("Spec.Parent = %q, want empty (top-level folder)", rt.Spec.Parent)
	}
}

// TestNestedFolderJSONRoundTrip verifies that a nested Folder (with a Parent)
// survives a JSON round-trip.
func TestNestedFolderJSONRoundTrip(t *testing.T) {
	folder := Folder{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindFolder,
		},
		Metadata: Metadata{Name: "eu"},
		Spec: FolderSpec{
			DisplayName:  "EU",
			Organization: "acme",
			Parent:       "payments",
		},
	}

	data, err := json.Marshal(folder)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Verify inline embedding (apiVersion at top level, no TypeMeta key)
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if raw["apiVersion"] != APIVersion {
		t.Errorf("apiVersion = %v, want %q", raw["apiVersion"], APIVersion)
	}
	if _, ok := raw["TypeMeta"]; ok {
		t.Error("TypeMeta found as a nested key; embedding is broken")
	}

	var rt Folder
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("Unmarshal Folder: %v", err)
	}
	if rt.Spec.Parent != "payments" {
		t.Errorf("Spec.Parent = %q, want %q", rt.Spec.Parent, "payments")
	}
}

// TestProjectWithFolderParentJSONRoundTrip verifies that a Project with a
// folder parent (non-empty Parent field) survives a JSON round-trip.
func TestProjectWithFolderParentJSONRoundTrip(t *testing.T) {
	proj := Project{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindProject,
		},
		Metadata: Metadata{Name: "payments-api"},
		Spec: ProjectSpec{
			Organization: "acme",
			Parent:       "eu",
		},
	}

	data, err := json.Marshal(proj)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var rt Project
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rt.Spec.Organization != "acme" {
		t.Errorf("Spec.Organization = %q, want %q", rt.Spec.Organization, "acme")
	}
	if rt.Spec.Parent != "eu" {
		t.Errorf("Spec.Parent = %q, want %q", rt.Spec.Parent, "eu")
	}
}

// TestProjectDirectOrgChildJSONRoundTrip verifies that a Project that is a
// direct child of an org (Parent == Organization) round-trips correctly.
func TestProjectDirectOrgChildJSONRoundTrip(t *testing.T) {
	proj := Project{
		TypeMeta: TypeMeta{
			APIVersion: APIVersion,
			Kind:       KindProject,
		},
		Metadata: Metadata{Name: "frontend"},
		Spec: ProjectSpec{
			Organization: "acme",
			Parent:       "acme",
		},
	}

	data, err := json.Marshal(proj)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var rt Project
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rt.Spec.Parent != "acme" {
		t.Errorf("Spec.Parent = %q, want %q", rt.Spec.Parent, "acme")
	}
}

// TestPlatformInputFoldersOmitempty verifies that Folders is omitted from JSON
// when empty/nil (no unnecessary null or [] in output).
func TestPlatformInputFoldersOmitempty(t *testing.T) {
	pi := PlatformInput{
		Project:          "app",
		Namespace:        "holos-prj-app",
		GatewayNamespace: "istio-ingress",
		Organization:     "acme",
		Claims: Claims{
			Iss:           "https://dex.example.com",
			Sub:           "u1",
			Exp:           1700000000,
			Iat:           1699990000,
			Email:         "u@example.com",
			EmailVerified: false,
		},
	}

	data, err := json.Marshal(pi)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}

	if _, ok := raw["folders"]; ok {
		t.Error("folders should be omitted when nil")
	}
}

// TestOrganizationJSONRoundTrip verifies that Organization round-trips correctly.
func TestOrganizationJSONRoundTrip(t *testing.T) {
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
		t.Fatalf("Marshal: %v", err)
	}

	var rt Organization
	if err := json.Unmarshal(data, &rt); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rt.Kind != KindOrganization {
		t.Errorf("Kind = %q, want %q", rt.Kind, KindOrganization)
	}
	if rt.Metadata.Name != "acme" {
		t.Errorf("Metadata.Name = %q, want %q", rt.Metadata.Name, "acme")
	}
}

// TestConstantsV1alpha2 verifies that key constants have expected values.
func TestConstantsV1alpha2(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"APIVersion", APIVersion, "console.holos.run/v1alpha2"},
		{"KindFolder", KindFolder, "Folder"},
		{"KindResourceSet", KindResourceSet, "ResourceSet"},
		{"KindOrganization", KindOrganization, "Organization"},
		{"KindProject", KindProject, "Project"},
		{"ResourceTypeFolder", ResourceTypeFolder, "folder"},
		{"AnnotationParent", AnnotationParent, "console.holos.run/parent"},
		{"LabelManagedBy", LabelManagedBy, "app.kubernetes.io/managed-by"},
		{"ManagedByValue", ManagedByValue, "console.holos.run"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}
