import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Role } from '@/gen/holos/console/v1/rbac_pb'
import { useListTemplatePolicyBindings } from '@/queries/templatePolicyBindings'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/template-policy-bindings/',
)({
  component: FolderTemplatePolicyBindingsIndexRoute,
})

function FolderTemplatePolicyBindingsIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderTemplatePolicyBindingsIndexPage folderName={folderName} />
}

export function FolderTemplatePolicyBindingsIndexPage({
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
  const { data: bindings, isPending, error } = useListTemplatePolicyBindings(namespace)

  const userRole = folder?.userRole ?? Role.VIEWER
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
            <Link
              to="/orgs/$orgName/settings"
              params={{ orgName }}
              className="hover:underline"
            >
              {orgName}
            </Link>
            {' / '}
            <Link
              to="/orgs/$orgName/resources"
              params={{ orgName }}
              className="hover:underline"
            >
              Resources
            </Link>
            {' / '}
            <Link
              to="/folders/$folderName/settings"
              params={{ folderName }}
              className="hover:underline"
            >
              {folderName}
            </Link>
            {' / Template Policy Bindings'}
          </p>
          <CardTitle className="mt-1">Template Policy Bindings</CardTitle>
        </div>
        {canWrite && (
          <Link
            to="/folders/$folderName/template-policy-bindings/new"
            params={{ folderName }}
          >
            <Button size="sm">Create Binding</Button>
          </Link>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Bindings attach a single TemplatePolicy to an explicit list of project
          templates and deployments. Every target is named directly — no glob
          patterns. Bindings live only at folder or organization scope.
        </p>
        <Separator />
        {bindings && bindings.length > 0 ? (
          <ul className="space-y-2" data-testid="bindings-list">
            {bindings.map((binding) => (
              <li key={binding.name}>
                <Link
                  to="/folders/$folderName/template-policy-bindings/$bindingName"
                  params={{ folderName, bindingName: binding.name }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors border border-border"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium font-mono">
                        {binding.name}
                      </span>
                      <Badge variant="outline" className="text-xs">
                        {binding.targetRefs.length} target
                        {binding.targetRefs.length === 1 ? '' : 's'}
                      </Badge>
                      {binding.policyRef?.name && (
                        <Badge
                          variant="outline"
                          className="text-xs border-blue-500/30 text-blue-500"
                        >
                          policy: {binding.policyRef.name}
                        </Badge>
                      )}
                    </div>
                    {binding.description && (
                      <p className="text-xs text-muted-foreground truncate mt-0.5">
                        {binding.description}
                      </p>
                    )}
                    {binding.creatorEmail && (
                      <p className="text-xs text-muted-foreground mt-0.5">
                        Created by {binding.creatorEmail}
                      </p>
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">
              No template policy bindings yet.
            </p>
            <p className="mt-1 text-sm text-muted-foreground">
              Bindings enumerate the explicit project templates and deployments
              a TemplatePolicy applies to. Create a binding to attach a policy
              to a specific target set in this folder.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
