import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Box, CircularProgress, Typography, Alert } from '@mui/material'
import { getUserManager } from './userManager'

/**
 * Callback component that handles the OIDC redirect after authentication.
 *
 * This component:
 * 1. Processes the authorization code from the URL
 * 2. Exchanges it for tokens via the token endpoint
 * 3. Redirects to the original page (from state.returnTo) or home on success
 */
export function Callback() {
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const handleCallback = async () => {
      try {
        const userManager = getUserManager()
        const user = await userManager.signinRedirectCallback()
        // Navigate to returnTo from state, or default to home
        const returnTo =
          (user.state as { returnTo?: string } | undefined)?.returnTo ?? '/'
        navigate(returnTo, { replace: true })
      } catch (err) {
        console.error('Callback error:', err)
        setError(err instanceof Error ? err.message : String(err))
      }
    }

    handleCallback()
  }, [navigate])

  if (error) {
    return (
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          minHeight: '100vh',
          gap: 2,
          p: 4,
        }}
      >
        <Alert severity="error" sx={{ maxWidth: 500 }}>
          <Typography variant="subtitle1" gutterBottom>
            Authentication Error
          </Typography>
          <Typography variant="body2">{error}</Typography>
        </Alert>
      </Box>
    )
  }

  return (
    <Box
      sx={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        gap: 2,
      }}
    >
      <CircularProgress />
      <Typography variant="body1" color="text.secondary">
        Completing authentication...
      </Typography>
    </Box>
  )
}
