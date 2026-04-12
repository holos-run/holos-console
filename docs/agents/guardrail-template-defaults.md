# Guardrail: Template Defaults Pre-Fill

**The backend must extract each `ProjectInput` field independently from the CUE `defaults` block (per-field extraction). The frontend must pre-fill all Create Deployment form fields from the extracted `TemplateDefaults` when a template is selected.** A non-concrete field must not prevent extraction of concrete siblings. All pre-filled fields must update when the user switches templates.

## Rationale

This feature has regressed multiple times. The original implementation (ADR 018) used whole-block `MarshalJSON()` on the entire `defaults` CUE value. If any single field was non-concrete (e.g., `env: _`), the marshal call failed for the entire value and silently dropped all defaults -- even fields like `name`, `image`, and `tag` that were perfectly concrete. ADR 025 replaced this with per-field extraction to eliminate the silent data loss.

On the frontend side, `handleTemplateChange` in the Create Deployment form must map every `TemplateDefaults` field to its corresponding form field. Missing a field means the form shows stale values from a previously selected template or empty values when the template intended to provide defaults.

## Enforcement

### Backend

`console/templates/defaults_test.go` includes a "mixed concrete and non-concrete fields" test case that verifies concrete fields are extracted while non-concrete fields are silently omitted:

```go
t.Run("mixed concrete and non-concrete fields returns partial defaults", func(t *testing.T) {
    cueSource := `
defaults: #ProjectInput & {
    name:  "httpbin"
    image: string  // non-concrete — constrained but no value
    tag:   "2.21.0"
    port:  8080
}
`
    d, err := ExtractDefaults(cueSource)
    // ... asserts name="httpbin", image="", tag="2.21.0", port=8080
})
```

### Frontend

`frontend/src/routes/_authenticated/projects/$projectName/deployments/-new.test.tsx` includes comprehensive defaults pre-fill regression tests (added in issue #853):

- `selecting a template with full defaults pre-fills all form fields` -- verifies every field is populated
- `selecting a different template updates all fields to new defaults` -- verifies switching templates replaces all values
- `selecting a template with partial defaults leaves other fields at default values` -- verifies missing defaults do not corrupt other fields

CI fails if either the backend extraction or the frontend pre-fill regresses.

## Common Failure Mode

Reverting `ExtractDefaults()` in `console/templates/defaults.go` to whole-block `MarshalJSON()` on the entire `defaults` value instead of per-field extraction. This causes all defaults to silently disappear when any single field is non-concrete.

On the frontend, omitting a field from the `handleTemplateChange` callback means that field retains its value from a previously selected template instead of updating to the new template's default.

## Triggers

Apply this rule when:

- Editing `console/templates/defaults.go` (backend extraction logic)
- Editing `frontend/src/routes/_authenticated/projects/$projectName/deployments/new.tsx` (the `handleTemplateChange` callback)
- Adding new fields to the `TemplateDefaults` proto message in `proto/holos/console/v1/templates.proto`
- Adding new fields to `ProjectInput` in `api/v1alpha2/types.go` that should have default values
- Resolving merge conflicts in any of the above files

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| `defaultsVal.MarshalJSON()` on the whole `defaults` value | One non-concrete field drops all defaults silently |
| `handleTemplateChange` that does not set every `TemplateDefaults` field | Stale values persist from previously selected template |
| Removing the "mixed concrete and non-concrete" backend test | The per-field extraction regression guard must stay |
| Removing the frontend defaults pre-fill regression tests | The form pre-fill regression guard must stay |
| Using `json.Unmarshal` on the entire defaults block into `ProjectInput` | Same whole-block failure mode as `MarshalJSON()` |

## Related

- [Template Service](template-service.md) -- Deployment Defaults section describes the extraction mechanism
- [Guardrail: Template Fields](guardrail-template-fields.md) -- New proto fields must propagate through defaults extraction
- [ADR 018: CUE Template Default Values](../adrs/018-cue-template-default-values.md) -- Original design for the `defaults` block
- [ADR 025: Per-Field CUE Defaults Extraction](../adrs/025-per-field-defaults-extraction.md) -- Per-field extraction design replacing whole-block marshaling
