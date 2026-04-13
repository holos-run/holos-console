# ADR 027: Authoritative Template Defaults Pre-Fill Behavior

## Status

Accepted

Supersedes [ADR 018](018-cue-template-default-values.md) and
[ADR 025](025-per-field-defaults-extraction.md) for the purpose of defining the
Create Deployment form pre-fill behavior. The schema decisions in ADR 018 (the
`defaults` block, the `input` field, the `Defaults *ProjectInput` field on
`ResourceSetSpec`) and the per-field extraction correctness rule from ADR 025
remain in effect — this ADR layers the authoritative pre-fill protocol on top
of that foundation.

## Decision

This ADR is the single source of truth for how the Create Deployment form
pre-fills fields from a selected deployment template. All future work, docs, and
reviews reference only ADR 027.

### 1. Dedicated `TemplateService.GetTemplateDefaults` RPC

A dedicated RPC, `TemplateService.GetTemplateDefaults`, returns the
`TemplateDefaults` for a given template. The frontend calls this RPC:

- Every time the user selects a template in the Create Deployment form.
- Every time the user clicks the **Load defaults** button.

The frontend never reads defaults from any other source for the purpose of
pre-filling the form.

### 2. `Template.defaults` remains in list/get responses (compat only)

`ListTemplates` and `GetTemplate` responses continue to carry an embedded
`Template.defaults` field populated by the backend's `ExtractDefaults` path
(unchanged from ADR 025). This is preserved purely so the proto contract stays
additive and existing clients do not break.

**The Create Deployment form ignores the embedded `Template.defaults` field.**
Pre-fill relies exclusively on the explicit `GetTemplateDefaults` RPC so the
behavior is consistently testable via MSW/Vitest mocks and there is exactly one
code path that can regress.

### 3. Pristine tracking rule

The form tracks a single boolean, `isPristine`:

- It starts **`true`** when the form mounts.
- It flips to **`false`** on any user edit of a defaultable field.
- It is reset to **`true`** by:
  - The **Load defaults** button (after the RPC resolves and overwrites fields).
  - A successful pre-fill on template selection (when the form was already
    pristine and the RPC response was applied).

### 4. Selection rule

On every template change the frontend issues `GetTemplateDefaults`:

- **If `isPristine` is `true`**: the response overwrites all defaultable form
  fields with the returned values. `isPristine` stays `true`.
- **If `isPristine` is `false`**: the response is cached but **no fields are
  overwritten**. The user's in-progress edits are preserved.

This rule guarantees that switching templates on a clean form always produces
the new template's defaults, while a form with user edits is never silently
clobbered when the template changes.

### 5. Load defaults button

The **Load defaults** button:

- Always overwrites all defaultable fields, regardless of `isPristine`.
- Re-issues `GetTemplateDefaults` (no stale cache) so the user sees the
  template's current authored defaults.
- Resets `isPristine` to `true` after applying the response.

### 6. Defaultable fields list

The defaultable fields, enumerated exactly once in this ADR, are:

1. `displayName`
2. `name` (slug)
3. `description`
4. `image`
5. `tag`
6. `port`
7. `command`
8. `args`
9. `env`

The guardrail (`docs/agents/guardrail-template-defaults.md`) references this
list by pointer only. The frontend holds the only code-level copy of this list
as a constant (populated in phase 3); any divergence between the ADR list and
the frontend constant is a bug.

### 7. Example-template invariant

Every shipped example deployment template **MUST** declare a top-level
`defaults: #ProjectInput & { ... }` block with concrete values that mirror the
inline `input` defaults. For example:

```cue
defaults: #ProjectInput & {
    name:        "httpbin"
    displayName: "httpbin"
    description: "A simple HTTP Request & Response Service"
    image:       "ghcr.io/mccutchen/go-httpbin"
    tag:         "2.21.0"
    port:        8080
}

input: #ProjectInput & {
    name:        *defaults.name        | _
    displayName: *defaults.displayName | _
    description: *defaults.description | _
    image:       *defaults.image       | _
    tag:         *defaults.tag         | _
    port:        *defaults.port        | _
}
```

