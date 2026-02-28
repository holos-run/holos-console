import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/projects/$projectName')({
  component: () => <div className="text-muted-foreground">Project detail placeholder</div>,
})
