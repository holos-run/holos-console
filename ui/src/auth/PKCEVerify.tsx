import { useEffect, useState } from 'react'
import { Box, CircularProgress, Typography, Alert, ThemeProvider, createTheme, CssBaseline } from '@mui/material'
import { getUserManager } from './userManager'

const theme = createTheme({ palette: { mode: 'light' } })

/**
 * PKCEVerify handles the OIDC PKCE callback at /pkce/verify.
 *
 * This component renders outside React Router (which uses basename="/ui")
 * because /pkce/verify is not under the /ui prefix. It uses window.location
 * for navigation instead of useNavigate.
 */
export function PKCEVerify() {
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const handleCallback = async () => {
      try {
        const userManager = getUserManager()
        const user = await userManager.signinRedirectCallback()
        const returnTo =
          (user.state as { returnTo?: string } | undefined)?.returnTo ?? '/'
        // Navigate using window.location since we're outside React Router
        window.location.replace('/ui' + returnTo)
      } catch (err) {
        console.error('PKCE verify error:', err)
        setError(err instanceof Error ? err.message : String(err))
      }
    }

    handleCallback()
  }, [])

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      {error ? (
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
      ) : (
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
      )}
    </ThemeProvider>
  )
}
