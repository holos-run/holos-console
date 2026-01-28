import { useState, useEffect } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  Alert,
  CircularProgress,
  List,
  ListItemButton,
  ListItemText,
} from '@mui/material'
import { useAuth } from '../auth'
import { secretsClient } from '../client'
import type { SecretMetadata } from '../gen/holos/console/v1/secrets_pb'

export function SecretsListPage() {
  const { isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [secrets, setSecrets] = useState<SecretMetadata[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      login('/secrets')
    }
  }, [authLoading, isAuthenticated, login])

  // Fetch secrets list when authenticated
  useEffect(() => {
    if (!isAuthenticated) return

    const fetchSecrets = async () => {
      setIsLoading(true)
      setError(null)

      try {
        const token = getAccessToken()
        const response = await secretsClient.listSecrets(
          {},
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )

        setSecrets(response.secrets)
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    fetchSecrets()
  }, [isAuthenticated, getAccessToken])

  // Show loading while checking auth or fetching secrets
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
    return (
      <Card variant="outlined">
        <CardContent>
          <Alert severity="error">{error.message}</Alert>
        </CardContent>
      </Card>
    )
  }

  // Show secrets list
  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Secrets
        </Typography>
        {secrets.length === 0 ? (
          <Typography color="text.secondary">
            No secrets available. Secrets must have the label{' '}
            <code>app.kubernetes.io/managed-by=console.holos.run</code> to appear here.
          </Typography>
        ) : (
          <List>
            {secrets.map((secret) => (
              <ListItemButton
                key={secret.name}
                component={RouterLink}
                to={`/secrets/${secret.name}`}
              >
                <ListItemText primary={secret.name} />
              </ListItemButton>
            ))}
          </List>
        )}
      </CardContent>
    </Card>
  )
}
