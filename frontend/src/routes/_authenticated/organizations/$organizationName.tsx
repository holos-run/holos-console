import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/organizations/$organizationName')({
  component: () => <div className="text-muted-foreground">Organization detail placeholder</div>,
})
