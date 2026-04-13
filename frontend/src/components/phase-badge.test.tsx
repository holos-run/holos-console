import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { create } from '@bufbuild/protobuf'
import {
  DeploymentPhase,
  DeploymentStatusSummarySchema,
  type DeploymentStatusSummary,
} from '@/gen/holos/console/v1/deployments_pb'
import { PhaseBadge } from './phase-badge'

function makeSummary(partial: Partial<DeploymentStatusSummary> = {}): DeploymentStatusSummary {
  return create(DeploymentStatusSummarySchema, {
    phase: DeploymentPhase.UNSPECIFIED,
    readyReplicas: 0,
    desiredReplicas: 0,
    availableReplicas: 0,
    updatedReplicas: 0,
    observedGeneration: 0n,
    message: '',
    ...partial,
  })
}

describe('PhaseBadge', () => {
  it('renders running badge with replica count when desiredReplicas > 0', () => {
    render(
      <PhaseBadge
        summary={makeSummary({ phase: DeploymentPhase.RUNNING, readyReplicas: 2, desiredReplicas: 3 })}
      />,
    )
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.getByText('2/3')).toBeInTheDocument()
  })

  it('renders pending badge with replica count', () => {
    render(
      <PhaseBadge
        summary={makeSummary({ phase: DeploymentPhase.PENDING, readyReplicas: 0, desiredReplicas: 1 })}
      />,
    )
    expect(screen.getByText('Pending')).toBeInTheDocument()
    expect(screen.getByText('0/1')).toBeInTheDocument()
  })

  it('renders failed badge with replica count', () => {
    render(
      <PhaseBadge
        summary={makeSummary({ phase: DeploymentPhase.FAILED, readyReplicas: 0, desiredReplicas: 1 })}
      />,
    )
    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('0/1')).toBeInTheDocument()
  })

  it('renders succeeded badge with replica count', () => {
    render(
      <PhaseBadge
        summary={makeSummary({ phase: DeploymentPhase.SUCCEEDED, readyReplicas: 1, desiredReplicas: 1 })}
      />,
    )
    expect(screen.getByText('Succeeded')).toBeInTheDocument()
    expect(screen.getByText('1/1')).toBeInTheDocument()
  })

  it('omits replica count when desiredReplicas is zero', () => {
    render(
      <PhaseBadge
        summary={makeSummary({ phase: DeploymentPhase.RUNNING, readyReplicas: 0, desiredReplicas: 0 })}
      />,
    )
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.queryByText('0/0')).not.toBeInTheDocument()
  })

  it('renders Unknown when summary is missing and no fallback', () => {
    render(<PhaseBadge />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
  })

  it('renders fallback phase when summary is missing', () => {
    render(<PhaseBadge fallbackPhase={DeploymentPhase.PENDING} />)
    expect(screen.getByText('Pending')).toBeInTheDocument()
    // no replica count when there is no summary
    expect(screen.queryByText(/\d+\/\d+/)).not.toBeInTheDocument()
  })

  it('renders unknown branch when summary.phase is UNSPECIFIED and no replicas', () => {
    render(<PhaseBadge summary={makeSummary({ phase: DeploymentPhase.UNSPECIFIED })} />)
    expect(screen.getByText('Unknown')).toBeInTheDocument()
  })
})
