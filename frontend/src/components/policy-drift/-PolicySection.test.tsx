import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { PolicySection, PolicyDriftBadge } from './PolicySection'
import type { PolicyState, LinkedTemplateRef } from '@/gen/holos/console/v1/policy_state_pb'
import { TemplateScope } from '@/gen/holos/console/v1/policy_state_pb'

function makeRef(partial: Partial<LinkedTemplateRef>): LinkedTemplateRef {
  return {
    $typeName: 'holos.console.v1.LinkedTemplateRef',
    scope: TemplateScope.ORGANIZATION,
    scopeName: 'acme',
    name: 'base',
    versionConstraint: '',
    ...partial,
  } as LinkedTemplateRef
}

function makeState(partial: Partial<PolicyState>): PolicyState {
  return {
    $typeName: 'holos.console.v1.PolicyState',
    appliedSet: [],
    currentSet: [],
    addedRefs: [],
    removedRefs: [],
    drift: false,
    hasAppliedState: true,
    ...partial,
  } as PolicyState
}

describe('PolicyDriftBadge', () => {
  it('renders the badge with aria-label and Policy Drift text', () => {
    render(<PolicyDriftBadge />)
    const badge = screen.getByTestId('policy-drift-badge')
    expect(badge).toBeInTheDocument()
    expect(badge).toHaveAttribute('aria-label', 'Policy drift')
    expect(screen.getByText(/policy drift/i)).toBeInTheDocument()
  })
})

