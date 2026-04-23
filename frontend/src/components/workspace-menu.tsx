import { Link } from '@tanstack/react-router'
import { Box, ChevronsUpDown, Info, Settings, ArrowRightLeft, FolderKanban, User, Wrench } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { getConsoleConfig } from '@/lib/console-config'

/**
 * WorkspaceMenu is the Linear-style "Holos Console" menu rendered at the top
 * of the sidebar. It replaces the previous stacked OrgPicker + ProjectPicker
 * dropdowns. Menu items appear in the canonical order:
 *
 *   About, Settings, Switch Projects, Switch organization, separator, Profile, Dev Tools
 *
 * Dev Tools is only visible when the server gates it on via `--enable-dev-tools`
 * (mirrored to the frontend via `getConsoleConfig().devToolsEnabled`). The
 * `Settings` item routes to org settings when an org is selected; when no org
 * is selected it is rendered disabled (there is no global settings route) so
 * the canonical item order stays visible in every state. `Switch Projects`
 * links to the org-scoped projects list and is disabled when no org is selected.
 */
export function WorkspaceMenu() {
  const { selectedOrg, organizations } = useOrg()
  const { selectedProject, projects } = useProject()
  const { devToolsEnabled } = getConsoleConfig()

  const selectedOrgObj = organizations.find((o) => o.name === selectedOrg)
  const orgDisplayName = selectedOrgObj
    ? selectedOrgObj.displayName || selectedOrgObj.name
    : null

  const selectedProjectObj = projects.find((p) => p.name === selectedProject)
  const projectDisplayName = selectedProjectObj
    ? selectedProjectObj.displayName || selectedProjectObj.name
    : null

  // Trigger label priority: Project > Org > "Holos Console". This mirrors the
  // Linear convention of surfacing the most specific scope at the top of the
  // sidebar.
  const triggerLabel = projectDisplayName ?? orgDisplayName ?? 'Holos Console'

  return (
    <div className="px-2 py-1">
      <p className="px-1 pb-1 text-xs font-semibold tracking-widest text-muted-foreground uppercase select-none">
        Holos
      </p>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            data-testid="workspace-menu"
            className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm hover:bg-accent"
          >
            <span className="flex items-center gap-2 truncate">
              {/*
                Placeholder project glyph. HOL-603 deliberately picks a
                generic icon (Box) so we can swap in a user-customizable
                logo in a follow-up phase without changing the layout.
              */}
              <Box className="h-4 w-4 shrink-0 opacity-70" aria-hidden="true" />
              <span className="truncate">{triggerLabel}</span>
            </span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="start">
          <DropdownMenuItem asChild>
            <Link to="/about" data-testid="workspace-menu-item-about">
              <Info className="h-4 w-4" />
              <span>About</span>
            </Link>
          </DropdownMenuItem>
          {selectedOrg ? (
            <DropdownMenuItem asChild>
              <Link
                to="/orgs/$orgName/settings"
                params={{ orgName: selectedOrg }}
                data-testid="workspace-menu-item-settings"
              >
                <Settings className="h-4 w-4" />
                <span>Settings</span>
              </Link>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem disabled data-testid="workspace-menu-item-settings">
              <Settings className="h-4 w-4" />
              <span>Settings</span>
            </DropdownMenuItem>
          )}
          {selectedOrg ? (
            <DropdownMenuItem asChild>
              <Link
                to="/organizations/$orgName/projects"
                params={{ orgName: selectedOrg }}
                data-testid="workspace-menu-item-switch-projects"
              >
                <FolderKanban className="h-4 w-4" />
                <span>Switch Projects</span>
              </Link>
            </DropdownMenuItem>
          ) : (
            <DropdownMenuItem disabled data-testid="workspace-menu-item-switch-projects">
              <FolderKanban className="h-4 w-4" />
              <span>Switch Projects</span>
            </DropdownMenuItem>
          )}
          <DropdownMenuItem asChild>
            <Link to="/organizations" data-testid="workspace-menu-item-switch-organization">
              <ArrowRightLeft className="h-4 w-4" />
              <span>Switch organization</span>
            </Link>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem asChild>
            <Link to="/profile" data-testid="workspace-menu-item-profile">
              <User className="h-4 w-4" />
              <span>Profile</span>
            </Link>
          </DropdownMenuItem>
          {devToolsEnabled ? (
            <DropdownMenuItem asChild>
              <Link to="/dev-tools" data-testid="workspace-menu-item-dev-tools">
                <Wrench className="h-4 w-4" />
                <span>Dev Tools</span>
              </Link>
            </DropdownMenuItem>
          ) : null}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
