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
  IconButton,
  Link,
  Snackbar,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import EditIcon from '@mui/icons-material/Edit'
import CheckIcon from '@mui/icons-material/Check'
import CloseIcon from '@mui/icons-material/Close'
import LinkIcon from '@mui/icons-material/Link'
import { useAuth } from '../auth'
import { secretsClient } from '../client'
import { SecretDataViewer } from './SecretDataViewer'
import { RawView } from './RawView'
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
  const { name, projectName } = useParams<{ name: string; projectName: string }>()
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

  // Inline edit state
  const [editingDescription, setEditingDescription] = useState(false)
  const [editingUrl, setEditingUrl] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')
  const [draftUrl, setDraftUrl] = useState('')

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
      login(`/projects/${projectName}/secrets/${name}`)
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
          { name, project: projectName || '' },
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
          { project: projectName || '' },
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
          project: projectName || '',
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
          { name, project: projectName || '' },
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
        { name, project: projectName || '', data: secretData, description, url },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setOriginalData({ ...secretData })
      setOriginalDescription(description)
      setOriginalUrl(url)
      setRawJson(null)
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
        { name, project: projectName || '' },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setDeleteOpen(false)
      navigate(`/projects/${projectName}/secrets`)
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
        <Typography variant="subtitle2" color="text.secondary" gutterBottom>
          {projectName} / Secrets
        </Typography>
        <Typography variant="h6" gutterBottom>
          {name}
        </Typography>
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
          {editingDescription ? (
            <>
              <TextField
                label="Description"
                fullWidth
                size="small"
                autoFocus
                value={draftDescription}
                onChange={(e) => setDraftDescription(e.target.value)}
                placeholder="What is this secret used for?"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    setDescription(draftDescription)
                    setEditingDescription(false)
                  }
                }}
              />
              <IconButton
                aria-label="save description"
                size="small"
                onClick={() => {
                  setDescription(draftDescription)
                  setEditingDescription(false)
                }}
              >
                <CheckIcon fontSize="small" />
              </IconButton>
              <IconButton
                aria-label="cancel editing description"
                size="small"
                onClick={() => setEditingDescription(false)}
              >
                <CloseIcon fontSize="small" />
              </IconButton>
            </>
          ) : (
            <>
              <Typography
                variant="body2"
                color={description ? 'text.primary' : 'text.secondary'}
                sx={{ flexGrow: 1 }}
              >
                {description || 'No description'}
              </Typography>
              <IconButton
                aria-label="edit description"
                size="small"
                onClick={() => {
                  setDraftDescription(description)
                  setEditingDescription(true)
                }}
              >
                <EditIcon fontSize="small" />
              </IconButton>
            </>
          )}
        </Stack>
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
          {editingUrl ? (
            <>
              <TextField
                label="URL"
                fullWidth
                size="small"
                autoFocus
                value={draftUrl}
                onChange={(e) => setDraftUrl(e.target.value)}
                placeholder="https://example.com/service"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    setUrl(draftUrl)
                    setEditingUrl(false)
                  }
                }}
              />
              <IconButton
                aria-label="save url"
                size="small"
                onClick={() => {
                  setUrl(draftUrl)
                  setEditingUrl(false)
                }}
              >
                <CheckIcon fontSize="small" />
              </IconButton>
              <IconButton
                aria-label="cancel editing url"
                size="small"
                onClick={() => setEditingUrl(false)}
              >
                <CloseIcon fontSize="small" />
              </IconButton>
            </>
          ) : (
            <>
              {url ? (
                <>
                  <LinkIcon fontSize="small" color="action" />
                  <Link
                    href={url}
                    target="_blank"
                    rel="noopener noreferrer"
                    variant="body2"
                    sx={{ flexGrow: 1 }}
                  >
                    {url}
                  </Link>
                </>
              ) : (
                <Typography
                  variant="body2"
                  color="text.secondary"
                  sx={{ flexGrow: 1 }}
                >
                  No URL
                </Typography>
              )}
              <IconButton
                aria-label="edit url"
                size="small"
                onClick={() => {
                  setDraftUrl(url)
                  setEditingUrl(true)
                }}
              >
                <EditIcon fontSize="small" />
              </IconButton>
            </>
          )}
        </Stack>
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
          <SecretDataViewer data={secretData} onChange={setSecretData} />
        )}
        {viewMode === 'raw' && rawJson && (
          <RawView
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
