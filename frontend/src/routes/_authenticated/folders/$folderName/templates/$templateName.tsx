import { useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Card, CardContent } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Skeleton } from '@/components/ui/skeleton'
import { namespaceForFolder } from '@/lib/scope-labels'
import { useGetFolder } from '@/queries/folders'

// HOL-607 retired the scope-specific folder-template editor. This route is a
// redirect shim that resolves the owning organization from the folder record,
// then navigates to the consolidated editor. Cleanup (HOL-612) may remove it
// once links settle.
export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/templates/$templateName',
)({
  component: FolderTemplateRedirect,
})

function FolderTemplateRedirect() {
  const { folderName, templateName } = Route.useParams()
  const navigate = useNavigate()
  const { data: folder, isPending, error } = useGetFolder(folderName)
  const orgName = folder?.organization ?? ''

  useEffect(() => {
    if (!orgName) return
    navigate({
      to: '/orgs/$orgName/templates/$namespace/$name',
      params: {
        orgName,
        namespace: namespaceForFolder(folderName),
        name: templateName,
      },
      replace: true,
    })
  }, [orgName, folderName, templateName, navigate])

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
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-40 w-full" />
        {isPending && <span className="sr-only">Redirecting…</span>}
      </CardContent>
    </Card>
  )
}
