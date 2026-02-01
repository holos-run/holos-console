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
  Snackbar,
  Stack,
  TextField,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import EditIcon from '@mui/icons-material/Edit'
import CheckIcon from '@mui/icons-material/Check'
import CloseIcon from '@mui/icons-material/Close'
import { useAuth } from '../auth'
import { organizationsClient } from '../client'
import { useOrg } from '../OrgProvider'
import { SharingPanel, type Grant } from './SharingPanel'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import type { Organization } from '../gen/holos/console/v1/organizations_pb'

export function OrganizationPage() {
  const { organizationName: name } = useParams<{ organizationName: string }>()
  const navigate = useNavigate()
  const { setSelectedOrg } = useOrg()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const { isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [organization, setOrganization] = useState<Organization | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Inline edit state for display name
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Inline edit state for description
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Save state
  const [isSaving, setIsSaving] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)

  // Sharing state
  const [isSavingSharing, setIsSavingSharing] = useState(false)

  // Delete state
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      login(`/organizations/${name}`)
    }
  }, [authLoading, isAuthenticated, login, name])

  // Fetch organization data
  useEffect(() => {
    if (!isAuthenticated || !name) return

    const fetchOrganization = async () => {
      setIsLoading(true)
      setError(null)

      try {
        const token = getAccessToken()
        const response = await organizationsClient.getOrganization(
          { name },
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )

        if (response.organization) {
          setOrganization(response.organization)
        }
      } catch (err) {
        setError(err instanceof Error ? err : new Error(String(err)))
      } finally {
        setIsLoading(false)
      }
    }

    fetchOrganization()
  }, [isAuthenticated, name, getAccessToken])

  const isOwner = organization?.userRole === Role.OWNER
  const isEditorOrAbove = organization != null && organization.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    if (!name) return
    setIsSaving(true)
    try {
      const token = getAccessToken()
      await organizationsClient.updateOrganization(
        { name, displayName: newDisplayName },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setOrganization((prev) => prev ? { ...prev, displayName: newDisplayName } : prev)
      setEditingDisplayName(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    } finally {
      setIsSaving(false)
    }
  }

  const handleSaveDescription = async (newDescription: string) => {
    if (!name) return
    setIsSaving(true)
    try {
      const token = getAccessToken()
      await organizationsClient.updateOrganization(
        { name, description: newDescription },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setOrganization((prev) => prev ? { ...prev, description: newDescription } : prev)
      setEditingDescription(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    } finally {
      setIsSaving(false)
    }
  }

  const handleSaveSharing = async (newUserGrants: Grant[], newGroupGrants: Grant[]) => {
    if (!name) return
    setIsSavingSharing(true)
    try {
      const token = getAccessToken()
      const response = await organizationsClient.updateOrganizationSharing(
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
      if (response.organization) {
        setOrganization(response.organization)
      }
    } finally {
      setIsSavingSharing(false)
    }
  }

  const handleDelete = async () => {
    if (!name) return
    setIsDeleting(true)
    setDeleteError(null)

    try {
      const token = getAccessToken()
      await organizationsClient.deleteOrganization(
        { name },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setDeleteOpen(false)
      navigate('/organizations')
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsDeleting(false)
    }
  }

  // Show loading while checking auth or fetching organization
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
      displayMessage = `Organization "${name}" not found`
    } else if (
      errorMessage.includes('permission') ||
      errorMessage.includes('denied') ||
      (error as Error & { code?: string }).code === 'permission_denied'
    ) {
      displayMessage = 'Permission denied: You are not authorized to view this organization'
    }

    return (
      <Card variant="outlined">
        <CardContent>
          <Alert severity="error">{displayMessage}</Alert>
        </CardContent>
      </Card>
    )
  }

  if (!organization) return null

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="subtitle2" color="text.secondary" gutterBottom>
          {organization.name}
        </Typography>

        {/* Display Name */}
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 1 }}>
          {editingDisplayName ? (
            <>
              <TextField
                label="Display Name"
                fullWidth
                size="small"
                autoFocus
                value={draftDisplayName}
                onChange={(e) => setDraftDisplayName(e.target.value)}
                placeholder="Display name"
                disabled={isSaving}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    handleSaveDisplayName(draftDisplayName)
                  }
                }}
              />
              <IconButton
                aria-label="save display name"
                size="small"
                onClick={() => handleSaveDisplayName(draftDisplayName)}
                disabled={isSaving}
              >
                <CheckIcon fontSize="small" />
              </IconButton>
              <IconButton
                aria-label="cancel editing display name"
                size="small"
                onClick={() => setEditingDisplayName(false)}
              >
                <CloseIcon fontSize="small" />
              </IconButton>
            </>
          ) : (
            <>
              <Typography
                variant="h6"
                sx={{ flexGrow: 1 }}
              >
                {organization.displayName || organization.name}
              </Typography>
              {isEditorOrAbove && (
                <IconButton
                  aria-label="edit display name"
                  size="small"
                  onClick={() => {
                    setDraftDisplayName(organization.displayName || '')
                    setEditingDisplayName(true)
                  }}
                >
                  <EditIcon fontSize="small" />
                </IconButton>
              )}
            </>
          )}
        </Stack>

        {/* Description */}
        <Stack direction="row" alignItems="center" spacing={1} sx={{ mb: 2 }}>
          {editingDescription ? (
            <>
              <TextField
                label="Description"
                fullWidth
                size="small"
                autoFocus
                value={draftDescription}
                onChange={(e) => setDraftDescription(e.target.value)}
                placeholder="What is this organization for?"
                disabled={isSaving}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    handleSaveDescription(draftDescription)
                  }
                }}
              />
              <IconButton
                aria-label="save description"
                size="small"
                onClick={() => handleSaveDescription(draftDescription)}
                disabled={isSaving}
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
                color={organization.description ? 'text.primary' : 'text.secondary'}
                sx={{ flexGrow: 1 }}
              >
                {organization.description || 'No description'}
              </Typography>
              {isEditorOrAbove && (
                <IconButton
                  aria-label="edit description"
                  size="small"
                  onClick={() => {
                    setDraftDescription(organization.description || '')
                    setEditingDescription(true)
                  }}
                >
                  <EditIcon fontSize="small" />
                </IconButton>
              )}
            </>
          )}
        </Stack>

        {/* Actions */}
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mb: 2 }}>
          <Button
            variant="contained"
            onClick={() => {
              setSelectedOrg(name || null)
              navigate('/projects')
            }}
          >
            Projects
          </Button>
          {isOwner && (
            <Button
              variant="outlined"
              color="error"
              onClick={() => setDeleteOpen(true)}
            >
              Delete
            </Button>
          )}
        </Stack>

        {/* Sharing */}
        <SharingPanel
          userGrants={organization.userGrants}
          groupGrants={organization.groupGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={isSavingSharing}
        />

        <Snackbar
          open={saveSuccess}
          autoHideDuration={3000}
          onClose={() => setSaveSuccess(false)}
          message="Organization updated successfully"
        />
      </CardContent>

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} fullScreen={isMobile}>
        <DialogTitle>Delete Organization</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete organization &quot;{name}&quot;? This action cannot be undone.
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
