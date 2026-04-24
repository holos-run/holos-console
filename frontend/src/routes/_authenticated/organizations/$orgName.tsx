import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useOrg } from '@/lib/org-context'

export const Route = createFileRoute('/_authenticated/organizations/$orgName')({
  component: RouteComponent,
})

function RouteComponent() {
  const { orgName } = Route.useParams()
  return <OrgLayout orgName={orgName} />
}

export function OrgLayout({ orgName }: { orgName: string }) {
  const { setSelectedOrg } = useOrg()

  useEffect(() => {
    setSelectedOrg(orgName)
  }, [orgName, setSelectedOrg])

  return <Outlet />
}