describe('PolicySection', () => {
  it('renders a loading skeleton when isPending is true', () => {
    render(<PolicySection isPending={true} />)
    expect(screen.getByTestId('policy-section')).toBeInTheDocument()
    // No drift badge while loading.
    expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
  })

  it('renders a loading skeleton when state is undefined', () => {
    render(<PolicySection state={undefined} />)
    expect(screen.getByTestId('policy-section')).toBeInTheDocument()
    expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
  })

  it('renders the error message when error is present', () => {
    render(<PolicySection error={new Error('rpc failed: internal')} />)
    expect(screen.getByText(/rpc failed: internal/)).toBeInTheDocument()
  })

  it('renders the never-applied note when hasAppliedState is false', () => {
    const state = makeState({
      hasAppliedState: false,
      currentSet: [makeRef({ name: 'base' })],
    })
    render(<PolicySection state={state} />)
    expect(screen.getByTestId('policy-never-applied')).toBeInTheDocument()
    // Drift badge is not rendered when hasAppliedState is false, regardless
    // of the drift flag, because un-initialized is not drifted.
    expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
    // Current effective set is still shown.
    expect(screen.getByTestId('policy-current-set')).toBeInTheDocument()
  })

  it('renders the "In sync" indicator when drift is false and state is applied', () => {
    const state = makeState({
      hasAppliedState: true,
      drift: false,
      appliedSet: [makeRef({ name: 'base' })],
      currentSet: [makeRef({ name: 'base' })],
    })
    render(<PolicySection state={state} />)
    expect(screen.getByTestId('policy-in-sync')).toBeInTheDocument()
    expect(screen.queryByTestId('policy-drift-badge')).not.toBeInTheDocument()
  })

  it('renders the drift badge and diff sections when drift is true', () => {
    const state = makeState({
      hasAppliedState: true,
      drift: true,
      appliedSet: [makeRef({ name: 'base' })],
      currentSet: [makeRef({ name: 'base' }), makeRef({ name: 'sidecar' })],
      addedRefs: [makeRef({ name: 'sidecar' })],
      removedRefs: [],
    })
    render(<PolicySection state={state} />)
    expect(screen.getByTestId('policy-drift-badge')).toBeInTheDocument()
    expect(screen.getByTestId('policy-added-refs')).toBeInTheDocument()
    // removed list is empty but renders the "None" placeholder.
    expect(screen.getByTestId('policy-removed-refs-empty')).toBeInTheDocument()
  })

  it('renders the reconcile slot only when drift is true and an action is provided', () => {
    const state = makeState({
      hasAppliedState: true,
      drift: true,
      currentSet: [makeRef({ name: 'base' })],
      addedRefs: [makeRef({ name: 'base' })],
    })
    const { rerender } = render(
      <PolicySection
        state={state}
        reconcileAction={<button type="button">Reconcile</button>}
      />,
    )
    expect(screen.getByTestId('policy-reconcile-slot')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /reconcile/i })).toBeInTheDocument()

    // Viewer case: no reconcileAction, no slot.
    rerender(<PolicySection state={state} />)
    expect(screen.queryByTestId('policy-reconcile-slot')).not.toBeInTheDocument()
  })

  it('does not render the reconcile slot when drift is false even if an action is provided', () => {
    const state = makeState({ hasAppliedState: true, drift: false })
    render(
      <PolicySection
        state={state}
        reconcileAction={<button type="button">Reconcile</button>}
      />,
    )
    expect(screen.queryByTestId('policy-reconcile-slot')).not.toBeInTheDocument()
  })

  it('formats refs with scope prefix and optional version constraint', () => {
    const state = makeState({
      hasAppliedState: true,
      drift: true,
      currentSet: [
        makeRef({ scope: TemplateScope.ORGANIZATION, scopeName: 'acme', name: 'base' }),
        makeRef({ scope: TemplateScope.FOLDER, scopeName: 'infra', name: 'istio', versionConstraint: '>=1.0.0' }),
        makeRef({ scope: TemplateScope.PROJECT, scopeName: 'web', name: 'api' }),
      ],
      addedRefs: [makeRef({ scope: TemplateScope.FOLDER, scopeName: 'infra', name: 'istio', versionConstraint: '>=1.0.0' })],
    })
    render(<PolicySection state={state} />)
    // Added ref appears in both the added-refs list and the current
    // effective set — assert that both occurrences render.
    expect(screen.getByText('org:acme/base')).toBeInTheDocument()
    expect(screen.getAllByText('folder:infra/istio@>=1.0.0').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('project:web/api')).toBeInTheDocument()
  })

  it('renders the custom heading when provided', () => {
    const state = makeState({ hasAppliedState: true })
    render(<PolicySection state={state} heading="TemplatePolicy" />)
    expect(screen.getByRole('heading', { name: /templatepolicy/i })).toBeInTheDocument()
  })

  // HOL-559 AC: the Policy section must be collapsible on both detail
  // surfaces. The shared component uses a native <details>/<summary>
  // disclosure to satisfy this.
  describe('collapsible', () => {
    it('renders a <details>/<summary> disclosure wrapper', () => {
      const state = makeState({ hasAppliedState: true })
      render(<PolicySection state={state} />)
      const section = screen.getByTestId('policy-section')
      expect(section.tagName.toLowerCase()).toBe('details')
      expect(screen.getByTestId('policy-section-summary').tagName.toLowerCase()).toBe('summary')
    })

    it('is collapsed by default when drift is false (in sync)', () => {
      const state = makeState({ hasAppliedState: true, drift: false })
      render(<PolicySection state={state} />)
      const section = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(section.open).toBe(false)
    })

    it('is expanded by default when drift is true', () => {
      const state = makeState({ hasAppliedState: true, drift: true, currentSet: [makeRef({})] })
      render(<PolicySection state={state} />)
      const section = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(section.open).toBe(true)
    })

    it('respects defaultOpen prop override', () => {
      const state = makeState({ hasAppliedState: true, drift: false })
      render(<PolicySection state={state} defaultOpen={true} />)
      const section = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(section.open).toBe(true)
    })

    // Regression for codex review round-2 finding: the details element
    // must not be a naively controlled element, so a parent re-render
    // (for example the deployment detail page's 5s status poll) cannot
    // stomp the user's toggle. If the component passed `open` as a
    // controlled prop, simulating the user collapsing the disclosure and
    // then re-rendering with the same props would reset `open` to the
    // original defaultOpen value.
    it('preserves the user toggle across parent re-renders', () => {
      const state = makeState({ hasAppliedState: true, drift: true, addedRefs: [makeRef({})], currentSet: [makeRef({})] })
      const { rerender } = render(<PolicySection state={state} />)
      const section = screen.getByTestId('policy-section') as HTMLDetailsElement
      // Drift-by-default opens the disclosure.
      expect(section.open).toBe(true)
      // Simulate the user collapsing it; the <details> element fires a
      // native toggle event when `open` changes.
      section.open = false
      section.dispatchEvent(new Event('toggle'))
      expect(section.open).toBe(false)
      // A parent re-render with the same props must NOT reopen it.
      rerender(<PolicySection state={state} />)
      const after = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(after.open).toBe(false)
    })

    // Regression for codex review round-3 finding: when the section
    // mounts in its loading state (defaultOpen=false) and the policy-
    // state RPC later resolves with drift=true, the disclosure MUST
    // auto-open so the attention signal is visible without the user
    // having to click. A shell that captures `open` exactly once on
    // mount (the round-2 approach) fails this test because the user
    // would see the section collapsed on first paint after loading
    // resolves.
    it('auto-opens when defaultOpen flips after mount and user has not toggled', () => {
      // First mount: loading state (state undefined → defaultOpen=false).
      const { rerender } = render(<PolicySection isPending={true} />)
      const loading = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(loading.open).toBe(false)
      // Policy-state RPC resolves with drift=true → defaultOpen becomes true.
      const drifted = makeState({ hasAppliedState: true, drift: true, addedRefs: [makeRef({})], currentSet: [makeRef({})] })
      rerender(<PolicySection state={drifted} />)
      const after = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(after.open).toBe(true)
    })

    // The auto-open sync must stop once the user interacts. If a user
    // has already collapsed the drifted section, a later prop change
    // (for example a refetch that still reports drift=true) must not
    // reopen the section against the user's intent.
    it('does not auto-reopen after the user collapses a drifted section', () => {
      const drifted = makeState({ hasAppliedState: true, drift: true, addedRefs: [makeRef({})], currentSet: [makeRef({})] })
      const { rerender } = render(<PolicySection state={drifted} />)
      const section = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(section.open).toBe(true)
      // User collapses.
      section.open = false
      section.dispatchEvent(new Event('toggle'))
      // A later re-render with the same drift=true props must not reopen.
      rerender(<PolicySection state={drifted} />)
      const after = screen.getByTestId('policy-section') as HTMLDetailsElement
      expect(after.open).toBe(false)
    })
  })
})
