import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { PlatformTemplateEnabledToggle } from './platform-template-enabled-toggle'
import {
  ENABLED_TOGGLE_ACTIVE_DESCRIPTION,
  ENABLED_TOGGLE_INACTIVE_DESCRIPTION,
} from './platform-template-copy'

describe('PlatformTemplateEnabledToggle', () => {
  describe('description text', () => {
    it('renders the active description when enabled=true', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByText(ENABLED_TOGGLE_ACTIVE_DESCRIPTION),
      ).toBeInTheDocument()
      expect(
        screen.queryByText(ENABLED_TOGGLE_INACTIVE_DESCRIPTION),
      ).not.toBeInTheDocument()
    })

    it('renders the inactive description when enabled=false', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={false}
          canWrite={true}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByText(ENABLED_TOGGLE_INACTIVE_DESCRIPTION),
      ).toBeInTheDocument()
      expect(
        screen.queryByText(ENABLED_TOGGLE_ACTIVE_DESCRIPTION),
      ).not.toBeInTheDocument()
    })
  })

  describe('switch state', () => {
    it('switch reflects data-state=checked when enabled=true', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toHaveAttribute('data-state', 'checked')
    })

    it('switch reflects data-state=unchecked when enabled=false', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={false}
          canWrite={true}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toHaveAttribute('data-state', 'unchecked')
    })
  })

  describe('onChange', () => {
    it('calls onChange(true) when toggled from off to on', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup()
      render(
        <PlatformTemplateEnabledToggle
          enabled={false}
          canWrite={true}
          isUpdating={false}
          onChange={onChange}
        />,
      )
      await user.click(screen.getByRole('switch', { name: /enabled/i }))
      expect(onChange).toHaveBeenCalledTimes(1)
      expect(onChange).toHaveBeenCalledWith(true)
    })

    it('calls onChange(false) when toggled from on to off', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup()
      render(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={false}
          onChange={onChange}
        />,
      )
      await user.click(screen.getByRole('switch', { name: /enabled/i }))
      expect(onChange).toHaveBeenCalledTimes(1)
      expect(onChange).toHaveBeenCalledWith(false)
    })
  })

  describe('permission gating', () => {
    it('disables the switch when canWrite=false', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={false}
          canWrite={false}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toBeDisabled()
    })

    it('does not call onChange when disabled switch is clicked', async () => {
      const onChange = vi.fn()
      const user = userEvent.setup()
      render(
        <PlatformTemplateEnabledToggle
          enabled={false}
          canWrite={false}
          isUpdating={false}
          onChange={onChange}
        />,
      )
      await user.click(screen.getByRole('switch', { name: /enabled/i }))
      expect(onChange).not.toHaveBeenCalled()
    })
  })

  describe('pending state', () => {
    it('disables the switch when isUpdating=true (matches existing pattern)', () => {
      render(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={true}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toBeDisabled()
    })

    it('re-enables the switch once isUpdating flips back to false', () => {
      const { rerender } = render(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={true}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).toBeDisabled()

      rerender(
        <PlatformTemplateEnabledToggle
          enabled={true}
          canWrite={true}
          isUpdating={false}
          onChange={vi.fn()}
        />,
      )
      expect(
        screen.getByRole('switch', { name: /enabled/i }),
      ).not.toBeDisabled()
    })
  })
})
