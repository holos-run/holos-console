/**
 * Project home — thin link landing (HOL-977).
 *
 * No heavy queries are fired here. The page renders navigation links to the
 * main sub-sections of the project. Heavy data is deferred to each sub-page.
 */

import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/_authenticated/projects/$projectName/')({
  component: ProjectIndexRoute,
})

function ProjectIndexRoute() {
  const { projectName } = Route.useParams()
  return <ProjectIndexPage projectName={projectName} />
}

export function ProjectIndexPage({ projectName }: { projectName: string }) {
  if (!projectName) return null

  const sections = [
    {
      title: 'Deployments',
      href: `/projects/${projectName}/deployments`,
      description: 'View and manage running applications in this project.',
    },
    {
      title: 'Secrets',
      href: `/projects/${projectName}/secrets`,
      description: 'Manage project-scoped secrets.',
    },
    {
      title: 'Templates',
      href: `/projects/${projectName}/templates`,
      description: 'Browse and author deployment templates.',
    },
    {
      title: 'Settings',
      href: `/projects/${projectName}/settings`,
      description: 'Project settings, sharing, and configuration.',
    },
  ]

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{projectName}</h1>
      <div className="grid gap-4 sm:grid-cols-2">
        {sections.map(({ title, href, description }) => (
          <Link key={title} to={href}>
            <Card className="h-full transition-colors hover:bg-muted/50">
              <CardHeader>
                <CardTitle className="text-base">{title}</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="text-sm text-muted-foreground">{description}</p>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  )
}