**Inline `*` defaults on the `input` field are NOT extracted.** This is a
deliberate simplification: the top-level `defaults` block is the single
authoring surface for form pre-fill. A template that expresses defaults only
through inline `*value | _` markers on `input` will produce **no**
extractable defaults and the form will not pre-fill, even though the template
renders correctly. This trap has bitten every prior attempt — the example
templates looked self-documenting but produced no extractable defaults at the
RPC layer.

Reviewers must reject any example template that omits the `defaults` block or
that drifts the `defaults` values out of sync with the inline `input` defaults.

## Why This Has Regressed

This behavior has regressed multiple times across the ADR 018 and ADR 025
iterations. The recurring failure modes:

1. **Frontend read from the embedded `Template.defaults` field** directly in
   the form component. This path was subject to list-cache staleness and was
   painful to mock in component tests. The explicit `GetTemplateDefaults` RPC
   makes the call site obvious and the mock surface small.

2. **Pristine tracking was implicit** — the form inferred "user has edited"
   from diffing current values against defaults, which was fragile across
   template switches. The single explicit `isPristine` boolean, flipped by user
   edits and reset only by pre-fill events, is both simpler and easier to
   assert in tests.

3. **The Load defaults button was missing or overloaded.** Users who wanted to
   reset the form to a template's defaults had no safe escape hatch after
   editing. The dedicated button, which always overwrites and re-issues the
   RPC, is now a first-class affordance.

4. **Example templates relied on inline `*` defaults only.** Per-field
   extraction (ADR 025) walks the `defaults` block; inline `input` defaults
   are never visited. A template missing the `defaults` block produces no
   pre-fill values. This ADR makes the `defaults` block mandatory for all
   example templates and forbids the inline-only pattern.

## Consequences

### Positive

- **Single source of truth.** One ADR describes the complete pre-fill
  behavior. Future authors reference only this document.

- **Explicit RPC boundary.** `GetTemplateDefaults` is the single call site for
  form pre-fill. MSW/Vitest mocks have one endpoint to stub, and regressions
  have one place to surface.

- **Deterministic pristine semantics.** The `isPristine` boolean is easy to
  reason about in both code and tests; its transitions are exhaustively
  enumerated above.

- **User-edit preservation.** A user who has started editing the form is never
  silently overwritten by a template change. The Load defaults button provides
  the explicit override.

- **Proto stays additive.** `Template.defaults` remains in list/get responses
  for backwards compatibility, so older generated clients continue to compile
  and deserialize. The new RPC is purely additive.

### Negative

- **Two backend code paths** (`ExtractDefaults` serving both
  `Template.defaults` and `GetTemplateDefaults`) for the transition period.
  The duplication is trivial (both call the same extractor) and acceptable.

- **Extra RPC round-trip on template selection.** The form issues one
  `GetTemplateDefaults` call per template change rather than reading from the
  already-fetched `ListTemplates` response. The latency cost is negligible
  (single-template CUE evaluation is millisecond-scale) and is the price of a
  testable, explicit boundary.

## References

- [ADR 018: CUE Template Default Values](018-cue-template-default-values.md) —
  superseded for pre-fill behavior; its schema decisions (the `defaults`
  block, `input` field wiring, `ResourceSetSpec.Defaults`) remain in effect.
- [ADR 025: Per-Field CUE Defaults Extraction](025-per-field-defaults-extraction.md) —
  superseded for pre-fill behavior; its per-field extraction correctness rule
  remains in effect on the backend extractor.
- [Guardrail: Template Defaults Pre-Fill](../agents/guardrail-template-defaults.md) —
  enforcement surface for this ADR.
- [Parent plan: #925](https://github.com/holos-run/holos-console/issues/925)
- [This ADR's implementation issue: #926](https://github.com/holos-run/holos-console/issues/926)
