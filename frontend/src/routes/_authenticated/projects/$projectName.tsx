import { createFileRoute, Navigate, Outlet, useMatchRoute } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useProject } from '@/lib/project-context'
import { useOrg } from '@/lib/org-context'
import { useGetProject } from '@/queries/projects'

export const Route = createFileRoute('/_authenticated/projects/$projectName')({
  component: RouteComponent,
})

function RouteComponent() {
  const { projectName } = Route.useParams()
  return <ProjectLayout projectName={projectName} />
}

export function ProjectLayout({ projectName }: { projectName: string }) {
  const matchRoute = useMatchRoute()
  const isExact = matchRoute({ to: '/projects/$projectName', params: { projectName } })
  const { setSelectedProject } = useProject()
  const { selectedOrg, setSelectedOrg } = useOrg()
  const { data: project } = useGetProject(projectName)

  useEffect(() => {
    setSelectedProject(projectName)
  }, [projectName, setSelectedProject])

  useEffect(() => {
    if (project?.organization && project.organization !== selectedOrg) {
      setSelectedOrg(project.organization)
    }
  }, [project?.organization, selectedOrg, setSelectedOrg])

  if (isExact) {
    return <Navigate to="/projects/$projectName/secrets" params={{ projectName }} replace />
  }

  return <Outlet />
}
