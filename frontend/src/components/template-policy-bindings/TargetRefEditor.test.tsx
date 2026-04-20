import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Mock } from 'vitest'
import userEvent, { PointerEventsCheckLevel } from '@testing-library/user-event'

if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

vi.mock('@/queries/projects', () => ({
  useListProjects: vi.fn(),
}))

vi.mock('@/queries/deployments', () => ({
  useListDeployments: vi.fn(),
}))

vi.mock('@/queries/templates', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templates')>(
    '@/queries/templates',
  )
  return {
    ...actual,
    useListTemplates: vi.fn(),
  }
})

import { TargetRefEditor } from './TargetRefEditor'
import { useListProjects } from '@/queries/projects'
import { useListDeployments } from '@/queries/deployments'
import { useListTemplates } from '@/queries/templates'
import { namespaceForProject } from '@/lib/scope-labels'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'
import type { TargetRefDraft } from './binding-draft'

function makeTarget(overrides: Partial<TargetRefDraft> = {}): TargetRefDraft {
  return {
    kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
    projectName: '',
    name: '',
    ...overrides,
  }
}

function stubQueries({
  projects = [{ name: 'proj-a', displayName: 'Project A' }],
  projectTemplates = [
    {
      name: 'ingress',
      displayName: 'Ingress',
      namespace: namespaceForProject('proj-a'),
    },
  ],
  deployments = [{ name: 'web', displayName: 'Web' }],
}: {
  projects?: Array<{ name: string; displayName: string }>
  projectTemplates?: Array<{
    name: string
    displayName: string
    namespace: string
  }>
  deployments?: Array<{ name: string; displayName: string }>
} = {}) {
  ;(useListProjects as Mock).mockReturnValue({
    data: { projects },
    isPending: false,
    error: null,
  })
  ;(useListTemplates as Mock).mockReturnValue({
    data: projectTemplates,
    isPending: false,
    error: null,
  })
  ;(useListDeployments as Mock).mockReturnValue({
    data: deployments,
    isPending: false,
    error: null,
  })
}

describe('TargetRefEditor', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubQueries()
  })

  it('renders one row per target', () => {
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({ projectName: 'proj-a', name: 'ingress' }),
          makeTarget({
            kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
            projectName: 'proj-a',
            name: 'web',
          }),
        ]}
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByTestId('target-ref-row-0')).toBeInTheDocument()
    expect(screen.getByTestId('target-ref-row-1')).toBeInTheDocument()
  })

  it('renders the empty-state hint when the target list is empty', () => {
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[]}
        onChange={vi.fn()}
      />,
    )

    expect(screen.getByText(/no targets yet/i)).toBeInTheDocument()
    expect(screen.queryByTestId('target-ref-row-0')).not.toBeInTheDocument()
  })

  it('adds a new row when Add Target is clicked', () => {
    const onChange = vi.fn()
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[makeTarget()]}
        onChange={onChange}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /add target/i }))
    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as TargetRefDraft[]
    expect(next).toHaveLength(2)
    expect(next[1]).toMatchObject({
      kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
      projectName: '',
      name: '',
    })
  })

  it('removes a row when Remove target is clicked', () => {
    const onChange = vi.fn()
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({ projectName: 'proj-a', name: 'ingress' }),
          makeTarget({
            kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
            projectName: 'proj-a',
            name: 'web',
          }),
        ]}
        onChange={onChange}
      />,
    )
    const row = screen.getByTestId('target-ref-row-1')
    fireEvent.click(within(row).getByRole('button', { name: /remove target 2/i }))
    expect(onChange).toHaveBeenCalledWith([
      expect.objectContaining({
        kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
        projectName: 'proj-a',
        name: 'ingress',
      }),
    ])
  })

  it('kind switch from PROJECT_TEMPLATE to DEPLOYMENT clears the name and re-renders picker items', async () => {
    // Target the Radix Select with user-event so the popover opens and we can
    // pick the "Deployment" option. The onChange handler must clear `name`
    // since project templates and deployments share the same combobox source.
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })
    const onChange = vi.fn()
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: 'proj-a',
            name: 'ingress',
          }),
        ]}
        onChange={onChange}
      />,
    )

    const row = screen.getByTestId('target-ref-row-0')
    const kindTrigger = within(row).getByRole('combobox', {
      name: /target 1 kind/i,
    })
    await user.click(kindTrigger)
    await user.click(await screen.findByRole('option', { name: /deployment/i }))

    expect(onChange).toHaveBeenCalledTimes(1)
    const next = onChange.mock.calls[0][0] as TargetRefDraft[]
    expect(next[0]).toMatchObject({
      kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
      projectName: 'proj-a',
      name: '',
    })
  })

  it('shows project-template items when kind=PROJECT_TEMPLATE', async () => {
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })

    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({
            kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
            projectName: 'proj-a',
            name: '',
          }),
        ]}
        onChange={vi.fn()}
      />,
    )

    const nameTrigger = within(screen.getByTestId('target-ref-row-0')).getByRole(
      'combobox',
      { name: /target 1 name/i },
    )
    await user.click(nameTrigger)
    await waitFor(() => {
      expect(screen.getByText(/Ingress \(ingress\)/)).toBeInTheDocument()
    })
    // The deployment label must NOT appear while kind=PROJECT_TEMPLATE.
    expect(screen.queryByText(/Web \(web\)/)).not.toBeInTheDocument()
  })

  it('shows deployment items when kind=DEPLOYMENT', async () => {
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })

    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({
            kind: TemplatePolicyBindingTargetKind.DEPLOYMENT,
            projectName: 'proj-a',
            name: '',
          }),
        ]}
        onChange={vi.fn()}
      />,
    )

    const nameTrigger = within(screen.getByTestId('target-ref-row-0')).getByRole(
      'combobox',
      { name: /target 1 name/i },
    )
    await user.click(nameTrigger)
    await waitFor(() => {
      expect(screen.getByText(/Web \(web\)/)).toBeInTheDocument()
    })
    // And the project template label must NOT appear while kind=DEPLOYMENT.
    expect(screen.queryByText(/Ingress \(ingress\)/)).not.toBeInTheDocument()
  })

  it('changing the project clears the name', async () => {
    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })
    const onChange = vi.fn()
    stubQueries({
      projects: [
        { name: 'proj-a', displayName: 'Project A' },
        { name: 'proj-b', displayName: 'Project B' },
      ],
    })

    render(
      <TargetRefEditor
        organization="test-org"
        targets={[
          makeTarget({ projectName: 'proj-a', name: 'ingress' }),
        ]}
        onChange={onChange}
      />,
    )

    const row = screen.getByTestId('target-ref-row-0')
    await user.click(
      within(row).getByRole('combobox', { name: /target 1 project/i }),
    )
    await user.click(await screen.findByText(/Project B \(proj-b\)/))

    expect(onChange).toHaveBeenCalled()
    const next = onChange.mock.calls[0][0] as TargetRefDraft[]
    expect(next[0]).toMatchObject({
      projectName: 'proj-b',
      name: '',
    })
  })

  it('hides the Add Target and Remove buttons when disabled', () => {
    render(
      <TargetRefEditor
        organization="test-org"
        targets={[makeTarget({ projectName: 'proj-a', name: 'ingress' })]}
        onChange={vi.fn()}
        disabled
      />,
    )

    expect(
      screen.queryByRole('button', { name: /add target/i }),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: /remove target 1/i }),
    ).not.toBeInTheDocument()
  })
})
