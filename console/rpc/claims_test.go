package rpc

import (
	"testing"
)

func TestExtractRolesFromClaims(t *testing.T) {
	tests := []struct {
		name       string
		rolesClaim string
		claims     map[string]interface{}
		wantRoles  []string
	}{
		{
			name:       "default groups claim",
			rolesClaim: "groups",
			claims: map[string]interface{}{
				"groups": []interface{}{"owner", "admin"},
			},
			wantRoles: []string{"owner", "admin"},
		},
		{
			name:       "custom realm_roles claim",
			rolesClaim: "realm_roles",
			claims: map[string]interface{}{
				"realm_roles": []interface{}{"editor"},
				"groups":      []interface{}{"should-be-ignored"},
			},
			wantRoles: []string{"editor"},
		},
		{
			name:       "missing claim returns empty",
			rolesClaim: "roles",
			claims: map[string]interface{}{
				"groups": []interface{}{"owner"},
			},
			wantRoles: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractRoles(tt.claims, tt.rolesClaim)
			if len(got) != len(tt.wantRoles) {
				t.Fatalf("ExtractRoles() = %v, want %v", got, tt.wantRoles)
			}
			for i := range got {
				if got[i] != tt.wantRoles[i] {
					t.Errorf("ExtractRoles()[%d] = %q, want %q", i, got[i], tt.wantRoles[i])
				}
			}
		})
	}
}
