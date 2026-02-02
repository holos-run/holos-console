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
  ToggleButton,
  ToggleButtonGroup,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import EditIcon from '@mui/icons-material/Edit'
import CheckIcon from '@mui/icons-material/Check'
import CloseIcon from '@mui/icons-material/Close'
import { useAuth } from '../auth'
import { organizationsClient } from '../client'
import { useOrg } from '../OrgProvider'
import {
  useGetOrganization,
  useDeleteOrganization,
  useUpdateOrganization,
  useUpdateOrganizationSharing,
} from '../queries/organizations'
import { SharingPanel, type Grant } from './SharingPanel'
import { RawView } from './RawView'
import { Role } from '../gen/holos/console/v1/rbac_pb'

export function OrganizationPage() {
  const { organizationName: name } = useParams<{ organizationName: string }>()
  const navigate = useNavigate()
  const { setSelectedOrg } = useOrg()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const { isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const { data, isLoading, error } = useGetOrganization(name ?? '')
  const organization = data?.organization ?? null

  const deleteOrganization = useDeleteOrganization()
  const updateOrganization = useUpdateOrganization()
  const updateOrganizationSharing = useUpdateOrganizationSharing()

  // Inline edit state for display name
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Inline edit state for description
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Save state
  const [saveSuccess, setSaveSuccess] = useState(false)

  // View mode state
  const [viewMode, setViewMode] = useState<'editor' | 'raw'>('editor')
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  // Delete state
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      login(`/organizations/${name}`)
    }
  }, [authLoading, isAuthenticated, login, name])

  const isOwner = organization?.userRole === Role.OWNER
  const isEditorOrAbove = organization != null && organization.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    if (!name) return
    try {
      await updateOrganization.mutateAsync({ name, displayName: newDisplayName })
      setEditingDisplayName(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    }
  }

  const handleSaveDescription = async (newDescription: string) => {
    if (!name) return
    try {
      await updateOrganization.mutateAsync({ name, description: newDescription })
      setEditingDescription(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    }
  }

  const handleSaveSharing = async (newUserGrants: Grant[], newGroupGrants: Grant[]) => {
    if (!name) return
    await updateOrganizationSharing.mutateAsync({
      name,
      userGrants: newUserGrants,
      groupGrants: newGroupGrants,
    })
  }

  const handleViewModeChange = async (_: React.MouseEvent<HTMLElement>, newMode: 'editor' | 'raw' | null) => {
    if (newMode === null) return
    setViewMode(newMode)

    if (newMode === 'raw' && rawJson === null && name) {
      try {
        const token = getAccessToken()
        const response = await organizationsClient.getOrganizationRaw(
          { name },
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )
        setRawJson(response.raw)
      } catch (err) {
        // handled by error state
        void err
      }
    }
  }

  const handleDelete = async () => {
    if (!name) return
    setDeleteError(null)

    try {
      await deleteOrganization.mutateAsync({ name })
      setDeleteOpen(false)
      navigate('/organizations')
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
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

    if (errorMessage.includes('not found') || errorMessage.includes('not_found')) {
      displayMessage = `Organization "${name}" not found`
    } else if (
      errorMessage.includes('permission') ||
      errorMessage.includes('denied')
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
                disabled={updateOrganization.isPending}
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
                disabled={updateOrganization.isPending}
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
                disabled={updateOrganization.isPending}
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
                disabled={updateOrganization.isPending}
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
        {viewMode === 'raw' && rawJson && (
          <RawView
            raw={rawJson}
            includeAllFields={includeAllFields}
            onToggleIncludeAllFields={() => setIncludeAllFields((prev) => !prev)}
          />
        )}
        {viewMode === 'editor' && (
          <>
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
          </>
        )}

        {/* Sharing */}
        <SharingPanel
          userGrants={organization.userGrants}
          groupGrants={organization.groupGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateOrganizationSharing.isPending}
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
          <Button onClick={handleDelete} color="error" variant="contained" disabled={deleteOrganization.isPending}>
            {deleteOrganization.isPending ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  )
}
