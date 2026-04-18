import { render, screen, fireEvent, waitFor, within } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import type { Mock } from 'vitest'
import userEvent, { PointerEventsCheckLevel } from '@testing-library/user-event'

// Polyfills for jsdom — Radix Select / Combobox / Popover use pointer capture
// APIs that jsdom does not implement. Mirror the RuleEditor.test.tsx pattern.
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false
}
if (!Element.prototype.setPointerCapture) {
  Element.prototype.setPointerCapture = () => {}
}
if (!Element.prototype.releasePointerCapture) {
  Element.prototype.releasePointerCapture = () => {}
}

vi.mock('@/queries/templatePolicies', async () => {
  const actual = await vi.importActual<typeof import('@/queries/templatePolicies')>(
    '@/queries/templatePolicies',
  )
  return {
    ...actual,
    useListTemplatePolicies: vi.fn(),
  }
})

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

import { BindingForm } from './BindingForm'
import { useListTemplatePolicies } from '@/queries/templatePolicies'
import { useListProjects } from '@/queries/projects'
import { useListDeployments } from '@/queries/deployments'
import { useListTemplates, TemplateScope, makeOrgScope } from '@/queries/templates'
import { TemplatePolicyBindingTargetKind } from '@/queries/templatePolicyBindings'

const ORG_SCOPE = makeOrgScope('test-org')

function stubQueries({
  policies = [],
  projects = [],
  deployments = [],
  projectTemplates = [],
}: {
  policies?: Array<{
    name: string
    displayName: string
    description: string
    scopeRef?: { scope: number; scopeName: string }
  }>
  projects?: Array<{ name: string; displayName: string }>
  deployments?: Array<{ name: string; displayName: string }>
  projectTemplates?: Array<{
    name: string
    displayName: string
    scopeRef: { scope: number; scopeName: string }
  }>
}) {
  ;(useListTemplatePolicies as Mock).mockReturnValue({
    data: policies,
    isPending: false,
    error: null,
  })
  ;(useListProjects as Mock).mockReturnValue({
    data: { projects },
    isPending: false,
    error: null,
  })
  ;(useListDeployments as Mock).mockReturnValue({
    data: deployments,
    isPending: false,
    error: null,
  })
  ;(useListTemplates as Mock).mockReturnValue({
    data: projectTemplates,
    isPending: false,
    error: null,
  })
}

