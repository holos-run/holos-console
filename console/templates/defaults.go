package templates

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	v1alpha2 "github.com/holos-run/holos-console/api/v1alpha2"
	consolev1 "github.com/holos-run/holos-console/gen/holos/console/v1"
)

// ExtractDefaults evaluates the CUE template source and extracts the concrete
// values from the top-level `defaults` field. Returns the populated
// DeploymentDefaults proto message, or nil if the template has no `defaults`
// block. Returns an error only for CUE compilation failures — a missing
// `defaults` field is not an error.
//
// This function prepends the generated v1alpha2 schema (same as the renderer
// does) so that templates can reference #ProjectInput and related types.
func ExtractDefaults(cueSource string) (*consolev1.TemplateDefaults, error) {
	cueCtx := cuecontext.New()

	// Prepend generated schema so templates can use #ProjectInput, etc.
	fullSource := v1alpha2.GeneratedSchema + "\n" + cueSource
	val := cueCtx.CompileString(fullSource)
	if err := val.Err(); err != nil {
		return nil, fmt.Errorf("compiling CUE template for defaults extraction: %w", err)
	}

	defaultsVal := val.LookupPath(cue.ParsePath("defaults"))
	if !defaultsVal.Exists() {
		// Template has no defaults block — this is normal.
		return nil, nil
	}
	if err := defaultsVal.Err(); err != nil {
		return nil, fmt.Errorf("evaluating defaults field: %w", err)
	}

	// Extract each field independently so that a non-concrete field does not
	// prevent extraction of concrete siblings. See ADR 025 for rationale.
	d := &consolev1.TemplateDefaults{}

	// String fields: name, image, tag, description.
	for _, f := range []struct {
		path string
		dest *string
	}{
		{"name", &d.Name},
		{"image", &d.Image},
		{"tag", &d.Tag},
		{"description", &d.Description},
	} {
		v := defaultsVal.LookupPath(cue.ParsePath(f.path))
		if !v.Exists() || !v.IsConcrete() {
			slog.Debug("defaults field not concrete, skipping", "field", f.path)
			continue
		}
		b, err := v.MarshalJSON()
		if err != nil {
			slog.Debug("defaults field marshal failed, skipping", "field", f.path, "error", err)
			continue
		}
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			slog.Debug("defaults field unmarshal failed, skipping", "field", f.path, "error", err)
			continue
		}
		*f.dest = s
	}

	// Integer field: port.
	if v := defaultsVal.LookupPath(cue.ParsePath("port")); !v.Exists() || !v.IsConcrete() {
		slog.Debug("defaults field not concrete, skipping", "field", "port")
	} else if b, err := v.MarshalJSON(); err != nil {
		slog.Debug("defaults field marshal failed, skipping", "field", "port", "error", err)
	} else {
		var n int32
		if err := json.Unmarshal(b, &n); err != nil {
			slog.Debug("defaults field unmarshal failed, skipping", "field", "port", "error", err)
		} else {
			d.Port = n
		}
	}

	// String slice fields: command, args.
	for _, f := range []struct {
		path string
		dest *[]string
	}{
		{"command", &d.Command},
		{"args", &d.Args},
	} {
		v := defaultsVal.LookupPath(cue.ParsePath(f.path))
		if !v.Exists() || !v.IsConcrete() {
			slog.Debug("defaults field not concrete, skipping", "field", f.path)
			continue
		}
		b, err := v.MarshalJSON()
		if err != nil {
			slog.Debug("defaults field marshal failed, skipping", "field", f.path, "error", err)
			continue
		}
		var ss []string
		if err := json.Unmarshal(b, &ss); err != nil {
			slog.Debug("defaults field unmarshal failed, skipping", "field", f.path, "error", err)
			continue
		}
		if len(ss) > 0 {
			*f.dest = ss
		}
	}

	// Return nil if all fields are zero — means no meaningful defaults.
	if d.Name == "" && d.Image == "" && d.Tag == "" && d.Description == "" && d.Port == 0 && len(d.Command) == 0 && len(d.Args) == 0 {
		return nil, nil
	}

	return d, nil
}
