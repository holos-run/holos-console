# Guardrail: Pages Over Modals

**Create and update UIs use dedicated route pages, not modal dialogs.** Modals
are reserved for brief confirmations (delete, discard) and lightweight actions
that require only a name field (clone).

## Rules

1. Resource creation goes to a `/new` page (e.g., `/templates/new`).
2. Resource editing goes to a `/$name/edit` page (e.g., `/templates/$name/edit`).
3. Both pages use the standard Card layout with full viewport width.
4. Modals are acceptable only for actions with no textarea fields and at most one
   or two short inputs (name, confirmation checkbox).

## Why

Modals have fixed viewport constraints that break when content grows -- CUE
template textareas, example loading, and multi-field forms overflow or require
internal scrolling that fights the browser. Dedicated pages provide full
viewport space, browser back-button navigation, bookmarkable URLs, and natural
content flow.

## Triggers

Apply this rule when:
- Adding a new resource creation or editing UI
- Creating route files under `frontend/src/routes/`
- Adding forms with textareas or multi-field layouts

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| Dialog/modal for a create form with textarea fields | Content overflows fixed modal viewport; use a `/new` page |
| Dialog/modal for an edit form | Edit forms grow with the resource; use an `/$name/edit` page |
| Sheet/drawer for multi-field creation | Same viewport constraints as modals; use a dedicated page |

## Related

- [Collection Index Pages](guardrail-collection-index.md) -- URL structure for resource collections
- [URL Scheme](guardrail-url-scheme.md) -- Top-level resource URL conventions
- [UI Architecture](ui-architecture.md) -- TanStack Router and component library
