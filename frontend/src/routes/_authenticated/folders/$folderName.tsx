import { useEffect } from 'react'
import { createFileRoute, Outlet } from '@tanstack/react-router'
import { useOrg } from '@/lib/org-context'
import { useGetFolder } from '@/queries/folders'

export const Route = createFileRoute('/_authenticated/folders/$folderName')({
  component: FolderLayoutRoute,
})

function FolderLayoutRoute() {
  const { folderName } = Route.useParams()
  return <FolderLayout folderName={folderName} />
}

export function FolderLayout({ folderName }: { folderName: string }) {
  const { selectedOrg, setSelectedOrg } = useOrg()
  const { data: folder } = useGetFolder(folderName)

  useEffect(() => {
    if (folder?.organization && folder.organization !== selectedOrg) {
      setSelectedOrg(folder.organization)
    }
  }, [folder?.organization, selectedOrg, setSelectedOrg])

  return <Outlet />
}
