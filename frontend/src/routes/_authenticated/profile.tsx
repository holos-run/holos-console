import { useState, useEffect } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import { useAuth } from '@/lib/auth'

export const Route = createFileRoute('/_authenticated/profile')({
  component: ProfilePage,
})

function ProfilePage() {
  const {
    user,
    isAuthenticated,
    isLoading,
    refreshTokens,
    lastRefreshStatus,
    lastRefreshTime,
    lastRefreshError,
    login,
  } = useAuth()

  const [timeRemaining, setTimeRemaining] = useState<number | null>(null)
  const [isRefreshing, setIsRefreshing] = useState(false)

  useEffect(() => {
    if (!user?.expires_at) {
      setTimeRemaining(null)
      return
    }

    const updateTimeRemaining = () => {
      const now = Math.floor(Date.now() / 1000)
      const remaining = user.expires_at! - now
      setTimeRemaining(Math.max(0, remaining))
    }

    updateTimeRemaining()
    const interval = setInterval(updateTimeRemaining, 1000)
    return () => clearInterval(interval)
  }, [user?.expires_at])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await refreshTokens()
    } catch (err) {
      console.error('Manual refresh failed:', err)
    } finally {
      setIsRefreshing(false)
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="pt-6">
          <span className="text-sm">Loading...</span>
        </CardContent>
      </Card>
    )
  }

  if (!isAuthenticated) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-4">
          <h2 className="text-lg font-semibold">Profile</h2>
          <p className="text-muted-foreground">Sign in to view token information.</p>
          <Button onClick={() => login('/profile')}>Sign In</Button>
        </CardContent>
      </Card>
    )
  }

  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60)
    const secs = seconds % 60
    return `${mins}:${secs.toString().padStart(2, '0')}`
  }

  const totalLifetime = user?.expires_in ?? 900
  const progress = timeRemaining !== null
    ? Math.min(100, ((totalLifetime - timeRemaining) / totalLifetime) * 100)
    : 0

  return (
    <div className="space-y-4">
      <Card>
        <CardHeader>
          <CardTitle>ID Token Status</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <div className="flex justify-between mb-1">
              <span className="text-sm text-muted-foreground">Time Remaining</span>
              <span className="text-sm font-bold">
                {timeRemaining !== null ? formatTime(timeRemaining) : 'N/A'}
              </span>
            </div>
            <div className="w-full bg-muted rounded-full h-2">
              <div
                className={`h-2 rounded-full transition-all ${
                  timeRemaining !== null && timeRemaining < 60 ? 'bg-yellow-500' : 'bg-primary'
                }`}
                style={{ width: `${progress}%` }}
              />
            </div>
          </div>

          <div className="flex flex-wrap gap-2">
            <Badge variant={user?.expired ? 'destructive' : 'default'}>
              {user?.expired ? 'Expired' : 'Valid'}
            </Badge>
            <Badge variant="outline">
              Expires: {new Date((user?.expires_at ?? 0) * 1000).toLocaleTimeString()}
            </Badge>
          </div>

          <Button variant="outline" onClick={handleRefresh} disabled={isRefreshing}>
            {isRefreshing ? 'Refreshing...' : 'Refresh Now'}
          </Button>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Last Refresh Status</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge
              variant={
                lastRefreshStatus === 'success'
                  ? 'default'
                  : lastRefreshStatus === 'error'
                  ? 'destructive'
                  : 'outline'
              }
            >
              {lastRefreshStatus}
            </Badge>
            {lastRefreshTime && (
              <span className="text-sm text-muted-foreground">
                {lastRefreshTime.toLocaleTimeString()}
              </span>
            )}
          </div>

          {lastRefreshError && (
            <Alert variant="destructive">
              <AlertDescription>{lastRefreshError.message}</AlertDescription>
            </Alert>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Token Details</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <Separator />
          <div className="space-y-3">
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Subject (sub)</p>
              <p className="font-mono">{user?.profile?.sub ?? 'N/A'}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Email</p>
              <p>{(user?.profile?.email as string) ?? 'N/A'}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Scopes</p>
              <p className="font-mono">{user?.scope ?? 'N/A'}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Token Type</p>
              <p className="font-mono">{user?.token_type ?? 'N/A'}</p>
            </div>
            <div>
              <p className="text-xs uppercase tracking-wider text-muted-foreground">Roles</p>
              <p className="font-mono">
                {(user?.profile?.groups as string[] | undefined)?.length
                  ? (user?.profile?.groups as string[]).join(', ')
                  : 'None'}
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
