import type React from 'react'
import { Link, useRouter } from '@tanstack/react-router'
import {
  KeyRound,
  FolderTree,
  LayoutTemplate,
  Layers,
} from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from '@/components/ui/sidebar'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'
import { WorkspaceMenu } from '@/components/workspace-menu'

// NavItem describes a top-level flat nav entry.
interface NavItem {
  label: string
  icon: React.ComponentType<{ className?: string }>
  // href is the fully-resolved href string for the anchor (used by the Link mock in tests).
  href: string
  // to / params are the TanStack Router typed route args; undefined for
  // always-enabled top-level routes that don't take params.
  to?: string
  params?: Record<string, string>
  // When disabled, the link is replaced by a Tooltip explaining the prerequisite.
  disabled: boolean
  disabledReason?: string
}

export function AppSidebar() {
  const { data: versionData } = useVersion()
  const router = useRouter()
  const pathname = router.state.location.pathname
  const { selectedProject } = useProject()

  const hasProject = Boolean(selectedProject)

  // Flat 4-item nav: Secrets, Deployments, Templates, Resource Manager.
  // The first three items are scoped to the selected project and are disabled
  // until a project is chosen from the WorkspaceMenu. Resource Manager is a
  // top-level route that is always enabled.
  const navItems: NavItem[] = [
    {
      label: 'Secrets',
      icon: KeyRound,
      href: hasProject ? `/projects/${selectedProject}/secrets` : '#',
      to: '/projects/$projectName/secrets',
      params: hasProject ? { projectName: selectedProject! } : undefined,
      disabled: !hasProject,
      disabledReason: 'Select a project to view Secrets',
    },
    {
      label: 'Deployments',
      icon: Layers,
      href: hasProject ? `/projects/${selectedProject}/deployments` : '#',
      to: '/projects/$projectName/deployments',
      params: hasProject ? { projectName: selectedProject! } : undefined,
      disabled: !hasProject,
      disabledReason: 'Select a project to view Deployments',
    },
    {
      label: 'Templates',
      icon: LayoutTemplate,
      href: hasProject ? `/projects/${selectedProject}/templates` : '#',
      to: '/projects/$projectName/templates',
      params: hasProject ? { projectName: selectedProject! } : undefined,
      disabled: !hasProject,
      disabledReason: 'Select a project to view Templates',
    },
    {
      label: 'Resource Manager',
      icon: FolderTree,
      href: '/resource-manager',
      to: '/resource-manager',
      params: undefined,
      disabled: false,
    },
  ]

  return (
    <Sidebar>
      <SidebarHeader className="px-2 py-2">
          {/* WorkspaceMenu provides org/project selection, profile, and dev tools. */}
        <WorkspaceMenu />
      </SidebarHeader>

      <SidebarContent>
        {/* Flat 4-item nav — Secrets, Deployments, Templates, Resource Manager */}
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {navItems.map((item) => {
                const resolvedPath = item.href
                const isActive =
                  resolvedPath !== '#' &&
                  (pathname === resolvedPath ||
                    pathname.startsWith(`${resolvedPath}/`))

                if (item.disabled) {
                  return (
                    <SidebarMenuItem key={item.label}>
                      <TooltipProvider>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <SidebarMenuButton
                              disabled
                              aria-disabled="true"
                              data-testid={`nav-${item.label.toLowerCase().replace(/\s+/g, '-')}`}
                            >
                              <item.icon className="h-4 w-4" />
                              <span>{item.label}</span>
                            </SidebarMenuButton>
                          </TooltipTrigger>
                          <TooltipContent side="right">
                            {item.disabledReason}
                          </TooltipContent>
                        </Tooltip>
                      </TooltipProvider>
                    </SidebarMenuItem>
                  )
                }

                return (
                  <SidebarMenuItem key={item.label}>
                    <SidebarMenuButton
                      asChild
                      isActive={isActive}
                      data-testid={`nav-${item.label.toLowerCase().replace(/\s+/g, '-')}`}
                    >
                      <Link
                        to={item.to as string}
                        params={item.params ?? {}}
                      >
                        <item.icon className="h-4 w-4" />
                        <span>{item.label}</span>
                      </Link>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                )
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      {/* Version label in the footer, bottom-left. */}
      {versionData?.version && (
        <SidebarFooter>
          <div className="px-2 py-2 text-xs text-muted-foreground">
            {versionData.version}
          </div>
        </SidebarFooter>
      )}
    </Sidebar>
  )
}
