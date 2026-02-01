import { useState } from 'react'
import { useParams, useNavigate, Link as RouterLink } from 'react-router-dom'
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
import { SharingPanel, type Grant } from './SharingPanel'
import { useGetProject, useDeleteProject, useUpdateProject, useUpdateProjectSharing } from '../queries/projects'
import { Role } from '../gen/holos/console/v1/rbac_pb'

export function ProjectPage() {
  const { projectName: name } = useParams<{ projectName: string }>()
  const navigate = useNavigate()
  const muiTheme = useTheme()
  const isMobile = useMediaQuery(muiTheme.breakpoints.down('md'))
  const { isAuthenticated, isLoading: authLoading, login } = useAuth()

  const { data, isLoading, error } = useGetProject(name ?? '')
  const project = data?.project

  const deleteProjectMutation = useDeleteProject()
  const updateProjectMutation = useUpdateProject()
  const updateSharingMutation = useUpdateProjectSharing()

  // Inline edit state for display name
  const [editingDisplayName, setEditingDisplayName] = useState(false)
  const [draftDisplayName, setDraftDisplayName] = useState('')

  // Inline edit state for description
  const [editingDescription, setEditingDescription] = useState(false)
  const [draftDescription, setDraftDescription] = useState('')

  // Save success snackbar
  const [saveSuccess, setSaveSuccess] = useState(false)

  // Delete dialog
  const [deleteOpen, setDeleteOpen] = useState(false)

  // Local project overrides for optimistic display after update
  const [localDisplayName, setLocalDisplayName] = useState<string | null>(null)
  const [localDescription, setLocalDescription] = useState<string | null>(null)
  const [localProject, setLocalProject] = useState<typeof project | null>(null)

  // Redirect to login if not authenticated
  if (!authLoading && !isAuthenticated) {
    login(`/projects/${name}`)
  }

  const effectiveProject = localProject ?? project
  const displayName = localDisplayName ?? effectiveProject?.displayName
  const description = localDescription ?? effectiveProject?.description

  const isOwner = effectiveProject?.userRole === Role.OWNER
  const isEditorOrAbove = effectiveProject != null && effectiveProject.userRole >= Role.EDITOR

  const handleSaveDisplayName = async (newDisplayName: string) => {
    if (!name) return
    try {
      await updateProjectMutation.mutateAsync({ name, displayName: newDisplayName })
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
      await updateProjectMutation.mutateAsync({ name, description: newDescription })
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
      if (response.project) {
        setLocalProject(response.project)
      }
    } catch {
      // Error handling could be added here
    }
  }

  const handleDelete = async () => {
    if (!name) return

    try {
      await deleteProjectMutation.mutateAsync({ name })
      setDeleteOpen(false)
      navigate('/projects')
    } catch {
      // Error is available via deleteProjectMutation.error
    }
  }

  // Show loading while checking auth or fetching project
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
      displayMessage = `Project "${name}" not found`
    } else if (
      errorMessage.includes('permission') ||
      errorMessage.includes('denied')
    ) {
      displayMessage = 'Permission denied: You are not authorized to view this project'
    }

    return (
      <Card variant="outlined">
        <CardContent>
          <Alert severity="error">{displayMessage}</Alert>
        </CardContent>
      </Card>
    )
  }

  if (!effectiveProject) return null

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="subtitle2" color="text.secondary" gutterBottom>
          {effectiveProject.organization ? (
            <RouterLink to={`/organizations/${effectiveProject.organization}`} style={{ color: 'inherit' }}>
              {effectiveProject.organization}
            </RouterLink>
          ) : null}
          {effectiveProject.organization ? ' / ' : ''}
          {effectiveProject.name}
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
                disabled={updateProjectMutation.isPending}
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
                disabled={updateProjectMutation.isPending}
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
                {displayName || effectiveProject.name}
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
                placeholder="What is this project for?"
                disabled={updateProjectMutation.isPending}
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
                disabled={updateProjectMutation.isPending}
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

        {/* Actions */}
        <Stack direction={{ xs: 'column', sm: 'row' }} spacing={2} sx={{ mb: 2 }}>
          <Button
            variant="contained"
            component={RouterLink}
            to={`/projects/${name}/secrets`}
          >
            Secrets
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
          userGrants={(localProject ?? effectiveProject).userGrants}
          groupGrants={(localProject ?? effectiveProject).groupGrants}
          isOwner={isOwner}
          onSave={handleSaveSharing}
          isSaving={updateSharingMutation.isPending}
        />

        <Snackbar
          open={saveSuccess}
          autoHideDuration={3000}
          onClose={() => setSaveSuccess(false)}
          message="Project updated successfully"
        />
      </CardContent>

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} fullScreen={isMobile}>
        <DialogTitle>Delete Project</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete project &quot;{name}&quot;? This will delete the namespace and all resources within it. This action cannot be undone.
          </DialogContentText>
          {deleteProjectMutation.error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteProjectMutation.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteOpen(false)}>Cancel</Button>
          <Button onClick={handleDelete} color="error" variant="contained" disabled={deleteProjectMutation.isPending}>
            {deleteProjectMutation.isPending ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  )
}
