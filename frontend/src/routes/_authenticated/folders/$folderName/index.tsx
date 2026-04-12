import { createFileRoute, Navigate } from '@tanstack/react-router'

export const Route = createFileRoute(
  '/_authenticated/folders/$folderName/',
)({
  component: FolderIndexRedirect,
})

/**
 * Temporary redirect: bare /folders/$folderName routes to settings.
 * Phase 3 will replace this with the folder index page.
 */
function FolderIndexRedirect() {
  const { folderName } = Route.useParams()
  return <Navigate to="/folders/$folderName/settings" params={{ folderName }} replace />
}
