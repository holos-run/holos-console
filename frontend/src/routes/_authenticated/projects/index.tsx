import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/projects/')({
  component: () => <div className="text-muted-foreground">Projects page placeholder</div>,
})
