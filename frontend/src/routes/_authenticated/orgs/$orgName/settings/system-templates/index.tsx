import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Lock } from 'lucide-react'
import { useListSystemTemplates } from '@/queries/system-templates'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/settings/system-templates/')({
  component: SystemTemplatesListRoute,
})

function SystemTemplatesListRoute() {
  const { orgName } = Route.useParams()
  return <SystemTemplatesListPage orgName={orgName} />
}

export function SystemTemplatesListPage({ orgName: propOrgName }: { orgName?: string } = {}) {
  let routeOrgName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeOrgName = Route.useParams().orgName
  } catch {
    routeOrgName = undefined
  }
  const orgName = propOrgName ?? routeOrgName ?? ''

  const { data: templates, isPending, error } = useListSystemTemplates(orgName)

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
      <CardContent className="pt-6 space-y-4">
        <div>
          <p className="text-sm text-muted-foreground">{orgName} / Settings / System Templates</p>
          <h2 className="text-xl font-semibold mt-1">System Templates</h2>
        </div>
        <p className="text-sm text-muted-foreground">
          System templates are automatically applied to project namespaces when projects are created.
          Mandatory templates are marked with a lock badge.
        </p>
        <Separator />
        {templates && templates.length > 0 ? (
          <ul className="space-y-2">
            {templates.map((tmpl) => (
              <li key={tmpl.name}>
                <Link
                  to="/orgs/$orgName/settings/system-templates/$templateName"
                  params={{ orgName, templateName: tmpl.name }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium font-mono">{tmpl.name}</span>
                      {tmpl.mandatory && (
                        <Badge variant="secondary" className="flex items-center gap-1 text-xs">
                          <Lock className="h-3 w-3" />
                          Mandatory
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
          <p className="text-sm text-muted-foreground">No system templates found.</p>
        )}
      </CardContent>
    </Card>
  )
}
