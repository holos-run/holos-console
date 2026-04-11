# Selection Components

All selection inputs that present a **dynamic collection of items** MUST use the searchable `Combobox` component (`frontend/src/components/ui/combobox.tsx`), not the basic `Select` component. This follows the Linear-style pattern: text input for filtering, keyboard-navigable list, single-select.

Use the basic `Select` only for **small, static enumerations** (e.g., 2-4 fixed choices such as "Value / SecretRef / ConfigMapRef" or "Viewer / Editor / Owner"). When in doubt, use `Combobox`.

## Examples

- Template selection in the Create Deployment form -> `Combobox` (dynamic list from K8s)
- Organization selection in the Create Project dialog -> `Combobox` (dynamic list from K8s)
- Env var source type ("Value / SecretRef / ConfigMapRef") -> `Select` (3 static choices)
- Role picker ("Viewer / Editor / Owner") in the sharing panel -> `Select` (3 static choices)

## Related

- [UI Styling Conventions](ui-styling.md) — Broader component and styling guidelines
- [UI Architecture](ui-architecture.md) — Frontend tech stack and component library
