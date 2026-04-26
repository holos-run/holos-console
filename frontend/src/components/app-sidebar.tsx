import type React from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import {
  Box,
  ChevronRight,
  KeyRound,
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
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
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
import { WorkspaceMenu } from '@/components/workspace-menu'

// NavItem describes a top-level flat nav entry.
interface NavItem {
  label: string
  icon: React.ComponentType<{ className?: string }>
  href: string
  to?: string
  params?: Record<string, string>
  disabled: boolean
  disabledReason?: string
}

// TemplatesSubLink describes a single leaf link inside the Templates collapsible group.
interface TemplatesSubLink {
  label: string
  href: string
  to: string
  params: Record<string, string>
  testId: string
}

// TemplatesSubGroup groups related sub-links under a labelled section heading.
interface TemplatesSubGroup {
  heading: string
  links: TemplatesSubLink[]
}

export function AppSidebar() {
  const { data: versionData } = useVersion()
  const { pathname } = useLocation()
  const { selectedOrg } = useOrg()
  const { selectedProject } = useProject()

  const hasOrg = Boolean(selectedOrg)
  const hasProject = Boolean(selectedProject)

  const navItems: NavItem[] = [
    {
      label: 'Project',
      icon: Box,
      href: hasProject ? `/projects/${selectedProject}` : '#',
      to: '/projects/$projectName/',
      params: hasProject ? { projectName: selectedProject! } : undefined,
      disabled: !hasProject,
      disabledReason: 'Select a project to view Project',
    },
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
  ]

  const templatesRootHref = hasOrg
    ? `/organizations/${selectedOrg}/templates`
    : hasProject
      ? `/projects/${selectedProject}/templates`
      : '#'
  const templatesDisabled = !hasOrg && !hasProject
  const templatesDisabledReason = 'Select an organization to view Templates'

  // Sub-links within the Templates collapsible group (project-scoped, HOL-1009 and HOL-1013).
  const templatesSubGroups: TemplatesSubGroup[] = hasProject
    ? [
        {
          heading: 'Policy',
          links: [
            {
              label: 'Template Policies',
              href: `/projects/${selectedProject}/templates/policies`,
              to: '/projects/$projectName/templates/policies/',
              params: { projectName: selectedProject! },
              testId: 'nav-template-policies',
            },
            {
              label: 'Policy Bindings',
              href: `/projects/${selectedProject}/templates/policy-bindings`,
              to: '/projects/$projectName/templates/policy-bindings/',
              params: { projectName: selectedProject! },
              testId: 'nav-policy-bindings',
            },
          ],
        },
        {
          heading: 'Dependencies',
          links: [
            {
              label: 'Template Dependencies',
              href: `/projects/${selectedProject}/templates/dependencies`,
              to: '/projects/$projectName/templates/dependencies/',
              params: { projectName: selectedProject! },
              testId: 'nav-template-dependencies',
            },
            {
              label: 'Requirements',
              href: `/projects/${selectedProject}/templates/requirements`,
              to: '/projects/$projectName/templates/requirements/',
              params: { projectName: selectedProject! },
              testId: 'nav-requirements',
            },
          ],
        },
        {
          heading: 'Grants',
          links: [
            {
              label: 'Template Grants',
              href: `/projects/${selectedProject}/templates/grants`,
              to: '/projects/$projectName/templates/grants/',
              params: { projectName: selectedProject! },
              testId: 'nav-template-grants',
            },
          ],
        },
      ]
    : []

  // Expand the Templates group when any descendant route (or root) is active.
  const allSubHrefs = templatesSubGroups
    .flatMap((g) => g.links)
    .map((l) => l.href)
  const isTemplatesActive =
    templatesRootHref !== '#' &&
    (pathname === templatesRootHref ||
      pathname.startsWith(`${templatesRootHref}/`) ||
      allSubHrefs.some(
        (h) => pathname === h || pathname.startsWith(`${h}/`),
      ))

  return (
    <Sidebar>
      <SidebarHeader className="px-2 py-2">
        {/* WorkspaceMenu provides org/project selection, profile, and dev tools. */}
        <WorkspaceMenu />
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {/* Flat nav items: Project, Secrets, Deployments */}
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

              {/* Templates — collapsible group when enabled;
                  disabled tooltip button when no org or project is selected. */}
              {templatesDisabled ? (
                <SidebarMenuItem>
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <SidebarMenuButton
                          disabled
                          aria-disabled="true"
                          data-testid="nav-templates"
                        >
                          <LayoutTemplate className="h-4 w-4" />
                          <span>Templates</span>
                        </SidebarMenuButton>
                      </TooltipTrigger>
                      <TooltipContent side="right">
                        {templatesDisabledReason}
                      </TooltipContent>
                    </Tooltip>
                  </TooltipProvider>
                </SidebarMenuItem>
              ) : (
                <Collapsible
                  asChild
                  open={isTemplatesActive}
                  className="group/collapsible"
                >
                  <SidebarMenuItem>
                    <CollapsibleTrigger asChild>
                      <SidebarMenuButton
                        asChild
                        isActive={isTemplatesActive}
                        data-testid="nav-templates"
                      >
                        <Link
                          to={
                            hasOrg
                              ? '/organizations/$orgName/templates'
                              : '/projects/$projectName/templates'
                          }
                          params={
                            hasOrg
                              ? { orgName: selectedOrg! }
                              : { projectName: selectedProject! }
                          }
                        >
                          <LayoutTemplate className="h-4 w-4" />
                          <span>Templates</span>
                          <ChevronRight className="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90" />
                        </Link>
                      </SidebarMenuButton>
                    </CollapsibleTrigger>

                    <CollapsibleContent>
                      {templatesSubGroups.map((group) => (
                        <SidebarMenuSub key={group.heading}>
                          <SidebarMenuSubItem>
                            <span
                              className="px-2 py-1 text-xs font-semibold text-muted-foreground uppercase tracking-wider"
                              data-testid={`nav-group-${group.heading.toLowerCase()}`}
                            >
                              {group.heading}
                            </span>
                          </SidebarMenuSubItem>
                          {group.links.map((link) => {
                            const isLinkActive =
                              pathname === link.href ||
                              pathname.startsWith(`${link.href}/`)
                            return (
                              <SidebarMenuSubItem key={link.label}>
                                <SidebarMenuSubButton
                                  asChild
                                  isActive={isLinkActive}
                                  data-testid={link.testId}
                                >
                                  <Link to={link.to} params={link.params}>
                                    <span>{link.label}</span>
                                  </Link>
                                </SidebarMenuSubButton>
                              </SidebarMenuSubItem>
                            )
                          })}
                        </SidebarMenuSub>
                      ))}
                    </CollapsibleContent>
                  </SidebarMenuItem>
                </Collapsible>
              )}
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
