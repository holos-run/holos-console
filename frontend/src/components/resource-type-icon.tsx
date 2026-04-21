import { FolderKanban, FolderTree } from 'lucide-react'
import { ResourceType } from '@/gen/holos/console/v1/resources_pb'

export interface ResourceTypeIconProps {
  type: ResourceType
  className?: string
}

/**
 * ResourceTypeIcon renders the canonical icon for a ResourceType enum value.
 *
 * - FOLDER → FolderTree (decorative, aria-hidden)
 * - PROJECT → FolderKanban (decorative, aria-hidden)
 * - UNSPECIFIED / unknown → null (callers decide whether to show a fallback)
 *
 * The icon is decorative so aria-hidden="true" is set to avoid polluting the
 * accessible name of the surrounding Badge or link element.
 *
 * Default className is "h-4 w-4" to match the sidebar's icon sizing; callers
 * can override via the className prop.
 */
export function ResourceTypeIcon({ type, className = 'h-4 w-4' }: ResourceTypeIconProps) {
  switch (type) {
    case ResourceType.FOLDER:
      return <FolderTree className={className} aria-hidden="true" data-testid="resource-type-icon-folder" />
    case ResourceType.PROJECT:
      return <FolderKanban className={className} aria-hidden="true" data-testid="resource-type-icon-project" />
    default:
      return null
  }
}
