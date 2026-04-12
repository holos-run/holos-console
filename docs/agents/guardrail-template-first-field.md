# Guardrail: Template-First Field Ordering

**The Template field must be the first form field in the Create Deployment form (`new.tsx`), before Display Name, Name (slug), and Description.** When no templates are available, the "No templates available" fallback occupies the same first position.

## Rationale

Selecting a template auto-populates all other fields (name, description, image, tag, port, command, args, env) via `handleTemplateChange`. If Template is not first, users fill in details manually before discovering the template overwrites them -- wasting effort and causing confusion. This ordering was established by PR #785 / issue #772.

## Enforcement

A unit test in `-new.test.tsx` asserts that the Template label appears as the first form field in DOM order:

```tsx
it('renders Template as the first form field, before Display Name', () => {
  // ...
  expect(templateIndex).toBe(0) // Template is the very first field
})
```

CI will fail if the ordering regresses. The test covers both the Combobox path (templates exist) and the "No templates available" fallback path (empty template list).

## Common Failure Mode

Branches forked before the Template-first reorder carry `new.tsx` with Template in position 4. When these branches merge, Git's 3-way merge can silently revert the ordering. The unit test catches this -- a merge that reverts the field order will fail CI.

## Triggers

Apply this rule when:
- Editing `frontend/src/routes/_authenticated/projects/$projectName/deployments/new.tsx`
- Adding, removing, or reordering form fields in the Create Deployment form
- Resolving merge conflicts in `new.tsx`

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| Template field after Display Name | Users type a name, then template overwrites it |
| Template field after any other form field | Same auto-populate confusion applies to all fields |
| Removing the field-ordering test | The regression guard must stay in place |

## Related

- [Selection Components](selection-components.md) -- Combobox for dynamic collections, Select for static enumerations
- [Deployment Service](deployment-service.md) -- Kubernetes Deployment CRUD and CUE rendering
- [Guardrail: Template Fields](guardrail-template-fields.md) -- New proto fields must propagate through the render pipeline
