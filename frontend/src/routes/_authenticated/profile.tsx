import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_authenticated/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  return (
    <div className="p-4">
      <h1 className="text-2xl font-bold">Profile</h1>
      <p className="text-muted-foreground">Profile page placeholder</p>
    </div>
  )
}
