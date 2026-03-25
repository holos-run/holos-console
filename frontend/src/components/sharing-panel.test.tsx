import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { SharingPanel, type Grant } from './sharing-panel'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { vi } from 'vitest'

function grant(principal: string, role: Role, nbf?: bigint, exp?: bigint): Grant {
  return { principal, role, nbf, exp }
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

      expect(screen.getByText(/not before/i)).toBeInTheDocument()
      expect(screen.getByText(/expires/i)).toBeInTheDocument()
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
          roleGrants={[]}
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
          roleGrants={[]}
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

  describe('expiration UTC behavior', () => {
    it('stores midnight UTC when a date is selected for user exp', async () => {
      const onSave = vi.fn().mockResolvedValue(undefined)
      const { container } = render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.VIEWER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      // datetime-local inputs in order: user nbf (0), user exp (1)
      const datetimeInputs = container.querySelectorAll('input[type="datetime-local"]')
      const expInput = datetimeInputs[1]
      fireEvent.change(expInput, { target: { value: '2026-04-15T14:30' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        const savedUsers = onSave.mock.calls[0][0]
        const expectedTs = BigInt(Math.floor(new Date('2026-04-15T00:00:00Z').getTime() / 1000))
        expect(savedUsers[0].exp).toBe(expectedTs)
      })
    })

    it('pre-populates exp with non-empty default on focus when unset', () => {
      const { container } = render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.VIEWER)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      const datetimeInputs = container.querySelectorAll('input[type="datetime-local"]')
      const expInput = datetimeInputs[1]
      expect(expInput).toHaveValue('')
      fireEvent.focus(expInput)
      expect(expInput).not.toHaveValue('')
    })

    it('displays UTC midnight timestamp correctly in exp field', () => {
      const exp = BigInt(Math.floor(new Date('2026-01-01T00:00:00Z').getTime() / 1000))
      const { container } = render(
        <SharingPanel
          userGrants={[grant('alice@example.com', Role.VIEWER, undefined, exp)]}
          roleGrants={[]}
          isOwner={true}
          onSave={vi.fn()}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      const datetimeInputs = container.querySelectorAll('input[type="datetime-local"]')
      const expInput = datetimeInputs[1]
      expect(expInput).toHaveValue('2026-01-01T00:00')
    })

    it('stores midnight UTC when a date is selected for role exp', async () => {
      const onSave = vi.fn().mockResolvedValue(undefined)
      const { container } = render(
        <SharingPanel
          userGrants={[]}
          roleGrants={[grant('dev-team', Role.EDITOR)]}
          isOwner={true}
          onSave={onSave}
          isSaving={false}
        />,
      )

      fireEvent.click(screen.getByRole('button', { name: /edit/i }))

      const datetimeInputs = container.querySelectorAll('input[type="datetime-local"]')
      const expInput = datetimeInputs[1]
      fireEvent.change(expInput, { target: { value: '2026-06-30T09:15' } })
      fireEvent.click(screen.getByRole('button', { name: /save/i }))

      await waitFor(() => {
        const savedRoles = onSave.mock.calls[0][1]
        const expectedTs = BigInt(Math.floor(new Date('2026-06-30T00:00:00Z').getTime() / 1000))
        expect(savedRoles[0].exp).toBe(expectedTs)
      })
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
})
