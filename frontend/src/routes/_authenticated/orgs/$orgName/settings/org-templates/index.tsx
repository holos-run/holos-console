import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplates, makeOrgScope } from '@/queries/templates'
import { useGetOrganization } from '@/queries/organizations'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/org-templates/')({
  component: OrgTemplatesListRoute,
})

function OrgTemplatesListRoute() {
  const { orgName } = Route.useParams()
  return <OrgTemplatesListPage orgName={orgName} />
}

export function OrgTemplatesListPage({ orgName: propOrgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const scope = makeOrgScope(orgName)
  const { data: templates, isPending, error } = useListTemplates(scope)
  const { data: org } = useGetOrganization(orgName)

  const userRole = org?.userRole ?? Role.VIEWER
  const canWrite = userRole === Role.OWNER

  if (isPending) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-5 w-48" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </CardContent>
      </Card>
    )
  }

  if (error) {
    return (
      <Card>
        <CardContent className="pt-6">
          <Alert variant="destructive">
            <AlertDescription>{error.message}</AlertDescription>
          </Alert>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-col sm:flex-row items-start sm:items-center justify-between gap-2">
        <div>
          <p className="text-sm text-muted-foreground">{orgName} / Settings / Platform Templates</p>
          <CardTitle className="mt-1">Platform Templates</CardTitle>
        </div>
        {canWrite && (
          <Link to="/orgs/$orgName/settings/org-templates/new" params={{ orgName }}>
            <Button size="sm">Create Template</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Platform templates are automatically applied to project namespaces when projects are created.
          Mandatory templates are marked with a lock badge.
        </p>
        <Separator />
        {templates && templates.length > 0 ? (
          <ul className="space-y-2">
            {templates.map((tmpl) => (
              <li key={tmpl.name}>
                <Link
                  to="/orgs/$orgName/settings/org-templates/$templateName"
                  params={{ orgName, templateName: tmpl.name }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium font-mono">{tmpl.name}</span>
                      {/* Mandatory badge removed in HOL-555; TemplatePolicy
                          REQUIRE rules (HOL-558) will re-introduce an
                          "always applied" affordance. */}
                      {tmpl.enabled ? (
                        <Badge variant="outline" className="text-xs text-green-500 border-green-500/30">
                          Enabled
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="text-xs text-muted-foreground">
                          Disabled
                        </Badge>
                      )}
                    </div>
                    {tmpl.description && (
                      <p className="text-xs text-muted-foreground truncate mt-0.5">{tmpl.description}</p>
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No platform templates found.</p>
        )}
      </CardContent>
    </Card>
  )
}
