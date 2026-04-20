import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/resources/')({
  component: ResourcesIndexRoute,
})

function ResourcesIndexRoute() {
  const { orgName } = Route.useParams()
  return <ResourcesIndexPage orgName={orgName} />
}

export function ResourcesIndexPage({
  orgName: propOrgName,
}: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  return (
    <Card data-testid="resources-index-placeholder">
      <CardHeader>
        <p className="text-sm text-muted-foreground">{orgName} / Resources</p>
        <CardTitle className="mt-1">Resources</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          Not implemented yet. This page will list folders and projects as a
          flat searchable data grid.
        </p>
      </CardContent>
    </Card>
  )
}
