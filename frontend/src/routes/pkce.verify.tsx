import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { getUserManager } from '@/lib/auth'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

export const Route = createFileRoute('/pkce/verify')({
  component: PKCEVerify,
})

function PKCEVerify() {
  const [error, setError] = useState<string | null>(null)
  const navigate = useNavigate()

  useEffect(() => {
    const handleCallback = async () => {
      try {
        const userManager = getUserManager()
        const user = await userManager.signinRedirectCallback()
        const returnTo =
          (user.state as { returnTo?: string } | undefined)?.returnTo ?? '/'
        navigate({ to: returnTo })
      } catch (err) {
        console.error('PKCE verify error:', err)
        setError(err instanceof Error ? err.message : String(err))
      }
    }
    handleCallback()
  }, [navigate])

  if (error) {
    return (
      <div className="flex min-h-screen items-center justify-center p-4">
        <Alert variant="destructive" className="max-w-lg">
          <AlertTitle>Authentication Error</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-2">
      <div className="h-8 w-8 animate-spin rounded-full border-4 border-primary border-t-transparent" />
      <p className="text-muted-foreground">Completing authentication...</p>
    </div>
  )
}
