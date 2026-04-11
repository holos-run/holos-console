# UI Styling Conventions

The application uses a dark-only theme (ADR 011). All new UI elements must follow the conventions in `docs/ui-styling-guide.md`, which covers:

- Color palette usage — always use semantic CSS token classes (e.g., `bg-background`, `text-foreground`, `border-border`); never hardcode colors
- Scrollbar styling — global CSS in `frontend/src/app.css` handles scrollbars; new scrollable elements need only `overflow-auto` or `overflow-y-auto`
- Component selection — prefer shadcn/ui components in `frontend/src/components/ui/`
- Typography — `font-mono` for code/CUE content, default sans-serif for UI text
- Spacing — Tailwind default scale; `gap-*` for flex/grid layouts
- Form patterns — Display Name + slug, `Label` + `Input`/`Textarea` pairs
- Dialog/modal patterns — shadcn `Dialog`, `max-w-2xl` for forms, `DialogHeader` + `DialogFooter`
- Toast notifications — `sonner` with `theme="dark"` (already configured at root)

The guide also includes a "Before adding new UI elements" checklist. Consult it before writing any new component.

## Related

- [Selection Components](selection-components.md) — Combobox vs Select decision rule
- [UI Architecture](ui-architecture.md) — Frontend tech stack
