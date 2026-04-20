import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

// HOL-605: placeholder so the sidebar Organization > Templates link does not
// 404. The real unified Templates index lands in a subsequent phase.
export const Route = createFileRoute('/_authenticated/orgs/$orgName/templates/')({
  component: OrgTemplatesIndexRoute,
})

function OrgTemplatesIndexRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesIndexPage orgName={orgName} />
}

export function OrgTemplatesIndexPage({ orgName }: { orgName?: string } = {}) {
  return (
    <Card>
      <CardHeader>
        <p className="text-sm text-muted-foreground">
          {orgName ?? ''}
          {' / Templates'}
        </p>
        <CardTitle className="mt-1">Templates</CardTitle>
      </CardHeader>
      <CardContent>
        <p className="text-muted-foreground">Not implemented yet.</p>
      </CardContent>
    </Card>
  )
}
