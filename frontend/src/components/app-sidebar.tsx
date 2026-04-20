import type React from 'react'
import { Link, useRouter } from '@tanstack/react-router'
import {
  Boxes,
  Building2,
  ChevronRight,
  KeyRound,
  FolderKanban,
  LayoutTemplate,
  Layers,
  Settings,
  Shield,
} from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarSeparator,
} from '@/components/ui/sidebar'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'
import { useGetProjectSettings } from '@/queries/project-settings'
import { WorkspaceMenu } from '@/components/workspace-menu'

export function AppSidebar() {
  const { data: versionData } = useVersion()
  const router = useRouter()
  const pathname = router.state.location.pathname
  const { projects, selectedProject } = useProject()
  const { selectedOrg, organizations } = useOrg()
  const { data: projectSettings } = useGetProjectSettings(selectedProject ?? '')

  const selectedOrgObj = organizations.find((o) => o.name === selectedOrg)
  const orgDisplayName = selectedOrgObj
    ? (selectedOrgObj.displayName || selectedOrgObj.name)
    : selectedOrg ?? ''

  const selectedProjectObj = projects.find((p) => p.name === selectedProject)
  const projectDisplayName = selectedProjectObj
    ? (selectedProjectObj.displayName || selectedProjectObj.name)
    : selectedProject ?? ''

  // HOL-605 restructures the org-scope nav into a collapsible "Organization"
  // tree that mirrors the Project tree. Children order is canonical:
  // Resources, Templates, Template Policies. The former standalone Folders /
  // Projects / Template Policies / Org Settings group is gone — Resources
  // supersedes Folders + Projects, and Org Settings moved to the workspace
  // menu (HOL-603).
  const orgNavItems: Array<{
    label: string
    to: string
    params: Record<string, string>
    icon: React.ComponentType<{ className?: string }>
  }> = selectedOrg
    ? [
        {
          label: 'Resources',
          to: '/orgs/$orgName/resources' as const,
          params: { orgName: selectedOrg },
          icon: Boxes,
        },
        {
          label: 'Templates',
          to: '/orgs/$orgName/templates' as const,
          params: { orgName: selectedOrg },
          icon: LayoutTemplate,
        },
        {
          label: 'Template Policies',
          to: '/orgs/$orgName/template-policies' as const,
          params: { orgName: selectedOrg },
          icon: Shield,
        },
      ]
    : []

  const deploymentsEnabled = projectSettings?.deploymentsEnabled ?? false

  // HOL-604 restructures the project nav into a single collapsible "Project"
  // tree. Children order is canonical: Secrets, Deployments, Templates,
  // Settings. Deployments and Templates remain gated on
  // projectSettings.deploymentsEnabled to preserve the pre-existing feature
  // flag behavior (covered by the "Templates nav item conditional
  // visibility" suite). The Project tree itself is rendered only when a
  // project is selected.
  const projectNavItems: Array<{
    label: string
    to: string
    params: Record<string, string>
    icon: React.ComponentType<{ className?: string }>
  }> = selectedProject
    ? [
        {
          label: 'Secrets',
          to: '/projects/$projectName/secrets' as const,
          params: { projectName: selectedProject },
          icon: KeyRound,
        },
        ...(deploymentsEnabled
          ? [
              {
                label: 'Deployments',
                to: '/projects/$projectName/deployments' as const,
                params: { projectName: selectedProject },
                icon: Layers,
              },
              {
                label: 'Templates',
                to: '/projects/$projectName/templates' as const,
                params: { projectName: selectedProject },
                icon: LayoutTemplate,
              },
            ]
          : []),
        {
          label: 'Settings',
          to: '/projects/$projectName/settings/' as const,
          params: { projectName: selectedProject },
          icon: Settings,
        },
      ]
    : []

  return (
    <Sidebar>
      <SidebarHeader className="px-2 py-2">
        {/*
          HOL-603 replaces the previous stacked OrgPicker + ProjectPicker
          with a single Linear-style workspace menu. Profile / Dev Tools
          have moved off the footer and into this menu so all "global" nav
          lives in one place at the top of the sidebar.
        */}
        <WorkspaceMenu />
        {versionData?.version && (
          <div className="px-2 pt-1 text-xs text-muted-foreground">
            {versionData.version}
          </div>
        )}
      </SidebarHeader>

      <SidebarSeparator />

      <SidebarContent>
        {projectNavItems.length > 0 && (
          <SidebarGroup data-testid="project-tree">
            <SidebarGroupContent>
              <SidebarMenu>
                <Collapsible defaultOpen asChild className="group/collapsible">
                  <SidebarMenuItem>
                    <TooltipProvider>
                      <Tooltip>
                        <CollapsibleTrigger asChild>
                          <TooltipTrigger asChild>
                            <SidebarMenuButton
                              data-testid="project-tree-trigger"
                              isActive={pathname.startsWith('/projects/')}
                            >
                              <FolderKanban className="h-4 w-4" />
                              <span>Project</span>
                              <ChevronRight className="ml-auto h-4 w-4 transition-transform group-data-[state=open]/collapsible:rotate-90" />
                            </SidebarMenuButton>
                          </TooltipTrigger>
                        </CollapsibleTrigger>
                        <TooltipContent
                          side="right"
                          align="start"
                          data-testid="project-tree-tooltip"
                        >
                          <div>{projectDisplayName}</div>
                          <div>{selectedProject}</div>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                    <CollapsibleContent data-testid="project-tree-content">
                      <SidebarMenuSub>
                        {projectNavItems.map((item) => {
                          const activePath = (item.to as string)
                            .replace('$projectName', item.params.projectName)
                            .replace(/\/$/, '')
                          return (
                            <SidebarMenuSubItem key={item.label}>
                              <SidebarMenuSubButton
                                asChild
                                isActive={pathname === activePath || pathname.startsWith(`${activePath}/`)}
                              >
                                <Link to={item.to} params={item.params}>
                                  <item.icon className="h-4 w-4" />
                                  <span>{item.label}</span>
                                </Link>
                              </SidebarMenuSubButton>
                            </SidebarMenuSubItem>
                          )
                        })}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
        {orgNavItems.length > 0 && (
          <SidebarGroup data-testid="organization-tree">
            <SidebarGroupContent>
              <SidebarMenu>
                <Collapsible defaultOpen asChild className="group/collapsible">
                  <SidebarMenuItem>
                    <TooltipProvider>
                      <Tooltip>
                        <CollapsibleTrigger asChild>
                          <TooltipTrigger asChild>
                            <SidebarMenuButton
                              data-testid="organization-tree-trigger"
                              isActive={
                                pathname === `/orgs/${selectedOrg}` ||
                                pathname.startsWith(`/orgs/${selectedOrg}/`)
                              }
                            >
                              <Building2 className="h-4 w-4" />
                              <span>Organization</span>
                              <ChevronRight className="ml-auto h-4 w-4 transition-transform group-data-[state=open]/collapsible:rotate-90" />
                            </SidebarMenuButton>
                          </TooltipTrigger>
                        </CollapsibleTrigger>
                        <TooltipContent
                          side="right"
                          align="start"
                          data-testid="organization-tree-tooltip"
                        >
                          <div>{orgDisplayName}</div>
                          <div>{selectedOrg}</div>
                        </TooltipContent>
                      </Tooltip>
                    </TooltipProvider>
                    <CollapsibleContent data-testid="organization-tree-content">
                      <SidebarMenuSub>
                        {orgNavItems.map((item) => {
                          const activePath = (item.to as string)
                            .replace('$orgName', item.params.orgName)
                            .replace(/\/$/, '')
                          return (
                            <SidebarMenuSubItem key={item.label}>
                              <SidebarMenuSubButton
                                asChild
                                isActive={pathname === activePath || pathname.startsWith(`${activePath}/`)}
                              >
                                <Link to={item.to} params={item.params}>
                                  <item.icon className="h-4 w-4" />
                                  <span>{item.label}</span>
                                </Link>
                              </SidebarMenuSubButton>
                            </SidebarMenuSubItem>
                          )
                        })}
                      </SidebarMenuSub>
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        )}
      </SidebarContent>
    </Sidebar>
  )
}
