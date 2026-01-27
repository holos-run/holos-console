import { useState, useEffect } from 'react'
import { useParams } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  TextField,
  Alert,
  CircularProgress,
} from '@mui/material'
import { useAuth } from '../auth'
import { secretsClient } from '../client'

// Convert secret data map to env file format
function formatAsEnvFile(data: Record<string, Uint8Array>): string {
  return Object.entries(data)
    .map(([key, value]) => `${key}=${new TextDecoder().decode(value)}`)
    .join('\n')
}

export function SecretPage() {
  const { name } = useParams<{ name: string }>()
  const { isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [secretData, setSecretData] = useState<string>('')
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      login(`/secrets/${name}`)
    }
  }, [authLoading, isAuthenticated, login, name])

  // Fetch secret data when authenticated
  useEffect(() => {
    if (!isAuthenticated || !name) return

    const fetchSecret = async () => {
      setIsLoading(true)
      setError(null)

      try {
        const token = getAccessToken()
        const response = await secretsClient.getSecret(
          { name },
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )

        // Convert response data to env format
        const envContent = formatAsEnvFile(response.data as Record<string, Uint8Array>)
        setSecretData(envContent)
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    fetchSecret()
  }, [isAuthenticated, name, getAccessToken])

  // Show loading while checking auth or fetching secret
  if (authLoading || (isAuthenticated && isLoading)) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Box sx={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <CircularProgress size={24} />
            <Typography>Loading...</Typography>
          </Box>
        </CardContent>
      </Card>
    )
  }

  // Show error state
  if (error) {
    const errorMessage = error.message.toLowerCase()
    let displayMessage = error.message

    if (errorMessage.includes('not found') || (error as Error & { code?: string }).code === 'not_found') {
      displayMessage = `Secret "${name}" not found`
    } else if (
      errorMessage.includes('permission') ||
      errorMessage.includes('denied') ||
      (error as Error & { code?: string }).code === 'permission_denied'
    ) {
      displayMessage = 'Permission denied: You are not authorized to view this secret'
    }

    return (
      <Card variant="outlined">
        <CardContent>
          <Alert severity="error">{displayMessage}</Alert>
        </CardContent>
      </Card>
    )
  }

  // Show secret data
  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Secret: {name}
        </Typography>
        <TextField
          multiline
          fullWidth
          value={secretData}
          slotProps={{
            input: {
              readOnly: true,
              sx: { fontFamily: 'monospace' },
            },
          }}
          minRows={4}
        />
      </CardContent>
    </Card>
  )
}
