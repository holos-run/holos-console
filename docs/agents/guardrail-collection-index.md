# Guardrail: Collection Index Pages

**Every resource collection must have an index/listing page at the collection root URL.** When a user navigates to a resource type (e.g., `/orgs/$orgName/projects`), they land on an index page that lists all items in that collection. Resource settings and detail views live at dedicated subroutes, not at the collection root.

## URL Structure

| URL | Purpose |
|-----|---------|
| `/orgs/$orgName/projects` | **Index page** -- lists all projects in the org |
| `/projects/$projectName/secrets` | **Index page** -- lists all secrets in the project |
| `/projects/$projectName/settings` | **Settings page** -- project configuration |
| `/folders/$folderName/templates` | **Index page** -- lists folder-scoped templates |

The index page is the default view when navigating to a resource collection. Settings always live at a dedicated `/settings` subroute, never at the collection root.

## Standard Index Page Structure

Index pages follow a consistent structure:

1. **Card with header** -- collection title and a "Create" action button
2. **Search input** -- filter the collection (see [Searchable Collections](guardrail-searchable-collections.md))
3. **Data table** -- TanStack Table with columns appropriate to the resource type
4. **Pagination** -- shown when results exceed the page size
5. **Empty state** -- message and create action when the collection is empty

## Triggers

Apply this rule when:
- Adding a new resource type that has a collection (list) view
- Adding route files under `frontend/src/routes/`
- Restructuring navigation or URL hierarchy

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| Collection root shows settings | Settings belong at `/settings`; the root should list items |
| Collection root redirects to first item | Users expect to see the full collection, not a single item |
| No index page for a browsable collection | Every collection the user can navigate to needs a listing |

## Related

- [URL Scheme](guardrail-url-scheme.md) -- top-level resource URL conventions
- [Searchable Collections](guardrail-searchable-collections.md) -- all index pages must include search/filter
- [UI Architecture](ui-architecture.md) -- TanStack Router and component library
