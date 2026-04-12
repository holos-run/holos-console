package deployments

import (
	"encoding/json"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestSerializeUnstructured_Empty(t *testing.T) {
	tests := []struct {
		name      string
		input     []unstructured.Unstructured
		wantYAML  string
		wantJSON  string
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
			input:     []unstructured.Unstructured{},
			wantYAML:  "",
			wantJSON:  "[]",
			wantValid: true,
		},
		{
			name: "single resource",
			input: []unstructured.Unstructured{
				{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata":   map[string]any{"name": "test"},
					},
				},
			},
			wantValid: true,
		},
		{
			name: "multiple resources",
			input: []unstructured.Unstructured{
				{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata":   map[string]any{"name": "cm1"},
					},
				},
				{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "Secret",
						"metadata":   map[string]any{"name": "sec1"},
					},
				},
			},
			wantValid: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			yamlStr, jsonStr := serializeUnstructured(tt.input)
			if tt.wantYAML != "" && yamlStr != tt.wantYAML {
				t.Errorf("serializeUnstructured() yamlStr = %q, want %q", yamlStr, tt.wantYAML)
			}
			if tt.wantJSON != "" && jsonStr != tt.wantJSON {
				t.Errorf("serializeUnstructured() jsonStr = %q, want %q", jsonStr, tt.wantJSON)
			}
			if tt.wantValid && !json.Valid([]byte(jsonStr)) {
				t.Errorf("serializeUnstructured() jsonStr = %q is not valid JSON", jsonStr)
			}
		})
	}
}
