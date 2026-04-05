# UI Styling Conventions

This guide covers the styling conventions for the Holos Console frontend. Follow these rules whenever adding new UI elements to keep the interface consistent with the existing dark theme.

See ADR 011 (`docs/adrs/011-dark-only-theme.md`) for the decision to use a single dark theme. The authoritative color palette is defined in `frontend/src/app.css`.

## Design System Baseline

- **Component library**: shadcn/ui — check `frontend/src/components/ui/` before building custom components.
- **CSS framework**: Tailwind CSS v4 with the shadcn preset.
- **Theme**: Dark-only (ADR 011). There is no light mode and no theme toggle. The `dark` class is set statically on `<html>` in `index.html` for third-party libraries that gate on it.
- **Design reference**: Linear (linear.app) — deep neutral backgrounds, near-white text, violet accent.

## Color Usage

Always use the CSS custom property tokens defined in `frontend/src/app.css` via Tailwind utility classes. **Never hardcode colors** (no raw `oklch(...)`, `#hex`, or `rgb(...)` values in component code).

| Token class | Purpose |
|---|---|
| `bg-background` | Page canvas (`oklch(0.13 0 0)`, ≈ `#141414`) |
| `bg-card` | Elevated surfaces such as cards and panels (`oklch(0.17 0 0)`) |
| `bg-sidebar` | Sidebar background (`oklch(0.16 0 0)`) |
| `bg-secondary` / `bg-muted` / `bg-accent` | Secondary surfaces (`oklch(0.22 0 0)`) |
| `text-foreground` | Primary text — near-white (`oklch(0.96 0 0)`) |
| `text-muted-foreground` | Secondary / helper text (`oklch(0.60 0 0)`) |
| `text-primary` | Violet accent (`oklch(0.60 0.20 270)`, ≈ `#5E6AD2`) |
| `text-destructive` | Danger / error text |
| `border-border` | Subtle borders (`oklch(1 0 0 / 9%)`) |
| `ring-ring` | Focus rings (violet, same as `primary`) |

### What to avoid

- `dark:` utility prefixes — there is only one theme, so these are dead code.
- Arbitrary color values like `bg-[#1a1a1a]` — use semantic tokens instead.
- Hardcoded opacity on colors — use tokens that already carry the correct opacity (e.g., `border-border`).

## Scrollbar Styling

Custom scrollbar CSS is applied **globally** via `frontend/src/app.css` using `*` selectors for both Firefox (`scrollbar-color`) and WebKit (`*::-webkit-scrollbar`). Scrollbars are styled dark automatically.

**You do not need to add any scrollbar CSS to individual components.** Simply make the element scrollable:

```tsx
// Correct — scrollbars inherit the global dark styling
<div className="overflow-y-auto max-h-96">...</div>

// Also correct
<div className="overflow-auto">...</div>
```

Never override `scrollbar-color`, `::-webkit-scrollbar`, or related properties on individual elements unless you have a documented reason to diverge from the global style.

## Component Selection

1. **Check `frontend/src/components/ui/` first.** All shadcn/ui components that are already used in the project live here. Use them before reaching for Radix primitives directly.
2. **Add a new shadcn component** with `npx shadcn@latest add <component>` when you need a component that isn't in the directory yet.
3. **Use Radix primitives directly** only when shadcn does not provide a suitable wrapper and building one is disproportionate to the need.
4. **Build a custom component** only as a last resort. When you do, follow the same token conventions above.

## Typography

- **Monospace font** (`font-mono`): use for code, CUE templates, YAML output, shell commands, secret values, and any verbatim content.
- **Default sans-serif**: all UI text — labels, descriptions, navigation, headings.
- **Heading hierarchy**: use `text-lg font-semibold` or `text-sm font-medium` following the patterns already in the codebase. Do not introduce arbitrary `text-*` sizes unless they match Tailwind's default scale.

## Spacing

- Follow Tailwind's default spacing scale (`p-4`, `gap-3`, `mb-2`, etc.).
- Use `gap-*` for flex and grid layouts instead of manual margins between children.
- Prefer consistent padding inside cards: `p-4` or `p-6` depending on the content density of the surrounding page.

## Forms

Follow the Display Name + slug pattern documented in `docs/frontend-patterns.md`:

- Display Name field first, machine-safe Name field second.
- Name auto-derived from Display Name via `toSlug()` (`frontend/src/lib/slug.ts`).
- Pair every input with a `<Label>` component from `frontend/src/components/ui/label.tsx`.
- Use `<Input>` and `<Textarea>` from `frontend/src/components/ui/`.
- Add `text-sm text-muted-foreground` helper text beneath inputs.

```tsx
<div className="grid gap-2">
  <Label htmlFor="display-name">Display Name</Label>
  <Input id="display-name" ... />
  <p className="text-sm text-muted-foreground">Human-readable name shown in the UI.</p>
</div>
```

## Dialog / Modal Patterns

- Use `Dialog` from `frontend/src/components/ui/dialog.tsx`.
- Limit form dialogs to `max-w-2xl` to stay readable on standard viewports.
- Always include `DialogHeader` (with `DialogTitle` and `DialogDescription`) and `DialogFooter`.
- Put destructive actions in the footer, styled with `variant="destructive"` on the `Button`.

```tsx
<Dialog>
  <DialogContent className="max-w-2xl">
    <DialogHeader>
      <DialogTitle>Create Organization</DialogTitle>
      <DialogDescription>
        Fill in the details below to create a new organization.
      </DialogDescription>
    </DialogHeader>
    {/* form fields */}
    <DialogFooter>
      <Button variant="outline" onClick={onClose}>Cancel</Button>
      <Button type="submit">Create</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

## Toast Notifications

Use `sonner` directly. The `Toaster` is mounted once at the root with `theme="dark"` — do not mount additional `Toaster` instances.

```tsx
import { toast } from 'sonner'

toast.success('Organization created')
toast.error('Failed to delete secret')
```

See `docs/frontend-patterns.md` for the full toast and copy-to-clipboard patterns.

## Before Adding New UI Elements — Checklist

Work through this checklist before writing any new component or styling:

- [ ] **Does a shadcn/ui component already exist?** Check `frontend/src/components/ui/`. Use it.
- [ ] **Are all colors from semantic tokens?** No raw color values. Use `bg-background`, `text-foreground`, `border-border`, etc.
- [ ] **Is the element scrollable?** Use `overflow-auto` or `overflow-y-auto` — no additional scrollbar CSS needed.
- [ ] **Are `dark:` prefixes absent?** There is one theme. Remove any `dark:` utilities.
- [ ] **Does typography follow the guide?** `font-mono` for code, default sans-serif for UI text.
- [ ] **Does the form follow the Display Name + slug pattern?** See `docs/frontend-patterns.md`.
- [ ] **Are dialogs sized `max-w-2xl` with `DialogHeader` and `DialogFooter`?**
- [ ] **Do toasts use `sonner` directly?**
- [ ] **Does `make test` pass?** Run before committing.
