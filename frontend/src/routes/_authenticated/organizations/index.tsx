import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/organizations/')({
  component: () => <div className="text-muted-foreground">Organizations page placeholder</div>,
})
