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

	// Marshal the CUE defaults value to JSON, then unmarshal into a ProjectInput
	// struct. This gives us typed access to the defaults fields.
	b, err := defaultsVal.MarshalJSON()
	if err != nil {
		// Defaults are not fully concrete — skip rather than error.
		slog.Debug("defaults field not concrete, skipping extraction", "error", err)
		return nil, nil
	}

	var pi v1alpha2.ProjectInput
	if err := json.Unmarshal(b, &pi); err != nil {
		return nil, fmt.Errorf("unmarshalling defaults into ProjectInput: %w", err)
	}

	d := &consolev1.TemplateDefaults{
		Name:        pi.Name,
		Image:       pi.Image,
		Tag:         pi.Tag,
		Description: pi.Description,
		Port:        int32(pi.Port),
	}

	// Map optional fields.
	if len(pi.Command) > 0 {
		d.Command = pi.Command
	}
	if len(pi.Args) > 0 {
		d.Args = pi.Args
	}
	// Env is not typically set in defaults; skip for now.

	// Return nil if all fields are zero — means no meaningful defaults.
	if d.Name == "" && d.Image == "" && d.Tag == "" && d.Description == "" && d.Port == 0 {
		return nil, nil
	}

	return d, nil
}
