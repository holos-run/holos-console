import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SharingPanel, type Grant } from './SharingPanel'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import { vi } from 'vitest'

function grant(principal: string, role: Role, nbf?: bigint, exp?: bigint): Grant {
  return { principal, role, nbf, exp }
}

/**
 * Override window.matchMedia so that queries matching the given pattern
 * return matches=true while all others return matches=false.
 */
function mockMatchMedia(matchPattern: RegExp): () => void {
  const original = window.matchMedia
  window.matchMedia = (query: string) => ({
    matches: matchPattern.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
  return () => {
    window.matchMedia = original
  }
}

describe('SharingPanel', () => {
  describe('rendering grants', () => {
    it('renders user grants', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER), grant('bob@example.com', Role.VIEWER)]}
          roleGrants={[]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.getByText('alice@example.com')).toBeInTheDocument()
      expect(screen.getByText('bob@example.com')).toBeInTheDocument()
    })

    it('renders role grants', () => {
      render(
        <SharingPanel
          userGrants={[]}
          roleGrants={[grant('dev-team', Role.EDITOR), grant('platform-team', Role.OWNER)]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.getByText('dev-team')).toBeInTheDocument()
      expect(screen.getByText('platform-team')).toBeInTheDocument()
    })

    it('shows empty state when no grants', () => {
      render(
        <SharingPanel
          userGrants={[]}
          roleGrants={[]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.getByText(/no sharing grants/i)).toBeInTheDocument()
    })

    it('displays time bounds in read mode', () => {
      const nbf = BigInt(1704067200) // 2024-01-01T00:00:00Z
      const exp = BigInt(1735689600) // 2025-01-01T00:00:00Z

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER, nbf, exp)]}
          roleGrants={[]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      // Should show "from" and "until" text in the secondary line
      expect(screen.getByText(/from/)).toBeInTheDocument()
      expect(screen.getByText(/until/)).toBeInTheDocument()
    })

    it('shows only role when no time bounds', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.getByText('Owner')).toBeInTheDocument()
      expect(screen.queryByText(/from/)).not.toBeInTheDocument()
      expect(screen.queryByText(/until/)).not.toBeInTheDocument()
    })
  })

  describe('owner edit mode', () => {
    it('shows edit button for owners', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument()
    })

    it('does not show edit button for non-owners', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={false}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      expect(screen.queryByRole('button', { name: /edit/i })).not.toBeInTheDocument()
    })

    it('shows save and cancel buttons in edit mode', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
    })

    it('shows datetime fields in edit mode', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(screen.getByLabelText(/not before/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/expires/i)).toBeInTheDocument()
    })
  })

  describe('add grant', () => {
    it('adds a new user grant in edit mode', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /add user/i }))

      // Should show a new empty row
      const principalInputs = screen.getAllByPlaceholderText(/email/i)
      expect(principalInputs.length).toBeGreaterThanOrEqual(1)
    })

    it('adds a new role grant in edit mode', () => {
      render(
        <SharingPanel
          userGrants={[]}
          roleGrants={[grant('dev-team', Role.EDITOR)]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /add role/i }))

      const principalInputs = screen.getAllByPlaceholderText(/role/i)
      expect(principalInputs.length).toBeGreaterThanOrEqual(1)
    })
  })

  describe('remove grant', () => {
    it('removes a grant in edit mode', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER), grant('bob@example.com', Role.VIEWER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      // Remove bob
      const removeButtons = screen.getAllByLabelText(/remove/i)
      fireEvent.click(removeButtons[1]) // second user

      expect(screen.queryByDisplayValue('bob@example.com')).not.toBeInTheDocument()
    })
  })

  describe('save', () => {
    it('calls onSave with updated grants', async () => {
      const onSave = vi.fn().mockResolvedValue(undefined)

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[grant('dev-team', Role.EDITOR)]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(onSave).toHaveBeenCalledWith(
          [{ principal: 'alice@example.com', role: Role.OWNER }],
          [{ principal: 'dev-team', role: Role.EDITOR }],
        )
      })
    })

    it('preserves nbf/exp through save', async () => {
      const onSave = vi.fn().mockResolvedValue(undefined)
      const nbf = BigInt(1704067200)
      const exp = BigInt(1735689600)

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER, nbf, exp)]}
          roleGrants={[]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(onSave).toHaveBeenCalled()
        const savedUsers = onSave.mock.calls[0][0]
        expect(savedUsers[0].nbf).toBe(nbf)
        expect(savedUsers[0].exp).toBe(exp)
      })
    })

    it('disables save button while saving', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={true}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
    })

    it('exits edit mode after successful save', async () => {
      const onSave = vi.fn().mockResolvedValue(undefined)

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /edit/i })).toBeInTheDocument()
      })
    })
  })

  describe('error handling', () => {
    it('keeps edit mode and shows error when save fails', async () => {
      const onSave = vi.fn().mockRejectedValue(new Error('permission denied'))

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          groupGrants={[]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument()
        expect(screen.getByText(/permission denied/i)).toBeInTheDocument()
      })

      // Should still be in edit mode (save/cancel buttons visible)
      expect(screen.getByRole('button', { name: /save/i })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /cancel/i })).toBeInTheDocument()
    })

    it('clears error when user cancels edit mode', async () => {
      const onSave = vi.fn().mockRejectedValue(new Error('server error'))

      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER)]}
          groupGrants={[]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        expect(screen.getByRole('alert')).toBeInTheDocument()
      })

      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  describe('cancel', () => {
    it('reverts changes on cancel', () => {
      render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.OWNER), grant('bob@example.com', Role.VIEWER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      // Remove bob
      const removeButtons = screen.getAllByLabelText(/remove/i)
      fireEvent.click(removeButtons[1])

      // Cancel
      fireEvent.click(screen.getByRole('button', { name: /cancel/i }))

      // Bob should be back
      expect(screen.getByText('bob@example.com')).toBeInTheDocument()
    })
  })

  describe('responsive layout', () => {
    it('stacks grant edit rows vertically on mobile', () => {
      const cleanup = mockMatchMedia(/max-width/)
      try {
        render(
          <SharingPanel
            userGrants={[grant('alice@example.com', Role.OWNER)]}
            roleGrants={[]}
            isOwner={true}
            onSave={vi.fn()}
            isSaving={false}
          />,
        )

        fireEvent.click(screen.getByRole('button', { name: /edit/i }))

        // The principal row should use column direction on mobile
        const emailInput = screen.getByPlaceholderText(/email/i)
        const principalRow = emailInput.closest('.MuiStack-root')
        expect(principalRow).toHaveStyle({ 'flex-direction': 'column' })
      } finally {
        cleanup()
      }
    })
  })
})
