import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  TextField,
  Alert,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Snackbar,
  Stack,
} from '@mui/material'
import { useAuth } from '../auth'
import { secretsClient } from '../client'
import { SharingPanel } from './SharingPanel'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import type { ShareGrant } from '../gen/holos/console/v1/secrets_pb'

// Convert secret data map to env file format
function formatAsEnvFile(data: Record<string, Uint8Array>): string {
  return Object.entries(data)
    .map(([key, value]) => `${key}=${new TextDecoder().decode(value)}`)
    .join('\n')
}

// Parse env file format back to key-value map
function parseEnvFile(text: string): Record<string, Uint8Array> {
  const encoder = new TextEncoder()
  const result: Record<string, Uint8Array> = {}
  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (trimmed === '' || trimmed.startsWith('#')) continue
    const eqIndex = trimmed.indexOf('=')
    if (eqIndex === -1) continue
    const key = trimmed.slice(0, eqIndex)
    const value = trimmed.slice(eqIndex + 1)
    result[key] = encoder.encode(value)
  }
  return result
}

export function SecretPage() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const { user, isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [secretData, setSecretData] = useState<string>('')
  const [originalData, setOriginalData] = useState<string>('')
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Sharing state
  const [userGrants, setUserGrants] = useState<ShareGrant[]>([])
  const [groupGrants, setGroupGrants] = useState<ShareGrant[]>([])
  const [isSavingSharing, setIsSavingSharing] = useState(false)

  const isDirty = secretData !== originalData

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
        setOriginalData(envContent)
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    fetchSecret()
  }, [isAuthenticated, name, getAccessToken])

  // Fetch sharing metadata via listSecrets
  useEffect(() => {
    if (!isAuthenticated || !name) return

    const fetchMetadata = async () => {
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
        const meta = response.secrets.find((s) => s.name === name)
        if (meta) {
          setUserGrants(meta.userGrants)
          setGroupGrants(meta.groupGrants)
        }
      } catch {
        // Sharing metadata is non-critical; don't block page
      }
    }

    fetchMetadata()
  }, [isAuthenticated, name, getAccessToken])

  const userEmail = user?.profile?.email as string | undefined
  const isOwner =
    userEmail != null &&
    userGrants.some((g) => g.principal === userEmail && g.role === Role.OWNER)

  const handleSaveSharing = async (
    newUserGrants: { principal: string; role: Role }[],
    newGroupGrants: { principal: string; role: Role }[],
  ) => {
    if (!name) return
    setIsSavingSharing(true)
    try {
      const token = getAccessToken()
      const response = await secretsClient.updateSharing(
        {
          name,
          userGrants: newUserGrants,
          groupGrants: newGroupGrants,
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      if (response.metadata) {
        setUserGrants(response.metadata.userGrants)
        setGroupGrants(response.metadata.groupGrants)
      }
    } finally {
      setIsSavingSharing(false)
    }
  }

  const handleSave = async () => {
    if (!name || !isDirty) return
    setIsSaving(true)
    setSaveError(null)
    setSaveSuccess(false)

    try {
      const token = getAccessToken()
      const data = parseEnvFile(secretData)
      await secretsClient.updateSecret(
        { name, data },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setOriginalData(secretData)
      setSaveSuccess(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!name) return
    setIsDeleting(true)
    setDeleteError(null)

    try {
      const token = getAccessToken()
      await secretsClient.deleteSecret(
        { name },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setDeleteOpen(false)
      navigate('/secrets')
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsDeleting(false)
    }
  }

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
          onChange={(e) => setSecretData(e.target.value)}
          slotProps={{
            input: {
              sx: { fontFamily: 'monospace' },
            },
          }}
          minRows={4}
        />
        {saveError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {saveError}
          </Alert>
        )}
        <Stack direction="row" spacing={2} sx={{ mt: 2 }}>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={!isDirty || isSaving}
          >
            {isSaving ? 'Saving...' : 'Save'}
          </Button>
          <Button
            variant="outlined"
            color="error"
            onClick={() => setDeleteOpen(true)}
          >
            Delete
          </Button>
        </Stack>
        <SharingPanel
          userGrants={userGrants}
          groupGrants={groupGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={isSavingSharing}
        />
        <Snackbar
          open={saveSuccess}
          autoHideDuration={3000}
          onClose={() => setSaveSuccess(false)}
          message="Secret saved successfully"
        />
      </CardContent>

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)}>
        <DialogTitle>Delete Secret</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete secret &quot;{name}&quot;? This action cannot be undone.
          </DialogContentText>
          {deleteError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteOpen(false)}>Cancel</Button>
          <Button onClick={handleDelete} color="error" variant="contained" disabled={isDeleting}>
            {isDeleting ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  )
}
