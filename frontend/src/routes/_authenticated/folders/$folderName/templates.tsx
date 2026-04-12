import { createFileRoute, Outlet } from '@tanstack/react-router'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/templates',
)({
  component: FolderTemplatesLayout,
})

function FolderTemplatesLayout() {
  return <Outlet />
}
