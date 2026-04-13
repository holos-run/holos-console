# Guardrail: Template Defaults Pre-Fill

**Authority:** [ADR 027](../adrs/027-template-defaults-prefill.md) is the single source of truth. This guardrail enforces ADR 027. If this document and ADR 027 disagree, ADR 027 wins and this document must be corrected.

## Rule

The Create Deployment form pre-fills defaultable fields exclusively via the explicit `TemplateService.GetTemplateDefaults` RPC. It **never** reads from the embedded `Template.defaults` field on `ListTemplates` / `GetTemplate` responses. The form tracks a single `isPristine` boolean, issues `GetTemplateDefaults` on every template selection and every **Load defaults** click, and overwrites fields only when pristine (or when the user clicks Load defaults).

### Pristine Tracking

- `isPristine` starts **`true`** on mount.
- It flips to **`false`** on any user edit of a defaultable field.
- It is reset to **`true`** by a successful pre-fill on selection and by the **Load defaults** button.

### Selection Rule

On every template change, the frontend calls `GetTemplateDefaults`.

- If `isPristine` is `true`, the response overwrites all defaultable form fields.
- If `isPristine` is `false`, the response is cached and **no fields are overwritten**.

### Load Defaults Button

The **Load defaults** button always overwrites all defaultable fields, re-issues the RPC (no stale cache), and resets `isPristine` to `true`.

### Defaultable Fields

The defaultable fields are enumerated in [ADR 027 §6](../adrs/027-template-defaults-prefill.md). The frontend holds the only code-level copy of this list as a constant; do not add a second copy here or anywhere else.

### Example-Template Invariant

Every shipped example deployment template **MUST** declare a top-level `defaults: #ProjectInput & { ... }` block with concrete values that mirror the inline `input` defaults.

**Inline `*value | _` defaults on the `input` field are NOT extracted.** A template that expresses defaults only through inline markers will produce no pre-fill values even though it renders correctly. The `defaults` block is the single authoring surface for form pre-fill. Reviewers must reject any example template that omits `defaults` or drifts its values away from the inline `input` defaults.

## Triggers

Apply this rule when:

- Editing the Create Deployment form (`frontend/src/routes/_authenticated/projects/$projectName/deployments/new.tsx`) or the template-change / load-defaults handlers.
- Adding or modifying the `GetTemplateDefaults` RPC, the `TemplateDefaults` proto message, or `ProjectInput` fields that should be pre-filled.
- Authoring or editing an example deployment template (the `defaults` block is required).
- Editing `console/templates/defaults.go` or the server-side `GetTemplateDefaults` handler.
- Resolving merge conflicts in any of the above files.

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| Reading `Template.defaults` from a `ListTemplates` / `GetTemplate` response to pre-fill the form | ADR 027 requires the explicit `GetTemplateDefaults` RPC; embedded `Template.defaults` is compat-only |
| Skipping the `GetTemplateDefaults` call on Load defaults (re-using a cached response) | Load defaults must re-issue the RPC to reflect the template's current authored values |
| Inferring "user has edited" by diffing form values against defaults | The pristine rule is a single explicit boolean; implicit inference has regressed multiple times |
| Overwriting fields on template change when `isPristine` is `false` | The selection rule preserves in-progress user edits unless Load defaults is clicked |
| Shipping an example template that relies on inline `*value | _` markers without a `defaults` block | Inline defaults are not extracted; the form will silently show no pre-fill |
| Adding a second copy of the defaultable fields list anywhere other than the frontend constant | ADR 027 is the only enumeration; duplicate lists drift silently |

## Related

- [ADR 027: Authoritative Template Defaults Pre-Fill Behavior](../adrs/027-template-defaults-prefill.md) — this guardrail's authority.
- [ADR 018: CUE Template Default Values](../adrs/018-cue-template-default-values.md) — superseded for pre-fill behavior; schema decisions still in effect.
- [ADR 025: Per-Field CUE Defaults Extraction](../adrs/025-per-field-defaults-extraction.md) — superseded for pre-fill behavior; extraction correctness rule still honored.
- [Template Service](template-service.md)
- [Guardrail: Template Fields](guardrail-template-fields.md)
