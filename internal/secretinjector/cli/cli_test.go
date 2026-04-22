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

package cli

import (
	"strings"
	"testing"
)

// TestResolveMeshTrustDomain pins the flag / env / default precedence
// introduced in HOL-839. The flag wins over the env var; an empty flag
// falls back to HOLOS_SECRETINJECTOR_MESH_TRUST_DOMAIN; an empty env var
// returns "" so controller.Options defaults to the upstream Istio
// cluster.local.
func TestResolveMeshTrustDomain(t *testing.T) {
	cases := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{name: "flag wins over env", flag: "mesh.example.com", env: "env.example.com", want: "mesh.example.com"},
		{name: "flag wins when env empty", flag: "mesh.example.com", env: "", want: "mesh.example.com"},
		{name: "env used when flag empty", flag: "", env: "env.example.com", want: "env.example.com"},
		{name: "both empty returns empty (manager applies cluster.local)", flag: "", env: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(MeshTrustDomainEnv, tc.env)
			if got := resolveMeshTrustDomain(tc.flag); got != tc.want {
				t.Fatalf("resolveMeshTrustDomain(%q) with env %q = %q; want %q", tc.flag, tc.env, got, tc.want)
			}
		})
	}
}

// TestCommand_MeshTrustDomainFlagRegistered guards the flag registration
// so a future refactor of Command() cannot silently drop the knob
// operators depend on for re-pegged meshes.
func TestCommand_MeshTrustDomainFlagRegistered(t *testing.T) {
	cmd := Command()
	flag := cmd.PersistentFlags().Lookup("mesh-trust-domain")
	if flag == nil {
		t.Fatalf("--mesh-trust-domain flag not registered on root command")
	}
	if flag.DefValue != "" {
		t.Fatalf("--mesh-trust-domain default=%q; want empty so the controller package owns the cluster.local fallback", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, MeshTrustDomainEnv) {
		t.Fatalf("--mesh-trust-domain usage string does not mention %s: %q", MeshTrustDomainEnv, flag.Usage)
	}
}
