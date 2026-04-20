import { useEffect } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { namespaceForOrg } from '@/lib/scope-labels'

// HOL-607 retired the scope-specific org-template editor. This route remains
// as a redirect shim to preserve existing bookmarks; all editing now happens
// on the consolidated editor at /orgs/$orgName/templates/$namespace/$name.
// Cleanup (HOL-612) may remove this file entirely once links settle.
export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/settings/org-templates/$templateName',
)({
  component: OrgTemplateRedirect,
})

export function OrgTemplateRedirect() {
  const { orgName, templateName } = Route.useParams()
  const navigate = useNavigate()
  useEffect(() => {
    navigate({
      to: '/orgs/$orgName/templates/$namespace/$name',
      params: {
        orgName,
        namespace: namespaceForOrg(orgName),
        name: templateName,
      },
      replace: true,
    })
  }, [orgName, templateName, navigate])
  return (
    <Card>
      <CardContent className="pt-6 space-y-4">
        <span className="sr-only" role="status">
          Redirecting to consolidated template editor…
        </span>
        <Skeleton className="h-5 w-48" />
        <Skeleton className="h-40 w-full" />
      </CardContent>
    </Card>
  )
}
