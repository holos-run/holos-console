# Frontend Patterns

Common patterns used across the React frontend. Follow these when adding new features to keep the UI consistent and testable.

## Copy to Clipboard

Use `navigator.clipboard.writeText` followed by `toast.success('Copied to clipboard')`. This combination is the standard for all copy actions in this codebase.

```tsx
import { toast } from 'sonner'

const handleCopy = (value: string) => {
  navigator.clipboard.writeText(value)
  toast.success('Copied to clipboard')
}
```

The `Toaster` component is mounted once at the root in `frontend/src/routes/__root.tsx` and uses the custom wrapper at `frontend/src/components/ui/sonner.tsx` (dark theme, lucide icons).

### Testing copy actions

Mock `sonner` and `navigator.clipboard` in unit tests, then assert both were called:

```tsx
import { toast } from 'sonner'
import { vi } from 'vitest'

vi.mock('sonner', () => ({
  toast: { success: vi.fn() },
}))

it('copy button copies the value and shows a toast', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.assign(navigator, { clipboard: { writeText } })

  // render component ...
  fireEvent.click(screen.getByLabelText('copy'))

  await waitFor(() => expect(writeText).toHaveBeenCalledWith('expected value'))
  expect(toast.success).toHaveBeenCalledWith('Copied to clipboard')
})
```

## Toast Notifications

All toast notifications use `sonner`. Import directly from the package:

```tsx
import { toast } from 'sonner'

toast.success('Operation succeeded')
toast.error('Something went wrong')
```

Do not import from `@/components/ui/sonner` — that file exports the `Toaster` component only (used once in the root layout). The `toast` function always comes from `'sonner'` directly.

## Browser Automation (agent-browser)

**Use `eval`-based clicking for React buttons**, not `agent-browser click`.

`agent-browser click <selector>` uses CDP's `Input.dispatchMouseEvent` which does **not** bubble through React's synthetic event system. React attaches event handlers to the document root via event delegation, so CDP mouse events that bypass bubbling will not trigger React `onClick` handlers.

```bash
# Wrong — CDP click, React onClick never fires
$AB click '[aria-label="copy"]'

# Correct — DOM .click() bubbles normally through React's event system
$AB eval "document.querySelector('[aria-label=\"copy\"]')?.click()"

# Correct — find by text and click
$AB eval "
  for (const b of document.querySelectorAll('button')) {
    if (b.textContent.trim() === 'Create Organization' && !b.disabled) { b.click(); break; }
  }
"
```

This applies to any React `onClick` handler. For non-React interactions (scrolling, focus, native inputs), `agent-browser click` / `agent-browser fill` works fine.
