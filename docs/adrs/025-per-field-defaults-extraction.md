# ADR 025: Per-Field CUE Defaults Extraction

## Status

Superseded by [ADR 027](027-template-defaults-prefill.md).

See ADR 027 for the authoritative Create Deployment form pre-fill behavior; the per-field extraction correctness rule below is still honored by the backend extractor.

## Context

ADR 018 introduced the `defaults` block in CUE templates, allowing template
authors to declare default values for `ProjectInput` fields (name, image, tag,
description, port, command, args). The backend extracts these defaults by
calling `defaultsVal.MarshalJSON()` on the entire `defaults` CUE value, then
unmarshalling the JSON into a `ProjectInput` struct.

This whole-block marshaling approach has a silent failure mode: if **any single
field** in the `defaults` block is not concrete (e.g., it references a CUE
expression that has not been unified to a final value), `MarshalJSON()` fails
for the entire value and the function returns nil. This means **all** defaults
are silently dropped, even fields that are perfectly concrete and extractable.

For example, consider a template where `env` is defined using a CUE
comprehension that references external context:

```cue
defaults: #ProjectInput & {
    name:        "my-service"
    image:       "ghcr.io/example/my-service"
    tag:         "v1.0.0"
    description: "Example service"
    port:        8080
    env:         _  // Non-concrete: depends on platform context
}
```

Under the current implementation, the non-concrete `env` field causes
`MarshalJSON()` to fail, which means the backend returns no defaults at all.
The frontend receives no pre-fill values for the Create Deployment form, even
though `name`, `image`, `tag`, `description`, and `port` are all concrete and
could be extracted.

This is a correctness problem: the failure of one field should not prevent the
extraction of other independent fields.

## Decisions

### 1. Extract each `ProjectInput` field independently from the `defaults` block.

Instead of marshaling the entire `defaults` value as a single JSON object,
`ExtractDefaults` iterates over the known `ProjectInput` fields and marshals
each one individually. A field that fails to marshal (because it is not
concrete) is silently skipped, while concrete fields are preserved.

The extraction logic follows this pattern:

```go
defaultsVal := value.LookupPath(cue.ParsePath("defaults"))
for _, fieldName := range []string{"name", "image", "tag", "description", "port", "command", "args"} {
    fieldVal := defaultsVal.LookupPath(cue.ParsePath(fieldName))
    if !fieldVal.Exists() {
        continue
    }
    b, err := fieldVal.MarshalJSON()
    if err != nil {
        // Field is not concrete — skip it, do not fail the whole extraction.
        slog.Debug("defaults field not concrete, skipping", "field", fieldName, "error", err)
        continue
    }
    // Unmarshal b into the corresponding field of TemplateDefaults.
}
```

This replaces the previous approach where a single `defaultsVal.MarshalJSON()`
call marshaled the entire defaults block or failed entirely.

### 2. Non-concrete fields are omitted, not errored.

When a field in the `defaults` block is not fully concrete (e.g., it contains
`_`, an open constraint, or a reference to an unfilled path), that field is
silently omitted from the extracted `TemplateDefaults`. The function does not
return an error for non-concrete fields — it logs a debug message and moves on.

This matches the existing behavior for the whole-block case (which returned nil
on marshal failure) but with finer granularity: instead of losing all defaults,
only the non-concrete fields are lost.

### 3. Concrete fields are always preserved regardless of sibling field concreteness.

The core guarantee of per-field extraction is: a concrete field in `defaults`
is always extracted and returned in the `TemplateDefaults` proto, regardless of
whether other sibling fields in the same `defaults` block are concrete. There
is no interaction between fields — each is marshaled independently.

### 4. The `env` field is deferred from extraction.

The `env` field (a list of `EnvVar` structs) is not included in the per-field
extraction loop. Environment variables are the most likely field to contain
non-concrete values (referencing platform secrets, service discovery endpoints,
or runtime context). Extracting `env` defaults would require additional design
work around how to represent partially-concrete lists. This is deferred to a
future iteration.

The extracted `ProjectInput` fields are: `name`, `image`, `tag`,
`description`, `port`, `command`, `args`.

### 5. The `TemplateDefaults` proto message is unchanged.

Per-field extraction is a backend implementation change only. The
`TemplateDefaults` proto message retains the same fields and semantics. The
frontend does not need any changes — it continues to read `TemplateDefaults`
from the template response and pre-fill the Create Deployment form.

## Consequences

### Positive

- **Resilient extraction.** A template with one non-concrete field in
  `defaults` still provides pre-fill values for all its concrete fields. The
  frontend form is partially populated rather than completely empty.

- **No silent data loss.** Under the old approach, a template author adding a
  non-concrete field (like `env: _`) to the defaults block would unknowingly
  break all defaults extraction for that template. The per-field approach
  degrades gracefully.

- **Backwards compatible.** Templates with fully concrete `defaults` blocks
  produce identical `TemplateDefaults` output. Templates without a `defaults`
  block continue to return nil. The only behavioral change is that templates
  with partially-concrete defaults now return partial results instead of nil.

- **Debuggable.** Each skipped field emits a debug log entry with the field
  name and the CUE error, making it easy to diagnose which fields were not
  extractable and why.

### Negative

- **Per-field marshaling overhead.** Instead of one `MarshalJSON()` call, the
  function makes up to seven calls (one per field). The overhead is negligible
  for the field count involved, but it is a measurable difference in
  micro-benchmarks.

- **Field list maintenance.** The list of fields to extract
  (`name`, `image`, `tag`, `description`, `port`, `command`, `args`) is
  enumerated explicitly in the Go code. Adding a new field to `ProjectInput`
  requires updating this list. The guardrail in
  `docs/agents/guardrail-template-fields.md` covers this requirement.

## References

- [ADR 018: CUE Template Default Values](018-cue-template-default-values.md) — original design for the `defaults` block and `ExtractDefaults` function
- [Issue #850: Per-field defaults extraction](https://github.com/holos-run/holos-console/issues/850) — parent implementation issue
- [`console/templates/defaults.go`](../../console/templates/defaults.go) — `ExtractDefaults` implementation
- [`proto/holos/console/v1/templates.proto`](../../proto/holos/console/v1/templates.proto) — `TemplateDefaults` message definition
