import { createFileRoute, Navigate, Outlet, useMatchRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/projects/$projectName')({
  component: ProjectLayout,
})

function ProjectLayout() {
  const { projectName } = Route.useParams()
  const matchRoute = useMatchRoute()
  const isExact = matchRoute({ to: '/projects/$projectName', params: { projectName } })

  if (isExact) {
    return <Navigate to="/projects/$projectName/secrets" params={{ projectName }} replace />
  }

  return <Outlet />
}
