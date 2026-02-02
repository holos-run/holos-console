import { useState, useMemo } from 'react'
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
import { createClient } from '@connectrpc/connect'
import { useTransport } from '@connectrpc/connect-query'
import { useAuth } from '../auth'
import { useOrg } from '../OrgProvider'
import { SharingPanel, type Grant } from './SharingPanel'
import { RawView } from './RawView'
import { useGetOrganization, useDeleteOrganization, useUpdateOrganization, useUpdateOrganizationSharing } from '../queries/organizations'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import { OrganizationService } from '../gen/holos/console/v1/organizations_pb.js'

export function OrganizationPage() {
  const { organizationName: name } = useParams<{ organizationName: string }>()
  const navigate = useNavigate()
  const { setSelectedOrg } = useOrg()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const { isAuthenticated, isLoading: authLoading, login } = useAuth()
  const transport = useTransport()

  const { data, isLoading, error } = useGetOrganization(name ?? '')
  const organization = data?.organization

  const deleteOrganizationMutation = useDeleteOrganization()
  const updateOrganizationMutation = useUpdateOrganization()
  const updateSharingMutation = useUpdateOrganizationSharing()

  const organizationsClient = useMemo(() => createClient(OrganizationService, transport), [transport])

  // Inline edit state for display name
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Inline edit state for description
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Save success snackbar
  const [saveSuccess, setSaveSuccess] = useState(false)

  // View mode state
  const [viewMode, setViewMode] = useState<'editor' | 'raw'>('editor')
  const [rawJson, setRawJson] = useState<string | null>(null)
  const [rawError, setRawError] = useState<Error | null>(null)
  const [includeAllFields, setIncludeAllFields] = useState(false)

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Local overrides for optimistic display after update
  const [localDisplayName, setLocalDisplayName] = useState<string | null>(null)
  const [localDescription, setLocalDescription] = useState<string | null>(null)
  const [localOrganization, setLocalOrganization] = useState<typeof organization | null>(null)

  // Redirect to login if not authenticated
  if (!authLoading && !isAuthenticated) {
    login(`/organizations/${name}`)
  }

  const effectiveOrg = localOrganization ?? organization
  const displayName = localDisplayName ?? effectiveOrg?.displayName
  const description = localDescription ?? effectiveOrg?.description

  const isOwner = effectiveOrg?.userRole === Role.OWNER
  const isEditorOrAbove = effectiveOrg != null && effectiveOrg.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    if (!name) return
    try {
      await updateOrganizationMutation.mutateAsync({ name, displayName: newDisplayName })
      setLocalDisplayName(newDisplayName)
      setEditingDisplayName(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    }
  }

  const handleSaveDescription = async (newDescription: string) => {
    if (!name) return
    try {
      await updateOrganizationMutation.mutateAsync({ name, description: newDescription })
      setLocalDescription(newDescription)
      setEditingDescription(false)
      setSaveSuccess(true)
    } catch {
      // Keep editing state on failure
    }
  }

  const handleSaveSharing = async (newUserGrants: Grant[], newGroupGrants: Grant[]) => {
    if (!name) return
    try {
      const response = await updateSharingMutation.mutateAsync({
        name,
        userGrants: newUserGrants,
        groupGrants: newGroupGrants,
      })
      if (response.organization) {
        setLocalOrganization(response.organization)
      }
    } catch {
      // Error handling could be added here
    }
  }

  const handleViewModeChange = async (_: React.MouseEvent<HTMLElement>, newMode: 'editor' | 'raw' | null) => {
    if (newMode === null) return
    setViewMode(newMode)

    if (newMode === 'raw' && rawJson === null && name) {
      try {
        const response = await organizationsClient.getOrganizationRaw({ name })
        setRawJson(response.raw)
      } catch (err) {
        setRawError(err instanceof Error ? err : new Error(String(err)))
      }
    }
  }

  const handleDelete = async () => {
    if (!name) return

    try {
      await deleteOrganizationMutation.mutateAsync({ name })
      setDeleteOpen(false)
      navigate('/organizations')
    } catch {
      // Error is available via deleteOrganizationMutation.error
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
  const displayError = error || rawError
  if (displayError) {
    const errorMessage = displayError.message.toLowerCase()
    let displayMessage = displayError.message

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

  if (!effectiveOrg) return null

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="subtitle2" color="text.secondary" gutterBottom>
          {effectiveOrg.name}
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
                disabled={updateOrganizationMutation.isPending}
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
                disabled={updateOrganizationMutation.isPending}
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
                {displayName || effectiveOrg.name}
              </Typography>
              {isEditorOrAbove && (
                <IconButton
                  aria-label="edit display name"
                  size="small"
                  onClick={() => {
                    setDraftDisplayName(displayName || '')
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
                disabled={updateOrganizationMutation.isPending}
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
                disabled={updateOrganizationMutation.isPending}
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
              {isEditorOrAbove && (
                <IconButton
                  aria-label="edit description"
                  size="small"
                  onClick={() => {
                    setDraftDescription(description || '')
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
          userGrants={(localOrganization ?? effectiveOrg).userGrants}
          groupGrants={(localOrganization ?? effectiveOrg).groupGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateSharingMutation.isPending}
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
          {deleteOrganizationMutation.error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteOrganizationMutation.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteOpen(false)}>Cancel</Button>
          <Button onClick={handleDelete} color="error" variant="contained" disabled={deleteOrganizationMutation.isPending}>
            {deleteOrganizationMutation.isPending ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  )
}
