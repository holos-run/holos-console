import { createFileRoute, Link } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { Separator } from '@/components/ui/separator'
import { ParentType } from '@/gen/holos/console/v1/folders_pb'
import { useListProjectsByParent } from '@/queries/projects'
import { useGetFolder } from '@/queries/folders'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/projects/',
)({
  component: FolderProjectsIndexRoute,
})

function FolderProjectsIndexRoute() {
  const { folderName } = Route.useParams()
  return <FolderProjectsIndexPage folderName={folderName} />
}

export function FolderProjectsIndexPage({
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

  // Reuse the non-recursive parent-scoped query the HOL-610 folder index
  // already uses for the Projects summary section. The RPC filter returns
  // only projects whose immediate parent is this folder, never grandchildren.
  const {
    data: projects,
    isPending,
    error,
  } = useListProjectsByParent(orgName, ParentType.FOLDER, folderName)

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
            <Link to="/orgs/$orgName/resources" params={{ orgName }} className="hover:underline">
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
            {' / Projects'}
          </p>
          <CardTitle className="mt-1">Projects</CardTitle>
        </div>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Projects whose immediate parent is this folder. Projects nested inside
          sub-folders appear on their own folder's page.
        </p>
        <Separator />
        {projects && projects.length > 0 ? (
          <ul className="space-y-2" data-testid="projects-list">
            {projects.map((project) => (
              <li key={project.name}>
                <Link
                  to="/projects/$projectName"
                  params={{ projectName: project.name }}
                  className="flex items-center gap-2 p-3 rounded-md hover:bg-muted transition-colors border border-border"
                >
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-medium">
                        {project.displayName || project.name}
                      </span>
                      {project.displayName && project.displayName !== project.name && (
                        <span className="text-xs text-muted-foreground font-mono">
                          {project.name}
                        </span>
                      )}
                    </div>
                    {project.description && (
                      <p className="text-xs text-muted-foreground truncate mt-0.5">
                        {project.description}
                      </p>
                    )}
                    {project.creatorEmail && (
                      <p className="text-xs text-muted-foreground mt-0.5">
                        Created by {project.creatorEmail}
                      </p>
                    )}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        ) : (
          <div className="rounded-md border border-dashed border-border p-6 text-center">
            <p className="text-sm font-medium">No projects in this folder.</p>
            <p className="mt-1 text-sm text-muted-foreground">
              Projects are created from the organization Resources page. Once a
              project's parent is set to this folder it will appear here.
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
