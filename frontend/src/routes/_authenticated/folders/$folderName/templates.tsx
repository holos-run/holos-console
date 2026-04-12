import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Lock } from 'lucide-react'
import { useListTemplates } from '@/queries/templates'
import { useGetFolder } from '@/queries/folders'
import { TemplateScope } from '@/gen/holos/console/v1/templates_pb'
import { create } from '@bufbuild/protobuf'
import { TemplateScopeRefSchema } from '@/gen/holos/console/v1/templates_pb'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/templates',
)({
  component: FolderTemplatesRoute,
})

function FolderTemplatesRoute() {
  const { folderName } = Route.useParams()
  return <FolderTemplatesPage folderName={folderName} />
}

export function FolderTemplatesPage({
  orgName: propOrgName,
  folderName: propFolderName,
}: { orgName?: string; folderName?: string } = {}) {
  let routeParams: { folderName?: string } = {}
  try {
    // eslint-disable-next-line react-hooks/rules-of-hooks
    routeParams = Route.useParams()
  } catch {
    routeParams = {}
  }
  const folderName = propFolderName ?? routeParams.folderName ?? ''

  // Load folder to derive orgName from the response
  const { data: folder } = useGetFolder(folderName)
  const orgName = propOrgName ?? folder?.organization ?? ''

  const scope = create(TemplateScopeRefSchema, {
    scope: TemplateScope.FOLDER,
    scopeName: folderName,
  })
  const { data: templates, isPending, error } = useListTemplates(scope)

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
            {' / Platform Templates'}
          </p>
          <CardTitle className="mt-1">Platform Templates</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Platform templates at folder scope are applied to projects within this folder hierarchy.
          Mandatory templates are marked with a lock badge.
        </p>
        <Separator />
        {templates && templates.length > 0 ? (
          <ul className="space-y-2">
            {templates.map((tmpl) => (
              <li key={tmpl.name} className="flex items-center gap-2 p-3 rounded-md border border-border">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium font-mono">{tmpl.name}</span>
                    {tmpl.mandatory && (
                      <Badge variant="secondary" className="flex items-center gap-1 text-xs">
                        <Lock className="h-3 w-3" />
                        Mandatory
                      </Badge>
                    )}
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
              </li>
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No platform templates found for this folder.</p>
        )}
      </CardContent>
    </Card>
  )
}
