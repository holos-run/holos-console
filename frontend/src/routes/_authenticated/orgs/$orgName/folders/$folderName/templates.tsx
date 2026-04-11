import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/folders/$folderName/templates',
)({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: '/folders/$folderName/templates',
      params: { folderName: params.folderName },
    })
  },
})
