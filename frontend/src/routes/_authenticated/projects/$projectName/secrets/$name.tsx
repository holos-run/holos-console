import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/$name')({
  component: () => <div className="text-muted-foreground">Secret detail placeholder</div>,
})
