/**
 * TreeNode — a single row in the ResourceTree.
 *
 * Renders the disclosure control (+/-), display name link, Created At /
 * Updated At columns, and Settings / Delete icon buttons. Projects are
 * leaves (no disclosure control). Organizations and Folders have a `+`/`-`
 * toggle.
 */

import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { ChevronRight, ChevronDown, Settings, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ConfirmDeleteDialog } from '@/components/ui/confirm-delete-dialog'
import { useDeleteFolder } from '@/queries/folders'
import { useDeleteProject } from '@/queries/projects'
import { toast } from 'sonner'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type NodeType = 'org' | 'folder' | 'project'

export interface TreeNodeData {
  type: NodeType
  name: string
  displayName: string
  /** RFC3339 string or undefined when the server has not set this field. */
  createdAt: string | undefined
  /** RFC3339 string or undefined when the server has not set this field. */
  updatedAt: string | undefined
  children: TreeNodeData[]
}

export interface TreeNodeProps {
  node: TreeNodeData
  depth: number
  expanded: Set<string>
  onToggle: (path: string) => void
  organization: string
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Format an RFC3339 string or undefined into a locale date string. */
function formatDate(raw: string | undefined): string {
  if (!raw) return '-'
  try {
    return new Date(raw).toLocaleDateString()
  } catch {
    return raw
  }
}

// ---------------------------------------------------------------------------
// TreeNode
// ---------------------------------------------------------------------------

export function TreeNode({
  node,
  depth,
  expanded,
  onToggle,
  organization,
}: TreeNodeProps) {
  const isExpandable = node.type === 'org' || node.type === 'folder'
  const isExpanded = isExpandable && (node.type === 'org' || expanded.has(node.name))

  // Delete dialog state — owned per row to avoid shared dialog coupling
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<Error | null>(null)

  const deleteFolderMutation = useDeleteFolder(organization)
  const deleteProjectMutation = useDeleteProject()

  const isDeleting =
    deleteFolderMutation.isPending || deleteProjectMutation.isPending

  const handleDeleteConfirm = async () => {
    setDeleteError(null)
    try {
      if (node.type === 'folder') {
        await deleteFolderMutation.mutateAsync({ name: node.name })
      } else if (node.type === 'project') {
        await deleteProjectMutation.mutateAsync({ name: node.name })
      }
      toast.success(`Deleted ${node.displayName}`)
      setDeleteOpen(false)
    } catch (err) {
      const e = err instanceof Error ? err : new Error(String(err))
      setDeleteError(e)
      toast.error(e.message)
    }
  }

  const handleToggle = () => {
    if (isExpandable && node.type !== 'org') {
      onToggle(node.name)
    }
  }

  // Build the link destination for the display name
  function nameLink() {
    if (node.type === 'org') {
      return (
        <Link
          to="/orgs/$orgName"
          params={{ orgName: node.name }}
          className="hover:underline font-medium"
          data-testid={`tree-link-org-${node.name}`}
        >
          {node.displayName}
        </Link>
      )
    }
    if (node.type === 'folder') {
      return (
        <Link
          to="/folders/$folderName"
          params={{ folderName: node.name }}
          className="hover:underline font-medium"
          data-testid={`tree-link-folder-${node.name}`}
        >
          {node.displayName}
        </Link>
      )
    }
    // project
    return (
      <Link
        to="/projects/$projectName"
        params={{ projectName: node.name }}
        className="hover:underline font-medium"
        data-testid={`tree-link-project-${node.name}`}
      >
        {node.displayName}
      </Link>
    )
  }

  return (
    <>
      {/* Row */}
      <div
        role="treeitem"
        aria-expanded={isExpandable ? isExpanded : undefined}
        data-testid={`tree-row-${node.name}`}
        style={{ paddingLeft: depth * 20 }}
        className="flex items-center gap-2 py-2 pr-2 hover:bg-muted/50 rounded-sm border-b border-border/40 last:border-0"
      >
        {/* Disclosure control */}
        <div className="flex-shrink-0 w-5">
          {isExpandable ? (
            <button
              type="button"
              aria-label={isExpanded ? `collapse ${node.displayName}` : `expand ${node.displayName}`}
              data-testid={`tree-toggle-${node.name}`}
              onClick={handleToggle}
              className="inline-flex items-center justify-center w-5 h-5 text-muted-foreground hover:text-foreground transition-colors"
            >
              {isExpanded ? (
                <ChevronDown className="h-3.5 w-3.5" />
              ) : (
                <ChevronRight className="h-3.5 w-3.5" />
              )}
            </button>
          ) : null}
        </div>

        {/* Display name link — grows to fill remaining space */}
        <div className="flex-1 min-w-0">{nameLink()}</div>

        {/* Created At */}
        <div
          className="hidden sm:block w-28 text-right text-muted-foreground text-xs whitespace-nowrap"
          data-testid={`tree-created-${node.name}`}
        >
          {formatDate(node.createdAt)}
        </div>

        {/* Updated At */}
        <div
          className="hidden sm:block w-28 text-right text-muted-foreground text-xs whitespace-nowrap"
          data-testid={`tree-updated-${node.name}`}
        >
          {formatDate(node.updatedAt)}
        </div>

        {/* Action icons */}
        <div className="flex items-center gap-0.5 flex-shrink-0">
          {/* Settings */}
          {node.type === 'org' && (
            <span data-testid={`tree-settings-${node.name}`}>
              <Button
                variant="ghost"
                size="icon"
                asChild
                aria-label={`settings for ${node.displayName}`}
              >
                <Link to="/orgs/$orgName/settings" params={{ orgName: node.name }}>
                  <Settings className="h-4 w-4" />
                </Link>
              </Button>
            </span>
          )}
          {node.type === 'folder' && (
            <span data-testid={`tree-settings-${node.name}`}>
              <Button
                variant="ghost"
                size="icon"
                asChild
                aria-label={`settings for ${node.displayName}`}
              >
                <Link to="/folders/$folderName/settings" params={{ folderName: node.name }}>
                  <Settings className="h-4 w-4" />
                </Link>
              </Button>
            </span>
          )}
          {node.type === 'project' && (
            <span data-testid={`tree-settings-${node.name}`}>
              <Button
                variant="ghost"
                size="icon"
                asChild
                aria-label={`settings for ${node.displayName}`}
              >
                <Link to="/projects/$projectName/settings" params={{ projectName: node.name }}>
                  <Settings className="h-4 w-4" />
                </Link>
              </Button>
            </span>
          )}

          {/* Delete — only for folders and projects (not the org root) */}
          {node.type !== 'org' && (
            <Button
              variant="ghost"
              size="icon"
              aria-label={`delete ${node.displayName}`}
              data-testid={`tree-delete-${node.name}`}
              onClick={() => {
                setDeleteError(null)
                setDeleteOpen(true)
              }}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {/* Children — rendered when expanded */}
      {isExpanded &&
        node.children.map((child) => (
          <TreeNode
            key={child.name}
            node={child}
            depth={depth + 1}
            expanded={expanded}
            onToggle={onToggle}
            organization={organization}
          />
        ))}

      {/* Delete dialog */}
      {node.type !== 'org' && (
        <ConfirmDeleteDialog
          open={deleteOpen}
          onOpenChange={(open) => {
            if (!open) setDeleteOpen(false)
          }}
          displayName={node.displayName}
          name={node.name}
          namespace={organization}
          onConfirm={handleDeleteConfirm}
          isDeleting={isDeleting}
          error={deleteError}
        />
      )}
    </>
  )
}
