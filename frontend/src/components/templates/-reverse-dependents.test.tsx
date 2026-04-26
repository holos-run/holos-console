// Unit tests for the ReverseDependents component (HOL-987).
//
// Covers: loading skeleton, error state, empty state, Template dependent rows
// with scope badges, Deployment dependent rows, and disabled state.

import { render, screen } from '@testing-library/react'
import { vi } from 'vitest'
import type { Mock } from 'vitest'
import React from 'react'

vi.mock('@/queries/templateDependencies', () => ({
  useListTemplateDependents: vi.fn(),
  useListDeploymentDependents: vi.fn(),
  DependencyScope: {
    UNSPECIFIED: 0,
    INSTANCE: 1,
    PROJECT: 2,
    REMOTE_PROJECT: 3,
  },
}))

vi.mock('@/lib/scope-labels', () => ({
  scopeNameFromNamespace: vi.fn((ns: string) => {
    if (ns.startsWith('holos-prj-')) return ns.slice('holos-prj-'.length)
    return ''
  }),
}))

import {
  useListTemplateDependents,
  useListDeploymentDependents,
  DependencyScope,
} from '@/queries/templateDependencies'
import { ReverseDependents } from './ReverseDependents'

describe('ReverseDependents — Template kind', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders loading skeletons while the RPC is pending', () => {
    ;(useListTemplateDependents as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })

    render(
      <ReverseDependents
        kind="Template"
        namespace="holos-org-acme"
        name="reference-grant"
        enabled
      />,
    )

    expect(screen.getByTestId('reverse-dependents-loading')).toBeInTheDocument()
  })

  it('renders an error alert when the RPC fails', () => {
    ;(useListTemplateDependents as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('network failure'),
    })

    render(
      <ReverseDependents
        kind="Template"
        namespace="holos-org-acme"
        name="reference-grant"
        enabled
      />,
    )

    expect(screen.getByTestId('reverse-dependents-error')).toBeInTheDocument()
    expect(screen.getByText('network failure')).toBeInTheDocument()
  })

  it('renders an empty message when there are no dependents', () => {
    ;(useListTemplateDependents as Mock).mockReturnValue({
      data: [],
      isPending: false,
      error: null,
    })

    render(
      <ReverseDependents
        kind="Template"
        namespace="holos-org-acme"
        name="reference-grant"
        enabled
      />,
    )

    expect(screen.getByText(/no dependents found/i)).toBeInTheDocument()
  })

  it('renders a row for each TemplateDependentRecord with a scope badge', () => {
    ;(useListTemplateDependents as Mock).mockReturnValue({
      data: [
        {
          scope: DependencyScope.INSTANCE,
          dependentNamespace: 'holos-prj-billing',
          dependentName: 'istio-ingress',
          requiringTemplateNamespace: 'holos-prj-billing',
          requiringTemplateName: 'reference-grant',
        },
        {
          scope: DependencyScope.PROJECT,
          dependentNamespace: 'holos-fld-team-alpha',
          dependentName: 'reference-grant-req',
          requiringTemplateNamespace: '',
          requiringTemplateName: '',
        },
        {
          scope: DependencyScope.REMOTE_PROJECT,
          dependentNamespace: 'holos-prj-infra',
          dependentName: 'remote-dep',
          requiringTemplateNamespace: 'holos-prj-infra',
          requiringTemplateName: 'reference-grant',
        },
      ],
      isPending: false,
      error: null,
    })

    render(
      <ReverseDependents
        kind="Template"
        namespace="holos-org-acme"
        name="reference-grant"
        enabled
      />,
    )

    const table = screen.getByTestId('reverse-dependents-template')
    expect(table).toBeInTheDocument()

    // Scope badges
    expect(screen.getByText('instance')).toBeInTheDocument()
    expect(screen.getByText('project')).toBeInTheDocument()
    expect(screen.getByText('remote-project')).toBeInTheDocument()

    // Row identities
    expect(screen.getByText('holos-prj-billing/istio-ingress')).toBeInTheDocument()
    expect(screen.getByText('holos-fld-team-alpha/reference-grant-req')).toBeInTheDocument()
    expect(screen.getByText('holos-prj-infra/remote-dep')).toBeInTheDocument()
  })

  it('renders nothing when enabled=false', () => {
    ;(useListTemplateDependents as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })

    const { container } = render(
      <ReverseDependents
        kind="Template"
        namespace="holos-org-acme"
        name="reference-grant"
        enabled={false}
      />,
    )

    expect(container.firstChild).toBeNull()
  })
})

describe('ReverseDependents — Deployment kind', () => {
  beforeEach(() => vi.clearAllMocks())

  it('renders a row for each DeploymentDependentRecord', () => {
    ;(useListDeploymentDependents as Mock).mockReturnValue({
      data: [
        {
          dependentNamespace: 'holos-prj-billing',
          dependentName: 'istio-ingress',
        },
        {
          dependentNamespace: 'holos-prj-infra',
          dependentName: 'istio-ingress',
        },
      ],
      isPending: false,
      error: null,
    })

    render(
      <ReverseDependents
        kind="Deployment"
        namespace="holos-prj-platform"
        name="istio-ingress"
        enabled
      />,
    )

    const table = screen.getByTestId('reverse-dependents-deployment')
    expect(table).toBeInTheDocument()

    // scopeNameFromNamespace strips 'holos-prj-' prefix
    expect(screen.getByText('billing')).toBeInTheDocument()
    expect(screen.getByText('infra')).toBeInTheDocument()
    // deployment name column
    expect(screen.getAllByText('istio-ingress')).toHaveLength(2)
  })

  it('renders loading state for Deployment kind', () => {
    ;(useListDeploymentDependents as Mock).mockReturnValue({
      data: undefined,
      isPending: true,
      error: null,
    })

    render(
      <ReverseDependents
        kind="Deployment"
        namespace="holos-prj-platform"
        name="istio-ingress"
        enabled
      />,
    )

    expect(screen.getByTestId('reverse-dependents-loading')).toBeInTheDocument()
  })

  it('renders error state for Deployment kind', () => {
    ;(useListDeploymentDependents as Mock).mockReturnValue({
      data: undefined,
      isPending: false,
      error: new Error('rpc error'),
    })

    render(
      <ReverseDependents
        kind="Deployment"
        namespace="holos-prj-platform"
        name="istio-ingress"
        enabled
      />,
    )

    expect(screen.getByTestId('reverse-dependents-error')).toBeInTheDocument()
    expect(screen.getByText('rpc error')).toBeInTheDocument()
  })
})
