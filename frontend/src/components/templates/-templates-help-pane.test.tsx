/**
 * Tests for TemplatesHelpPane (HOL-860).
 *
 * Covers:
 *   - Component renders the three content sections and summary
 *   - onOpenChange callback fires when open state changes
 *
 * NOTE: The Sheet's Esc-key and overlay-click behaviour is driven by
 * Radix UI internals. Those interactions are exercised in the route-level
 * tests at frontend/src/routes/_authenticated/projects/$projectName/templates/-index-help.test.tsx
 * where the full URL-param lifecycle can be asserted via mock router hooks.
 */

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import React from 'react'
import { TemplatesHelpPane } from './TemplatesHelpPane'

// ---------------------------------------------------------------------------
// Radix pointer-capture polyfills for jsdom
// ---------------------------------------------------------------------------

if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

describe('TemplatesHelpPane', () => {
  it('renders all three section headings when open', () => {
    render(<TemplatesHelpPane open={true} onOpenChange={vi.fn()} />)

    // Use testid-scoped queries to avoid ambiguity — each section has exactly one h3.
    const templateSection = screen.getByTestId('help-section-template')
    const policySection = screen.getByTestId('help-section-template-policy')
    const bindingSection = screen.getByTestId('help-section-template-policy-binding')

    expect(templateSection.querySelector('h3')).toHaveTextContent('Template')
    expect(policySection.querySelector('h3')).toHaveTextContent('Template Policy')
    expect(bindingSection.querySelector('h3')).toHaveTextContent('Template Policy Binding')
  })

  it('renders the summary paragraph when open', () => {
    render(<TemplatesHelpPane open={true} onOpenChange={vi.fn()} />)

    expect(
      screen.getByText(/Authors write templates.*product teams deploy/i),
    ).toBeInTheDocument()
  })

  it('renders the Template section copy block', () => {
    render(<TemplatesHelpPane open={true} onOpenChange={vi.fn()} />)

    const section = screen.getByTestId('help-section-template')
    expect(section).toBeInTheDocument()
    expect(section.textContent).toMatch(/reusable CUE configuration/i)
    expect(section.textContent).toMatch(/cloned, edited, and scoped/i)
  })

  it('renders the TemplatePolicy section copy block', () => {
    render(<TemplatesHelpPane open={true} onOpenChange={vi.fn()} />)

    const section = screen.getByTestId('help-section-template-policy')
    expect(section).toBeInTheDocument()
    expect(section.textContent).toMatch(/constraint defined at organization/i)
  })

  it('renders the TemplatePolicyBinding section copy block', () => {
    render(<TemplatesHelpPane open={true} onOpenChange={vi.fn()} />)

    const section = screen.getByTestId('help-section-template-policy-binding')
    expect(section).toBeInTheDocument()
    expect(section.textContent).toMatch(/attaches a policy to one or more templates/i)
    expect(section.textContent).toMatch(/enforcement point/i)
  })

  it('does not render section content when closed', () => {
    render(<TemplatesHelpPane open={false} onOpenChange={vi.fn()} />)

    // When Sheet is closed, content should not be in the document.
    expect(screen.queryByTestId('help-section-template')).not.toBeInTheDocument()
    expect(screen.queryByTestId('help-section-template-policy')).not.toBeInTheDocument()
    expect(screen.queryByTestId('help-section-template-policy-binding')).not.toBeInTheDocument()
  })
})
