import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/templates/')({
  component: OrgTemplatesIndexRoute,
})

function OrgTemplatesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesIndexPage orgName={orgName} />
}

export function OrgTemplatesIndexPage({
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
    <Card data-testid="org-templates-index-placeholder">
      <CardHeader>
        <p className="text-sm text-muted-foreground">{orgName} / Templates</p>
        <CardTitle className="mt-1">Templates</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-sm text-muted-foreground">
          Not implemented yet. This page will consolidate org, folder, and
          project templates into one searchable data grid.
        </p>
      </CardContent>
    </Card>
  )
}
