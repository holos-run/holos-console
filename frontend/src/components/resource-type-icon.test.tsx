import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ResourceType } from '@/gen/holos/console/v1/resources_pb'
import { ResourceTypeIcon } from './resource-type-icon'

describe('ResourceTypeIcon', () => {
  it('renders FolderTree icon for FOLDER', () => {
    render(<ResourceTypeIcon type={ResourceType.FOLDER} />)
    expect(screen.getByTestId('resource-type-icon-folder')).toBeInTheDocument()
  })

  it('renders FolderKanban icon for PROJECT', () => {
    render(<ResourceTypeIcon type={ResourceType.PROJECT} />)
    expect(screen.getByTestId('resource-type-icon-project')).toBeInTheDocument()
  })

  it('renders null for UNSPECIFIED', () => {
    const { container } = render(<ResourceTypeIcon type={ResourceType.UNSPECIFIED} />)
    expect(container.firstChild).toBeNull()
  })

  it('applies default className of h-4 w-4', () => {
    render(<ResourceTypeIcon type={ResourceType.FOLDER} />)
    const icon = screen.getByTestId('resource-type-icon-folder')
    expect(icon).toHaveClass('h-4', 'w-4')
  })

  it('applies custom className when provided', () => {
    render(<ResourceTypeIcon type={ResourceType.PROJECT} className="h-6 w-6" />)
    const icon = screen.getByTestId('resource-type-icon-project')
    expect(icon).toHaveClass('h-6', 'w-6')
    expect(icon).not.toHaveClass('h-4', 'w-4')
  })

  it('sets aria-hidden on FOLDER icon', () => {
    render(<ResourceTypeIcon type={ResourceType.FOLDER} />)
    const icon = screen.getByTestId('resource-type-icon-folder')
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })

  it('sets aria-hidden on PROJECT icon', () => {
    render(<ResourceTypeIcon type={ResourceType.PROJECT} />)
    const icon = screen.getByTestId('resource-type-icon-project')
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })
})
