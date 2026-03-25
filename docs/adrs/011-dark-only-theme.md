# ADR 011: Dark-Only Theme — Remove Light/Dark Toggle

## Status

Accepted

## Context

The initial UI shipped with a `ThemeToggle` component in the sidebar footer that allowed users to switch between light and dark modes. The implementation used `next-themes` to drive theme switching, with the Sonner toaster consuming `useTheme()` to follow the user's selection.

Maintaining dual themes adds ongoing cost:

- Every new component and shadcn/ui upgrade must be audited for `dark:` variants.
- The `next-themes` library is an extra dependency maintained solely for the Sonner integration.
- The `ThemeToggle` component itself adds UI surface and test coverage burden.
- Light-mode styles in CSS and component class strings are dead code once the product direction is confirmed as dark-only.

The target aesthetic is inspired by Linear — a consistently dark, high-contrast interface with no user-facing theme controls. All design work, screenshots, and stakeholder feedback reference dark mode. Supporting light mode indefinitely would require maintaining two visual designs that nobody is using.

## Decision

Adopt a dark-only theme permanently:

1. Delete the `ThemeToggle` component and remove it from the sidebar footer.
2. Remove the `next-themes` dependency. Replace `useTheme()` in the Sonner wrapper with a hardcoded `theme="dark"` prop.
3. Promote the `.dark {}` CSS variable values to `:root` defaults in `app.css` and remove the `.dark {}` override block and the `@custom-variant dark` directive.
4. Add `class="dark"` statically to `<html>` in `index.html` so third-party library components that gate styles on the `.dark` class (e.g., Sonner, Radix portal overlays) continue to receive it.
5. Remove all `dark:` Tailwind utility prefixes from local shadcn/ui component copies — they are dead code with a single theme.

## Consequences

**Benefits:**
- Eliminates `next-themes` dependency and its initialization flash.
- Removes `dark:` conditional class overhead from every component.
- Single visual design reduces QA surface.
- No flash of light mode on initial load (CSS variables always resolve to dark values).

**Trade-offs:**
- Users who prefer light mode have no toggle. This is an intentional product decision; the console targets power users who expect a dark interface.
- Future re-introduction of theming would require re-adding `dark:` variants to components. Given the intentional single-theme direction, this is not anticipated.
- Third-party components styled via the `.dark` class selector (Sonner, some Radix portal components) depend on `<html class="dark">` being present statically. This is documented here to avoid confusion when the class appears in the markup without a corresponding runtime toggle.