describe('BindingForm', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stubQueries({})
  })

  it('renders the name, display name, description, policy, and targets fields', () => {
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/name slug/i)).toBeInTheDocument()
    expect(screen.getByLabelText(/description/i)).toBeInTheDocument()
    // The Policy picker is exposed via a Combobox trigger (role=combobox).
    expect(
      screen.getByRole('combobox', { name: /template policy/i }),
    ).toBeInTheDocument()
    // TargetRefEditor always renders one empty row on create.
    expect(screen.getByTestId('target-ref-editor')).toBeInTheDocument()
    expect(screen.getByTestId('target-ref-row-0')).toBeInTheDocument()
  })

  it('rejects submission when the name is empty', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(
        screen.getByText(/binding name is required/i),
      ).toBeInTheDocument()
    })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects submission when no policy is selected', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Bind HTTPRoute' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(
        screen.getByText(/policy selection is required/i),
      ).toBeInTheDocument()
    })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('rejects submission when the target is missing a name', async () => {
    stubQueries({
      policies: [
        {
          name: 'require-http',
          displayName: 'Require HTTP',
          description: '',
          scopeRef: { scope: TemplateScope.ORGANIZATION, scopeName: 'test-org' },
        },
      ],
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
    })

    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Bind HTTPRoute' },
    })

    // Pick the policy via the Combobox.
    const policyTrigger = screen.getByRole('combobox', { name: /template policy/i })
    await user.click(policyTrigger)
    await user.click(
      await screen.findByText(/org \/ test-org \/ require-http/),
    )

    // Pick a project via the target row Combobox.
    const row = screen.getByTestId('target-ref-row-0')
    const projectTrigger = within(row).getByRole('combobox', {
      name: /target 1 project/i,
    })
    await user.click(projectTrigger)
    await user.click(await screen.findByText(/Project A \(proj-a\)/))

    // Submit without picking a name — client-side validation should reject.
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))
    await waitFor(() => {
      expect(screen.getByTestId('binding-form-error')).toHaveTextContent(
        /name is required/i,
      )
    })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('submits a valid draft with the expected mutation shape', async () => {
    stubQueries({
      policies: [
        {
          name: 'require-http',
          displayName: 'Require HTTP',
          description: '',
          scopeRef: { scope: TemplateScope.ORGANIZATION, scopeName: 'test-org' },
        },
      ],
      projects: [{ name: 'proj-a', displayName: 'Project A' }],
      projectTemplates: [
        {
          name: 'ingress',
          displayName: 'Ingress',
          scopeRef: { scope: TemplateScope.PROJECT, scopeName: 'proj-a' },
        },
      ],
    })

    const user = userEvent.setup({
      pointerEventsCheck: PointerEventsCheckLevel.Never,
    })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Bind HTTPRoute' },
    })
    fireEvent.change(screen.getByLabelText(/^description$/i), {
      target: { value: 'Attach on proj-a ingress' },
    })

    // Policy picker.
    await user.click(
      screen.getByRole('combobox', { name: /template policy/i }),
    )
    await user.click(
      await screen.findByText(/org \/ test-org \/ require-http/),
    )

    // Target project.
    const row = screen.getByTestId('target-ref-row-0')
    await user.click(
      within(row).getByRole('combobox', { name: /target 1 project/i }),
    )
    await user.click(await screen.findByText(/Project A \(proj-a\)/))

    // Kind defaults to PROJECT_TEMPLATE, so pick an ingress template.
    await user.click(
      within(row).getByRole('combobox', { name: /target 1 name/i }),
    )
    await user.click(await screen.findByText(/Ingress \(ingress\)/))

    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledTimes(1)
    })

    const arg = onSubmit.mock.calls[0][0]
    expect(arg.name).toBe('bind-httproute')
    expect(arg.displayName).toBe('Bind HTTPRoute')
    expect(arg.description).toBe('Attach on proj-a ingress')
    expect(arg.policyRef?.name).toBe('require-http')
    expect(arg.policyRef?.scopeRef?.scopeName).toBe('test-org')
    expect(arg.targetRefs).toHaveLength(1)
    expect(arg.targetRefs[0]).toMatchObject({
      kind: TemplatePolicyBindingTargetKind.PROJECT_TEMPLATE,
      projectName: 'proj-a',
      name: 'ingress',
    })
  })

  it('blocks submission when the resolved scope is project (contrived URL)', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    render(
      <BindingForm
        mode="create"
        scopeType="project"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={onSubmit}
        onCancel={vi.fn()}
      />,
    )

    fireEvent.change(screen.getByLabelText(/display name/i), {
      target: { value: 'Would-be' },
    })
    fireEvent.click(screen.getByRole('button', { name: /^create$/i }))

    await waitFor(() => {
      expect(screen.getByTestId('binding-form-error')).toHaveTextContent(
        /only be created at folder or organization scope/i,
      )
    })
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('disables form controls for VIEWER', () => {
    render(
      <BindingForm
        mode="create"
        scopeType="organization"
        scopeRef={ORG_SCOPE}
        organization="test-org"
        canWrite={false}
        submitLabel="Create"
        pendingLabel="Creating..."
        onSubmit={vi.fn()}
        onCancel={vi.fn()}
      />,
    )

    expect(screen.getByLabelText(/display name/i)).toBeDisabled()
    expect(screen.getByLabelText(/^description$/i)).toBeDisabled()
    expect(screen.getByRole('button', { name: /^create$/i })).toBeDisabled()
  })
})
