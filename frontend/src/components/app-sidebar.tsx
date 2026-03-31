import { Link, useRouter } from '@tanstack/react-router'
import {
  Info,
  KeyRound,
  Settings,
  User,
  ChevronsUpDown,
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
  SidebarSeparator,
} from '@/components/ui/sidebar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useOrg } from '@/lib/org-context'
import { useProject } from '@/lib/project-context'
import { useVersion } from '@/queries/version'

const bottomItems = [
  { label: 'About', to: '/about' as const, icon: Info },
  { label: 'Profile', to: '/profile' as const, icon: User },
]

export function AppSidebar() {
  const { data: versionData } = useVersion()
  const router = useRouter()
  const pathname = router.state.location.pathname
  const { selectedProject } = useProject()

  const navItems = selectedProject
    ? [
        {
          label: 'Secrets',
          to: '/projects/$projectName/secrets' as const,
          params: { projectName: selectedProject },
          icon: KeyRound,
        },
        {
          label: 'Settings',
          to: '/projects/$projectName' as const,
          params: { projectName: selectedProject },
          icon: Settings,
        },
      ]
    : []

  return (
    <Sidebar>
      <SidebarHeader className="px-4 py-3">
        <div className="font-semibold text-lg">Holos Console</div>
        {versionData?.version && (
          <div className="text-xs text-muted-foreground">{versionData.version}</div>
        )}
      </SidebarHeader>

      <SidebarSeparator />

      <OrgPicker />
      <ProjectPicker />

      <SidebarSeparator />

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              {navItems.map((item) => (
                <SidebarMenuItem key={item.label}>
                  <SidebarMenuButton asChild isActive={pathname.startsWith(
                    item.params
                      ? `/projects/${item.params.projectName}`
                      : item.to
                  )}>
                    <Link to={item.to} params={item.params ?? {}}>
                      <item.icon className="h-4 w-4" />
                      <span>{item.label}</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              ))}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter>
        <SidebarSeparator />
        <SidebarMenu>
          {bottomItems.map((item) => (
            <SidebarMenuItem key={item.label}>
              <SidebarMenuButton asChild isActive={pathname.startsWith(item.to)}>
                <Link to={item.to}>
                  <item.icon className="h-4 w-4" />
                  <span>{item.label}</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          ))}
        </SidebarMenu>
        <SidebarSeparator />
      </SidebarFooter>
    </Sidebar>
  )
}

function OrgPicker() {
  const { organizations, selectedOrg, setSelectedOrg, isLoading } = useOrg()

  if (isLoading || organizations.length === 0) return null

  const selectedOrgObj = organizations.find((o) => o.name === selectedOrg)
  const displayLabel = selectedOrgObj
    ? (selectedOrgObj.displayName || selectedOrgObj.name)
    : 'All Organizations'

  return (
    <div className="px-2 py-1">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button data-testid="org-picker" className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm hover:bg-accent">
            <span className="truncate">{displayLabel}</span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="start">
          <DropdownMenuItem onClick={() => setSelectedOrg(null)}>
            All Organizations
          </DropdownMenuItem>
          {organizations.map((org) => (
            <DropdownMenuItem
              key={org.name}
              onClick={() => setSelectedOrg(org.name)}
            >
              {org.displayName || org.name}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}

function ProjectPicker() {
  const { selectedOrg } = useOrg()
  const { projects, selectedProject, setSelectedProject, isLoading } = useProject()
  const router = useRouter()

  // Only show when an org is selected
  if (!selectedOrg) return null
  if (isLoading) return null

  const selectedProjectObj = projects.find((p) => p.name === selectedProject)
  const displayLabel = selectedProjectObj
    ? (selectedProjectObj.displayName || selectedProjectObj.name)
    : 'All Projects'

  return (
    <div className="px-2 py-1">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button data-testid="project-picker" className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm hover:bg-accent">
            <span className="truncate">{displayLabel}</span>
            <ChevronsUpDown className="ml-2 h-4 w-4 shrink-0 opacity-50" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent className="w-56" align="start">
          <DropdownMenuItem onClick={() => setSelectedProject(null)}>
            All Projects
          </DropdownMenuItem>
          {projects.map((project) => (
            <DropdownMenuItem
              key={project.name}
              onClick={() => {
                setSelectedProject(project.name)
                router.navigate({
                  to: '/projects/$projectName/secrets',
                  params: { projectName: project.name },
                })
              }}
            >
              {project.displayName || project.name}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
