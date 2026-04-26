/**
 * Org home — thin link landing (HOL-977).
 *
 * No heavy queries are fired here. The page renders navigation links to the
 * main sub-sections of the organization. Heavy data is deferred to each sub-page.
 */

import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute(
  '/_authenticated/organizations/$orgName/',
)({
  component: OrgIndexRoute,
})

function OrgIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgIndexPage orgName={orgName} />
}

export function OrgIndexPage({ orgName }: { orgName: string }) {
  if (!orgName) return null

  const sections = [
    {
      title: 'Projects',
      href: `/organizations/${orgName}/projects`,
      description: 'View and manage projects in this organization.',
    },
    {
      title: 'Templates',
      href: `/organizations/${orgName}/templates`,
      description: 'Browse platform-level templates for this organization.',
    },
    {
      title: 'Template Policies',
      href: `/organizations/${orgName}/template-policies`,
      description: 'Manage template policies governing allowed deployments.',
    },
    {
      title: 'Template Bindings',
      href: `/organizations/${orgName}/template-bindings`,
      description: 'Manage which templates are bound to this organization.',
    },
    {
      title: 'Settings',
      href: `/organizations/${orgName}/settings`,
      description: 'Organization settings and configuration.',
    },
  ]

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{orgName}</h1>
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
