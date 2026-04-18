import { act, render, screen, within } from '@testing-library/react'
import userEvent, { PointerEventsCheckLevel } from '@testing-library/user-event'
import { describe, it, expect, vi } from 'vitest'
import { RuleEditor } from './RuleEditor'
import type { RuleDraft } from './rule-draft'
import { TemplatePolicyKind } from '@/queries/templatePolicies'
import {
  REQUIRE_RULE_DESCRIPTION,
  EXCLUDE_RULE_DESCRIPTION,
} from '@/components/platform-template-copy'

// Polyfills for jsdom — Radix Select / Tooltip use pointer capture APIs that
// are absent in jsdom and must exist for `userEvent` interactions to dispatch
// cleanly. We also polyfill scrollIntoView (already installed globally by
// src/test/setup.ts but repeated here so the test is readable in isolation).
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

function makeRule(overrides: Partial<RuleDraft> = {}): RuleDraft {
  return {
    kind: TemplatePolicyKind.REQUIRE,
    templateKey: '',
    versionConstraint: '',
    ...overrides,
  }
}

describe('RuleEditor — Kind help tooltip (HOL-588)', () => {
  it('renders SelectItem rows with only the short REQUIRE/EXCLUDE labels', async () => {
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })

    render(
      <RuleEditor
        rules={[makeRule()]}
        onChange={vi.fn()}
        linkableTemplates={[]}
      />,
    )

    const row = screen.getByTestId('rule-editor-row-0')
    const trigger = within(row).getByRole('combobox', { name: /rule 1 kind/i })
    await user.click(trigger)

    // Radix renders SelectItem with role="option" only when the popover is
    // open, so we match by role to avoid false positives from hidden nodes.
    const options = await screen.findAllByRole('option')
    const labels = options.map((o) => o.textContent?.trim() ?? '')
    expect(labels).toEqual(expect.arrayContaining(['REQUIRE', 'EXCLUDE']))

    // Neither option row should carry the long description copy anymore.
    // We assert against a stable fragment of REQUIRE_RULE_DESCRIPTION so the
    // test stays meaningful even if the copy is edited lightly.
    for (const option of options) {
      expect(option.textContent ?? '').not.toMatch(
        /include this platform template in the effective ref set/i,
      )
      expect(option.textContent ?? '').not.toMatch(
        /remove this platform template from the effective ref set/i,
      )
    }
  })

  it('exposes a focusable tooltip trigger next to the Kind label with both descriptions', async () => {
    render(
      <RuleEditor
        rules={[makeRule()]}
        onChange={vi.fn()}
        linkableTemplates={[]}
      />,
    )

    const row = screen.getByTestId('rule-editor-row-0')
    const triggerButton = within(row).getByRole('button', {
      name: /explain require and exclude/i,
    })
    // AC: must be a focusable <button type="button"> (not a <span>) so it
    // participates in Tab order without extra tabIndex and does not submit
    // the surrounding form when Enter is pressed.
    expect(triggerButton.tagName).toBe('BUTTON')
    expect(triggerButton).toHaveAttribute('type', 'button')
    // A <button> has tabIndex 0 by default; asserting the button is focusable
    // via `.focus()` pins the observable contract without depending on
    // user-event's synthetic tab order in jsdom. Wrapping in act silences
    // Radix Tooltip's internal state update that fires on focus.
    act(() => {
      triggerButton.focus()
    })
    expect(triggerButton).toHaveFocus()

    // Radix Tooltip opens on focus and portals the content; findByRole
    // handles the async mount.
    const tooltip = await screen.findByRole('tooltip')
    expect(tooltip).toHaveTextContent(REQUIRE_RULE_DESCRIPTION)
    expect(tooltip).toHaveTextContent(EXCLUDE_RULE_DESCRIPTION)
  })
})

// HOL-598: Attachment is now expressed exclusively via TemplatePolicyBinding
// rows. The glob pattern inputs on each rule have been removed so admins stop
// authoring opaque globs. These tests pin the new contract: the RuleEditor
// MUST NOT render a "Project pattern" or "Deployment pattern" input, and any
// `projectPattern`/`deploymentPattern` field carried on a pre-existing rule is
// ignored at render time.
describe('RuleEditor — glob target inputs removed (HOL-598)', () => {
  it('does not render Project pattern or Deployment pattern inputs', () => {
    render(
      <RuleEditor
        rules={[makeRule()]}
        onChange={vi.fn()}
        linkableTemplates={[]}
      />,
    )

    const row = screen.getByTestId('rule-editor-row-0')
    expect(within(row).queryByLabelText(/project pattern/i)).toBeNull()
    expect(within(row).queryByLabelText(/deployment pattern/i)).toBeNull()
    expect(within(row).queryByText(/project pattern/i)).toBeNull()
    expect(within(row).queryByText(/deployment pattern/i)).toBeNull()
  })

  it('does not emit a projectPattern/deploymentPattern when adding a new rule', async () => {
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })
    const onChange = vi.fn<(rules: RuleDraft[]) => void>()

    render(
      <RuleEditor
        rules={[]}
        onChange={onChange}
        linkableTemplates={[]}
      />,
    )

    await user.click(screen.getByRole('button', { name: /add rule/i }))

    expect(onChange).toHaveBeenCalledTimes(1)
    const emitted = onChange.mock.calls[0][0]
    expect(emitted).toHaveLength(1)
    const draft = emitted[0] as Partial<RuleDraft> & {
      projectPattern?: string
      deploymentPattern?: string
    }
    // The RuleDraft shape is pruned; the add-rule handler must not introduce
    // either glob field on the new draft.
    expect(draft.projectPattern).toBeUndefined()
    expect(draft.deploymentPattern).toBeUndefined()
  })
})
