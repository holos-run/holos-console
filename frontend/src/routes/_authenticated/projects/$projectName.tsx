import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useProject } from '@/lib/project-context'

export const Route = createFileRoute('/_authenticated/projects/$projectName')({
  component: RouteComponent,
})

function RouteComponent() {
  const { projectName } = Route.useParams()
  return <ProjectLayout projectName={projectName} />
}

export function ProjectLayout({ projectName }: { projectName: string }) {
  const { setSelectedProject } = useProject()

  useEffect(() => {
    setSelectedProject(projectName)
  }, [projectName, setSelectedProject])

  return <Outlet />
}
