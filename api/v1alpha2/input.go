package v1alpha2

// KeyRef identifies a key within a Kubernetes Secret or ConfigMap.
type KeyRef struct {
	Name string `json:"name" yaml:"name" cue:"name"`
	Key  string `json:"key"  yaml:"key"  cue:"key"`
}

// EnvVar represents a container environment variable. Exactly one of Value,
// SecretKeyRef, or ConfigMapKeyRef should be set.
type EnvVar struct {
	Name            string  `json:"name"                      yaml:"name"                      cue:"name"`
	Value           string  `json:"value,omitempty"           yaml:"value,omitempty"           cue:"value?"`
	SecretKeyRef    *KeyRef `json:"secretKeyRef,omitempty"    yaml:"secretKeyRef,omitempty"    cue:"secretKeyRef?"`
	ConfigMapKeyRef *KeyRef `json:"configMapKeyRef,omitempty" yaml:"configMapKeyRef,omitempty" cue:"configMapKeyRef?"`
}

// Claims carries OIDC ID token claims from the authenticated user.
type Claims struct {
	Iss           string   `json:"iss"              yaml:"iss"              cue:"iss"`
	Sub           string   `json:"sub"              yaml:"sub"              cue:"sub"`
	Aud           string   `json:"aud,omitempty"    yaml:"aud,omitempty"    cue:"aud?"`
	Exp           int64    `json:"exp"              yaml:"exp"              cue:"exp"`
	Iat           int64    `json:"iat"              yaml:"iat"              cue:"iat"`
	Email         string   `json:"email"            yaml:"email"            cue:"email"`
	EmailVerified bool     `json:"email_verified"   yaml:"email_verified"   cue:"email_verified"`
	Name          string   `json:"name,omitempty"   yaml:"name,omitempty"   cue:"name?"`
	Groups        []string `json:"groups,omitempty" yaml:"groups,omitempty" cue:"groups?"`
}

// FolderInfo carries the name and namespace of a folder in the ancestry chain.
// It is used in [PlatformInput.Folders] to describe the folder path from the
// organization root down to the immediate parent of the project.
type FolderInfo struct {
	// Name is the folder's display name as set by the SRE who created it.
	Name string `json:"name"      yaml:"name"      cue:"name"`
	// Namespace is the Kubernetes namespace for this folder level.
	Namespace string `json:"namespace" yaml:"namespace" cue:"namespace"`
}

// PlatformInput carries values set by platform engineers and the system.
// Template authors can rely on these values being verified by the backend.
//
// In v1alpha2 it includes the folder ancestry chain (Folders) between the
// organization and the project (ADR 020 Decision 8).
type PlatformInput struct {
	// Project is the parent project name, resolved from the authenticated session.
	Project string `json:"project"          yaml:"project"          cue:"project"`
	// Namespace is the Kubernetes namespace for the project, resolved by the backend.
	Namespace string `json:"namespace"        yaml:"namespace"        cue:"namespace"`
	// GatewayNamespace is the namespace of the ingress gateway (default: "istio-ingress").
	GatewayNamespace string `json:"gatewayNamespace" yaml:"gatewayNamespace" cue:"gatewayNamespace"`
	// Organization is the root organization name.
	Organization string `json:"organization"     yaml:"organization"     cue:"organization"`
	// Claims carries the OIDC ID token claims of the authenticated user.
	Claims Claims `json:"claims"           yaml:"claims"           cue:"claims"`
	// Folders is the ordered chain of folder names from the organization down
	// to the immediate parent of the project. The first element is the
	// top-level folder (direct child of the organization); the last element is
	// the immediate folder parent of the project. Empty if the project is a
	// direct child of the organization (no folders).
	Folders []FolderInfo `json:"folders,omitempty" yaml:"folders,omitempty" cue:"folders?"`
}

// ProjectInput carries values provided by the product engineer via the
// deployment form. Template authors should treat these as user-supplied and
// validate them with CUE constraints.
type ProjectInput struct {
	// Name is the deployment name. Must be a valid DNS label.
	Name string `json:"name"             yaml:"name"             cue:"name"`
	// Image is the container image repository (e.g. "ghcr.io/example/app").
	Image string `json:"image"            yaml:"image"            cue:"image"`
	// Tag is the image tag (e.g. "v1.2.3").
	Tag string `json:"tag"              yaml:"tag"              cue:"tag"`
	// Command overrides the container ENTRYPOINT.
	Command []string `json:"command,omitempty" yaml:"command,omitempty" cue:"command?"`
	// Args overrides the container CMD.
	Args []string `json:"args,omitempty"    yaml:"args,omitempty"    cue:"args?"`
	// Env defines container environment variables.
	Env []EnvVar `json:"env,omitempty"     yaml:"env,omitempty"     cue:"env?"`
	// Port is the container port the application listens on (default: 8080).
	Port int `json:"port"              yaml:"port"              cue:"port"`
	// Description is a short human-readable description of the deployment.
	// Template authors can set this in the defaults block to pre-fill the
	// Create Deployment form. Users may override it at deploy time.
	Description string `json:"description,omitempty" yaml:"description,omitempty" cue:"description?"`
}
