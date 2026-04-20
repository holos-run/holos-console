import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// HOL-605: placeholder so the sidebar Organization > Resources link does not
// 404. The real resources index (unifying Folders + Projects) lands in a
// subsequent phase.
export const Route = createFileRoute('/_authenticated/orgs/$orgName/resources/')({
  component: OrgResourcesIndexRoute,
})

function OrgResourcesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgResourcesIndexPage orgName={orgName} />
}

export function OrgResourcesIndexPage({ orgName }: { orgName?: string } = {}) {
  return (
    <Card>
      <CardHeader>
        <p className="text-sm text-muted-foreground">
          {orgName ?? ''}
          {' / Resources'}
        </p>
        <CardTitle className="mt-1">Resources</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-muted-foreground">Not implemented yet.</p>
      </CardContent>
    </Card>
  )
}
