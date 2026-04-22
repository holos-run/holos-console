/**
 * ResourceTree — hierarchical tree view for the Resource Manager page.
 *
 * Renders Organization → Folders → Projects as an expandable/collapsible
 * tree. Each row is a clickable link to its namespace index page. Expansion
 * state is controlled by the caller (URL search params for shareability).
 *
 * Data model: useListResources returns a flat list of Resource entries with
 * path[] breadcrumb chains. ResourceTree groups them client-side into a tree
 * shape without additional RPCs.
 */

import { useMemo } from 'react'
import { ResourceType, type Resource } from '@/gen/holos/console/v1/resources_pb'
import { TreeNode, type TreeNodeData } from './TreeNode'

export interface ResourceTreeProps {
  /** The name of the root organization. */
  orgName: string
  /** Flat list of all resources returned by useListResources. */
  resources: Resource[]
  /**
   * Set of paths currently expanded, e.g. `new Set(["folder-a", "folder-b"])`.
   * The org root is always rendered expanded; individual folders track their
   * path (the folder slug) in this set.
   */
  expanded: Set<string>
  /** Called when the user toggles a folder row. */
  onToggle: (path: string) => void
  /** Namespace to use for delete dialogs (typically the org name). */
  organization: string
}

// ---------------------------------------------------------------------------
// Tree-building helpers
// ---------------------------------------------------------------------------

/**
 * Build a flat map of folderName → direct folder children (Resource entries).
 * The second argument determines the immediate parent for each Resource.
 * A folder is a direct child of another folder when its path's last element
 * matches the parent. A folder is a direct child of the org when its path
 * has exactly one element (the org).
 */
function buildFolderChildMap(resources: Resource[]): Map<string, Resource[]> {
  const map = new Map<string, Resource[]>()

  for (const r of resources) {
    if (r.type !== ResourceType.FOLDER) continue
    // The immediate parent of this folder is the last path element, or the
    // org itself (sentinel key '') when path.length === 1.
    const parentKey =
      r.path.length > 1 ? r.path[r.path.length - 1].name : ''
    if (!map.has(parentKey)) map.set(parentKey, [])
    map.get(parentKey)!.push(r)
  }

  return map
}

/**
 * Build a map of folderName → direct project children (Resource entries).
 * A project is a direct child of a folder when its last path element is that
 * folder. A project is a direct child of the org (sentinel key '') when its
 * path has length 1.
 */
function buildProjectChildMap(resources: Resource[]): Map<string, Resource[]> {
  const map = new Map<string, Resource[]>()

  for (const r of resources) {
    if (r.type !== ResourceType.PROJECT) continue
    const parentKey =
      r.path.length > 1 ? r.path[r.path.length - 1].name : ''
    if (!map.has(parentKey)) map.set(parentKey, [])
    map.get(parentKey)!.push(r)
  }

  return map
}

/**
 * Convert a flat Resource list into a recursive TreeNodeData tree rooted at
 * the org. Projects are leaves; folders are expandable.
 */
function buildTree(
  orgName: string,
  folderMap: Map<string, Resource[]>,
  projectMap: Map<string, Resource[]>,
): TreeNodeData {
  function makeFolder(r: Resource): TreeNodeData {
    const folderChildren = (folderMap.get(r.name) ?? []).map(makeFolder)
    const projectChildren = (projectMap.get(r.name) ?? []).map(makeProject)
    return {
      type: 'folder',
      name: r.name,
      displayName: r.displayName || r.name,
      createdAt: undefined,
      updatedAt: undefined,
      children: [...folderChildren, ...projectChildren],
    }
  }

  function makeProject(r: Resource): TreeNodeData {
    return {
      type: 'project',
      name: r.name,
      displayName: r.displayName || r.name,
      createdAt: undefined,
      updatedAt: undefined,
      children: [],
    }
  }

  const orgFolders = (folderMap.get('') ?? []).map(makeFolder)
  const orgProjects = (projectMap.get('') ?? []).map(makeProject)

  return {
    type: 'org',
    name: orgName,
    displayName: orgName,
    createdAt: undefined,
    updatedAt: undefined,
    children: [...orgFolders, ...orgProjects],
  }
}

// ---------------------------------------------------------------------------
// ResourceTree
// ---------------------------------------------------------------------------

export function ResourceTree({
  orgName,
  resources,
  expanded,
  onToggle,
  organization,
}: ResourceTreeProps) {
  const tree = useMemo(() => {
    const folderMap = buildFolderChildMap(resources)
    const projectMap = buildProjectChildMap(resources)
    return buildTree(orgName, folderMap, projectMap)
  }, [orgName, resources])

  return (
    <div
      role="tree"
      aria-label="Resource tree"
      className="font-mono text-sm"
      data-testid="resource-tree"
    >
      <TreeNode
        node={tree}
        depth={0}
        expanded={expanded}
        onToggle={onToggle}
        organization={organization}
      />
    </div>
  )
}
