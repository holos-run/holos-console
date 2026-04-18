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

// RED (HOL-588): The verbose REQUIRE/EXCLUDE copy previously rendered inside
// each `<SelectItem>` and overflowed the popover. The tooltip on a button
// trigger next to the Kind `<Label>` is now the single surface for that copy.
// These tests pin:
//
//   1. Kind <SelectItem> rows render only the short `REQUIRE` / `EXCLUDE`
//      label and do not carry any fragment of the long rule descriptions.
//   2. A focusable `<button>` sits next to the Kind label with
//      `aria-label="Explain REQUIRE and EXCLUDE"`, and its tooltip surfaces
//      text from both REQUIRE_RULE_DESCRIPTION and EXCLUDE_RULE_DESCRIPTION.
//
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
    projectPattern: '*',
    deploymentPattern: '*',
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
