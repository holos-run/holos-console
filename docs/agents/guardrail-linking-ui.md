# Guardrail: Linked Templates UI

The "Linked Platform Templates" section must always render on project template
create and edit pages, regardless of whether linkable ancestor templates exist.

## Rules

1. Never conditionally hide the section based on `linkableTemplates.length`.
2. When no linkable templates exist, show an empty state message.
3. The preview pane must pass linked templates to `useRenderTemplate` for unified output.
4. Regression tests in `-linking-regression.test.tsx` must pass.

## Why

This feature has been lost multiple times because conditional rendering made it
invisible in environments without pre-existing platform templates. The guardrail
tests and this document prevent that regression.

## Related

- [Guardrail: Template Linking](guardrail-template-linking.md) -- Backend linking model
- [Template Service](template-service.md) -- Render set formula
