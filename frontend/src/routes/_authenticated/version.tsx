import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/version')({
  component: () => <div className="text-muted-foreground">Version page placeholder</div>,
})
