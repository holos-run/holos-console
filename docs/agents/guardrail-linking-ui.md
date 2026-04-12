# Guardrail: Linked Templates UI

The "Linked Platform Templates" section must always render on project template
create and edit pages, regardless of whether linkable ancestor templates exist.

## Rules

1. Never conditionally hide the section based on `linkableTemplates.length`.
2. When no linkable templates exist, show an empty state message.
3. The preview pane must pass linked templates to `useRenderTemplate` for grouped output (platform and project resources displayed separately).
4. Regression tests in `-linking-regression.test.tsx` must pass.
5. When a linkable template has releases, the linking dialog must show a version selector dropdown next to the checkbox allowing the user to pin to a specific version or select "Latest (auto-update)".
6. On the detail page, linked template pill badges must show a version status indicator: a green check when up to date, an amber arrow when an update is available, or "unversioned" when the template has no releases.

## Why

This feature has been lost multiple times because conditional rendering made it
invisible in environments without pre-existing platform templates. The guardrail
tests and this document prevent that regression.

## Related

- [Guardrail: Template Linking](guardrail-template-linking.md) -- Backend linking model and version constraints
- [Template Service](template-service.md) -- Render set formula and versioning
- [ADR 024](../adrs/024-template-versioning.md) -- Versioning, releases, and version constraints design
