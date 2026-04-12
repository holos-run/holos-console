package templates

import (
	"encoding/json"
	"testing"
)

func TestSerializeResources_Empty(t *testing.T) {
	tests := []struct {
		name      string
		input     []RenderResource
		wantYAML  string
		wantJSON  string
		wantErr   bool
		wantValid bool // JSON output must be valid JSON
	}{
		{
			name:      "nil input returns empty JSON array",
			input:     nil,
			wantYAML:  "",
			wantJSON:  "[]",
			wantValid: true,
		},
		{
			name:      "empty slice returns empty JSON array",
			input:     []RenderResource{},
			wantYAML:  "",
			wantJSON:  "[]",
			wantValid: true,
		},
		{
			name: "single resource",
			input: []RenderResource{
				{
					YAML:   "apiVersion: v1\nkind: ConfigMap\n",
					Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"},
				},
			},
			wantYAML:  "apiVersion: v1\nkind: ConfigMap\n",
			wantValid: true,
		},
		{
			name: "multiple resources",
			input: []RenderResource{
				{
					YAML:   "apiVersion: v1\nkind: ConfigMap\n",
					Object: map[string]any{"apiVersion": "v1", "kind": "ConfigMap"},
				},
				{
					YAML:   "apiVersion: v1\nkind: Secret\n",
					Object: map[string]any{"apiVersion": "v1", "kind": "Secret"},
				},
			},
			wantYAML:  "apiVersion: v1\nkind: ConfigMap\n---\napiVersion: v1\nkind: Secret\n",
			wantValid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlStr, jsonStr, err := serializeResources(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("serializeResources() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantYAML != "" && yamlStr != tt.wantYAML {
				t.Errorf("serializeResources() yamlStr = %q, want %q", yamlStr, tt.wantYAML)
			}
			if tt.wantJSON != "" && jsonStr != tt.wantJSON {
				t.Errorf("serializeResources() jsonStr = %q, want %q", jsonStr, tt.wantJSON)
			}
			if tt.wantValid && !json.Valid([]byte(jsonStr)) {
				t.Errorf("serializeResources() jsonStr = %q is not valid JSON", jsonStr)
			}
		})
	}
}
