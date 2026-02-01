import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  Alert,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  Link,
  Snackbar,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import { useAuth } from '../auth'
import { secretsClient } from '../client'
import { SecretDataEditor } from './SecretDataEditor'
import { SecretRawView } from './SecretRawView'
import { SharingPanel, type Grant } from './SharingPanel'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import type { ShareGrant } from '../gen/holos/console/v1/secrets_pb'

// Serialize data map to a stable JSON string for dirty checking
function serializeData(data: Record<string, Uint8Array>): string {
  const sorted = Object.keys(data).sort()
  const obj: Record<string, string> = {}
  const decoder = new TextDecoder()
  for (const key of sorted) {
    obj[key] = decoder.decode(data[key])
  }
  return JSON.stringify(obj)
}

export function SecretPage() {
  const { name } = useParams<{ name: string }>()
  const navigate = useNavigate()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const { user, isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [secretData, setSecretData] = useState<Record<string, Uint8Array>>({})
  const [originalData, setOriginalData] = useState<Record<string, Uint8Array>>({})
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [saveSuccess, setSaveSuccess] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Description and URL state
  const [description, setDescription] = useState('')
  const [originalDescription, setOriginalDescription] = useState('')
  const [url, setUrl] = useState('')
  const [originalUrl, setOriginalUrl] = useState('')

  // View mode state
  const [viewMode, setViewMode] = useState<'editor' | 'raw'>('editor')
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  // Sharing state
  const [userGrants, setUserGrants] = useState<ShareGrant[]>([])
  const [groupGrants, setGroupGrants] = useState<ShareGrant[]>([])
  const [isSavingSharing, setIsSavingSharing] = useState(false)

  const isDirty =
    serializeData(secretData) !== serializeData(originalData) ||
    description !== originalDescription ||
    url !== originalUrl

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

        const data = response.data as Record<string, Uint8Array>
        setSecretData(data)
        setOriginalData(data)
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
          setDescription(meta.description ?? '')
          setOriginalDescription(meta.description ?? '')
          setUrl(meta.url ?? '')
          setOriginalUrl(meta.url ?? '')
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

  const handleSaveSharing = async (newUserGrants: Grant[], newGroupGrants: Grant[]) => {
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

  const handleViewModeChange = async (_: React.MouseEvent<HTMLElement>, newMode: 'editor' | 'raw' | null) => {
    if (newMode === null) return
    setViewMode(newMode)

    if (newMode === 'raw' && rawJson === null && name) {
      try {
        const token = getAccessToken()
        const response = await secretsClient.getSecretRaw(
          { name },
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )
        setRawJson(response.raw)
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  const handleSave = async () => {
    if (!name || !isDirty) return
    setIsSaving(true)
    setSaveError(null)
    setSaveSuccess(false)

    try {
      const token = getAccessToken()
      await secretsClient.updateSecret(
        { name, data: secretData, description, url },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setOriginalData({ ...secretData })
      setOriginalDescription(description)
      setOriginalUrl(url)
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
          label="Description"
          fullWidth
          size="small"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What is this secret used for?"
          sx={{ mb: 1 }}
        />
        <TextField
          label="URL"
          fullWidth
          size="small"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://example.com/service"
          sx={{ mb: 1 }}
          slotProps={{
            input: {
              endAdornment: url ? (
                <Link href={url} target="_blank" rel="noopener noreferrer" sx={{ whiteSpace: 'nowrap' }}>
                  Open
                </Link>
              ) : undefined,
            },
          }}
        />
        <ToggleButtonGroup
          value={viewMode}
          exclusive
          onChange={handleViewModeChange}
          size="small"
          sx={{ mb: 2 }}
        >
          <ToggleButton value="editor">Editor</ToggleButton>
          <ToggleButton value="raw">Raw</ToggleButton>
        </ToggleButtonGroup>
        {viewMode === 'editor' && (
          <SecretDataEditor initialData={originalData} onChange={setSecretData} />
        )}
        {viewMode === 'raw' && rawJson && (
          <SecretRawView
            raw={rawJson}
            includeAllFields={includeAllFields}
            onToggleIncludeAllFields={() => setIncludeAllFields((prev) => !prev)}
          />
        )}
        {saveError && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {saveError}
          </Alert>
        )}
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mt: 2 }}>
          <Button
            variant="contained"
            onClick={handleSave}
            disabled={!isDirty || isSaving || viewMode === 'raw'}
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

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} fullScreen={isMobile}>
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
