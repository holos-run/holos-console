import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute(
  '/_authenticated/orgs/$orgName/folders/$folderName/',
)({
  beforeLoad: ({ params }) => {
    throw redirect({
      to: '/folders/$folderName',
      params: { folderName: params.folderName },
    })
  },
})
