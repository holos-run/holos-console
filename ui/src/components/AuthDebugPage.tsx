import { useState, useEffect } from 'react'
import {
  Card,
  CardContent,
  Typography,
  Button,
  Box,
  LinearProgress,
  Alert,
  Chip,
  Stack,
  Divider,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import { useLocation } from 'react-router-dom'
import { useAuth } from '../auth'

export function AuthDebugPage() {
  const location = useLocation()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const {
    user,
    bffUser,
    isBFF,
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

  // Calculate time remaining on ID token
  useEffect(() => {
    if (isBFF || !user?.expires_at) {
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
  }, [isBFF, user?.expires_at])

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
      <Card variant="outlined">
        <CardContent>
          <Typography variant="body2">Loading...</Typography>
        </CardContent>
      </Card>
    )
  }

  // BFF Mode UI
  if (isBFF) {
    return (
      <Stack spacing={3}>
        <Card variant="outlined">
          <CardContent>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}>
              <Typography variant="h6">Profile</Typography>
              <Chip label="BFF Mode" color="info" size="small" />
            </Stack>

            <Alert severity="info" sx={{ mb: 2 }}>
              Authentication is handled by oauth2-proxy. Tokens are managed
              server-side and are not accessible to the frontend.
            </Alert>

            {isAuthenticated && bffUser ? (
              <Stack spacing={1.5}>
                <Box>
                  <Typography variant="overline" display="block">
                    User
                  </Typography>
                  <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                    {bffUser.user}
                  </Typography>
                </Box>
                <Box>
                  <Typography variant="overline" display="block">
                    Email
                  </Typography>
                  <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                    {bffUser.email}
                  </Typography>
                </Box>
                <Box>
                  <Typography variant="overline" display="block">
                    Roles
                  </Typography>
                  <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                    {bffUser.roles?.length ? bffUser.roles.join(', ') : 'None'}
                  </Typography>
                </Box>
              </Stack>
            ) : (
              <Button variant="contained" onClick={() => login(location.pathname)}>
                Sign In
              </Button>
            )}
          </CardContent>
        </Card>
      </Stack>
    )
  }

  // Not Authenticated
  if (!isAuthenticated) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2 }}>Profile</Typography>
          <Typography color="text.secondary" paragraph>
            Sign in to view token information.
          </Typography>
          <Button variant="contained" onClick={() => login(location.pathname)}>
            Sign In
          </Button>
        </CardContent>
      </Card>
    )
  }

  // Authenticated
  const formatTime = (seconds: number) => {
    const mins = Math.floor(seconds / 60)
    const secs = seconds % 60
    return `${mins}:${secs.toString().padStart(2, '0')}`
  }

  // Calculate progress based on initial TTL (estimate from expires_in or default 15 min)
  const totalLifetime = user?.expires_in ?? 900
  const progress = timeRemaining !== null
    ? Math.min(100, ((totalLifetime - timeRemaining) / totalLifetime) * 100)
    : 0

  return (
    <Stack spacing={3}>
      <Card variant="outlined">
        <CardContent>
          <Typography variant="h6" sx={{ mb: 2 }}>ID Token Status</Typography>

          <Box sx={{ mb: 3 }}>
            <Box sx={{ display: 'flex', justifyContent: 'space-between', mb: 1 }}>
              <Typography variant="body2" color="text.secondary">
                Time Remaining
              </Typography>
              <Typography variant="body2" fontWeight="bold">
                {timeRemaining !== null ? formatTime(timeRemaining) : 'N/A'}
              </Typography>
            </Box>
            <LinearProgress
              variant="determinate"
              value={progress}
              color={timeRemaining !== null && timeRemaining < 60 ? 'warning' : 'primary'}
            />
          </Box>

          <Stack direction={isMobile ? 'column' : 'row'} spacing={1} sx={{ mb: 2 }}>
            <Chip
              label={user?.expired ? 'Expired' : 'Valid'}
              color={user?.expired ? 'error' : 'success'}
              size="small"
            />
            <Chip
              label={`Expires: ${new Date((user?.expires_at ?? 0) * 1000).toLocaleTimeString()}`}
              size="small"
              variant="outlined"
            />
          </Stack>

          <Button
            variant="outlined"
            onClick={handleRefresh}
            disabled={isRefreshing}
          >
            {isRefreshing ? 'Refreshing...' : 'Refresh Now'}
          </Button>
        </CardContent>
      </Card>

      <Card variant="outlined">
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Last Refresh Status
          </Typography>

          <Stack direction={isMobile ? 'column' : 'row'} spacing={1} alignItems={isMobile ? 'flex-start' : 'center'} sx={{ mb: 2 }}>
            <Chip
              label={lastRefreshStatus}
              color={
                lastRefreshStatus === 'success'
                  ? 'success'
                  : lastRefreshStatus === 'error'
                  ? 'error'
                  : 'default'
              }
              size="small"
            />
            {lastRefreshTime && (
              <Typography variant="body2" color="text.secondary">
                {lastRefreshTime.toLocaleTimeString()}
              </Typography>
            )}
          </Stack>

          {lastRefreshError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {lastRefreshError.message}
            </Alert>
          )}
        </CardContent>
      </Card>

      <Card variant="outlined">
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Token Details
          </Typography>
          <Divider sx={{ my: 2 }} />

          <Stack spacing={1.5}>
            <Box>
              <Typography variant="overline" display="block">
                Subject (sub)
              </Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                {user?.profile?.sub ?? 'N/A'}
              </Typography>
            </Box>

            <Box>
              <Typography variant="overline" display="block">
                Email
              </Typography>
              <Typography variant="body1">
                {user?.profile?.email ?? 'N/A'}
              </Typography>
            </Box>

            <Box>
              <Typography variant="overline" display="block">
                Scopes
              </Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                {user?.scope ?? 'N/A'}
              </Typography>
            </Box>

            <Box>
              <Typography variant="overline" display="block">
                Token Type
              </Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                {user?.token_type ?? 'N/A'}
              </Typography>
            </Box>

            <Box>
              <Typography variant="overline" display="block">
                Roles
              </Typography>
              <Typography variant="body1" sx={{ fontFamily: 'monospace' }}>
                {(user?.profile?.groups as string[] | undefined)?.length
                  ? (user?.profile?.groups as string[]).join(', ')
                  : 'None'}
              </Typography>
            </Box>
          </Stack>
        </CardContent>
      </Card>
    </Stack>
  )
}
