# ADR 011: Dark-only theme, no toggle

**Status:** Accepted
**Date:** 2026-03-24

## Context

The UI shipped with a light/dark mode toggle backed by `localStorage` and a `.dark`
CSS class on `<html>`. Supporting two themes doubles the surface area to maintain:
every new component must be verified in both modes, palette decisions must work in
both contexts, and the toggle itself clutters the sidebar footer.

Linear (linear.app) is the design reference for Holos Console. Linear is a
dark-first product — no theme toggle is exposed to users. Their palette uses deep
neutral backgrounds (roughly `#141414–#1a1a1a`), near-white text, and a single
violet accent. Their sidebar sits a shade lighter than the canvas.

## Decision

Lock the application to a single dark theme inspired by Linear's aesthetic:

1. **One theme only.** The `:root` CSS custom properties carry the dark palette
   values directly. There is no `.dark {}` override block and no `@custom-variant
   dark` directive. The `dark` class is set statically on `<html>` in `index.html`
   so any third-party library components that gate on the class still receive it.

2. **Remove the toggle.** `ThemeToggle` and `theme-toggle.tsx` are deleted.
   `localStorage` theme persistence is gone. There is no JavaScript toggling of the
   `dark` class at runtime.

3. **Remove `dark:` utility classes.** All `dark:` Tailwind prefixes in the local
   shadcn/ui component copies are removed. Since there is only one theme, these
   prefixes are dead code. The dark-mode values are promoted to unconditional
   classes where they differ from the former light-mode defaults.

4. **Palette.** The design tokens target the Linear aesthetic:
   - Page background: `oklch(0.13 0 0)` (≈ `#141414`)
   - Card / elevated surface: `oklch(0.17 0 0)` (≈ `#1c1c1c`)
   - Sidebar: `oklch(0.16 0 0)` (slightly lighter than canvas)
   - Foreground: `oklch(0.96 0 0)` (near-white)
   - Muted foreground: `oklch(0.60 0 0)` (secondary text)
   - Accent (violet): `oklch(0.60 0.20 270)` (≈ Linear's `#5E6AD2`)
   - Borders: `oklch(1 0 0 / 9%)` (barely-visible, ~8–9% white opacity)
   - Border radius: `0.375rem` (tighter than the former `0.625rem`)

5. **Sonner toaster** is hardcoded to `theme="dark"` so toast notifications match
   the app theme without depending on `next-themes`.

## Consequences

- The `next-themes` dependency is removed from `package.json`.
- Maintenance burden is halved: every UI decision is made once for dark mode only.
- Users who strongly prefer light mode cannot override the theme in-app. This is an
  explicit trade-off: we prioritise a single polished experience over personal
  preference at this stage of the product.
- Future re-introduction of a light theme would require reversing this ADR and
  restoring the `.dark {}` CSS block pattern.
