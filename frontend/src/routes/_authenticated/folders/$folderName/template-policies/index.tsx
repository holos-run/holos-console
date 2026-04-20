import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import {
  useListTemplatePolicies,
  countRulesByKind,
} from '@/queries/templatePolicies'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policies/',
)({
  component: FolderTemplatePoliciesIndexRoute,
})

function FolderTemplatePoliciesIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderTemplatePoliciesIndexPage folderName={folderName} />
}

export function FolderTemplatePoliciesIndexPage({
  folderName: propFolderName,
}: { folderName?: string } = {}) {
  let routeFolderName: string | undefined
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeFolderName = Route.useParams().folderName
  } catch {
    routeFolderName = undefined
  }
  const folderName = propFolderName ?? routeFolderName ?? ''

  const { data: folder } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''

  const namespace = namespaceForFolder(folderName)
  const { data: policies, isPending, error } = useListTemplatePolicies(namespace)

  const userRole = folder?.userRole ?? Role.VIEWER
  // PERMISSION_TEMPLATE_POLICIES_WRITE cascades to editors too.
  const canWrite = userRole === Role.OWNER || userRole === Role.EDITOR

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
          <p className="text-sm text-muted-foreground">
            <Link to="/orgs/$orgName/settings" params={{ orgName }} className="hover:underline">
              {orgName}
            </Link>
            {' / '}
            <Link to="/orgs/$orgName/folders" params={{ orgName }} className="hover:underline">
              Folders
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/settings"
              params={{ folderName }}
              className="hover:underline"
            >
              {folderName}
            </Link>
            {' / Template Policies'}
          </p>
          <CardTitle className="mt-1">Template Policies</CardTitle>
        </div>
        {canWrite && (
          <Link to="/folders/$folderName/template-policies/new" params={{ folderName }}>
            <Button size="sm">Create Policy</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Template policies attach templates to projects via REQUIRE or EXCLUDE rules. Rules apply
          to BOTH project templates and deployments. Policies live only at folder or organization
          scope — they can never be authored inside a project.
        </p>
        <Separator />
        {policies && policies.length > 0 ? (
          <ul className="space-y-2" data-testid="policies-list">
            {policies.map((policy) => {
              const counts = countRulesByKind(policy)
              return (
                <li key={policy.name}>
                  <Link
                    to="/folders/$folderName/template-policies/$policyName"
                    params={{ folderName, policyName: policy.name }}
                    className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors border border-border"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span className="text-sm font-medium font-mono">{policy.name}</span>
                        {counts.require > 0 && (
                          <Badge
                            variant="outline"
                            className="text-xs border-green-500/30 text-green-500"
                          >
                            REQUIRE x {counts.require}
                          </Badge>
                        )}
                        {counts.exclude > 0 && (
                          <Badge
                            variant="outline"
                            className="text-xs border-amber-500/30 text-amber-500"
                          >
                            EXCLUDE x {counts.exclude}
                          </Badge>
                        )}
                      </div>
                      {policy.description && (
                        <p className="text-xs text-muted-foreground truncate mt-0.5">
                          {policy.description}
                        </p>
                      )}
                      {policy.creatorEmail && (
                        <p className="text-xs text-muted-foreground mt-0.5">
                          Created by {policy.creatorEmail}
                        </p>
                      )}
                    </div>
                  </Link>
                </li>
              )
            })}
          </ul>
        ) : (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">No template policies yet.</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Policies attach templates to projects through REQUIRE or EXCLUDE rules. Rules apply
              to both project templates and deployments. Create a policy to enforce a template
              across every project in this folder.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
