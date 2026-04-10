import { createFileRoute, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/orgs/$orgName/folders/$folderName')({
  component: FolderLayout,
})

function FolderLayout() {
  return <Outlet />
}
