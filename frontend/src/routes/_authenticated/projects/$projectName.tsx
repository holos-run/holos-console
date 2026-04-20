import { createFileRoute, Outlet } from '@tanstack/react-router'
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

  return <Outlet />
}
