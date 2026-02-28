import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/projects/$projectName/secrets/')({
  component: () => <div className="text-muted-foreground">Secrets list placeholder</div>,
})
