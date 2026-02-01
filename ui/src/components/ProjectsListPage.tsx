import { useState } from 'react'
import { Link as RouterLink, useNavigate } from 'react-router-dom'
import {
  Card,
  CardContent,
  Typography,
  Box,
  Alert,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Snackbar,
  Stack,
  TextField,
  Tooltip,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import { useAuth } from '../auth'
import { useOrg } from '../OrgProvider'
import { useListProjects, useDeleteProject, useCreateProject } from '../queries/projects'
import { Role } from '../gen/holos/console/v1/rbac_pb'

function roleName(role: Role): string {
  switch (role) {
    case Role.OWNER:
      return 'Owner'
    case Role.EDITOR:
      return 'Editor'
    case Role.VIEWER:
      return 'Viewer'
    default:
      return 'None'
  }
}

export function ProjectsListPage() {
  const { selectedOrg } = useOrg()
  const effectiveOrg = selectedOrg || ''
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const navigate = useNavigate()
  const { user, isAuthenticated, isLoading: authLoading, login } = useAuth()

  const { data, isLoading, error } = useListProjects(effectiveOrg)
  const projects = data?.projects ?? []

  const deleteProjectMutation = useDeleteProject()
  const createProjectMutation = useCreateProject()

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [createSuccess, setCreateSuccess] = useState(false)

  // Delete state
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleteSuccess, setDeleteSuccess] = useState(false)

  // Redirect to login if not authenticated
  if (!authLoading && !isAuthenticated) {
    login('/projects')
  }

  const handleDeleteOpen = (name: string) => {
    setDeleteTarget(name)
    deleteProjectMutation.reset()
    setDeleteOpen(true)
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return

    try {
      await deleteProjectMutation.mutateAsync({ name: deleteTarget })
      setDeleteOpen(false)
      setDeleteTarget(null)
      setDeleteSuccess(true)
    } catch {
      // Error is available via deleteProjectMutation.error
    }
  }

  const handleCreateOpen = () => {
    setCreateName('')
    setCreateDisplayName('')
    setCreateDescription('')
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleCreateClose = () => {
    setCreateOpen(false)
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Project name is required')
      return
    }

    setCreateError(null)

    try {
      await createProjectMutation.mutateAsync({
        name: createName.trim(),
        displayName: createDisplayName.trim(),
        description: createDescription.trim(),
        organization: effectiveOrg,
        userGrants: [{ principal: (user?.profile?.email as string) || '', role: Role.OWNER }],
        groupGrants: [],
      })

      setCreateOpen(false)
      setCreateSuccess(true)
      navigate(`/projects/${createName.trim()}`)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    }
  }

  // Show loading while checking auth or fetching projects
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

  // Show projects list
  return (
    <>
      <Card variant="outlined">
        <CardContent>
          <Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" alignItems={{ xs: 'stretch', sm: 'center' }} spacing={1} sx={{ mb: 1 }}>
            <Typography variant="h6">{effectiveOrg ? `Projects in ${effectiveOrg}` : 'Projects'}</Typography>
            <Tooltip title={effectiveOrg ? '' : 'Select an organization first'}>
              <span>
                <Button variant="contained" size="small" onClick={handleCreateOpen} disabled={!effectiveOrg}>
                  Create Project
                </Button>
              </span>
            </Tooltip>
          </Stack>
          {projects.length === 0 ? (
            <Typography color="text.secondary">
              No projects available. Projects are Kubernetes namespaces labeled{' '}
              <code>app.kubernetes.io/managed-by=console.holos.run</code>.
            </Typography>
          ) : (
            <List>
              {projects.map((project) => (
                <ListItem
                  key={project.name}
                  disablePadding
                  secondaryAction={
                    project.userRole === Role.OWNER ? (
                      <IconButton
                        edge="end"
                        aria-label={`delete ${project.name}`}
                        onClick={() => handleDeleteOpen(project.name)}
                        size="small"
                      >
                        <DeleteIcon />
                      </IconButton>
                    ) : undefined
                  }
                >
                  <ListItemButton
                    component={RouterLink}
                    to={`/projects/${project.name}`}
                  >
                    <ListItemText
                      primary={project.displayName || project.name}
                      secondary={project.description || project.name}
                    />
                    <Chip
                      label={roleName(project.userRole)}
                      size="small"
                      variant="outlined"
                      sx={{ ml: 1, flexShrink: 0 }}
                    />
                  </ListItemButton>
                </ListItem>
              ))}
            </List>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onClose={handleCreateClose} maxWidth="sm" fullWidth fullScreen={isMobile}>
        <DialogTitle>Create Project</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Name"
            fullWidth
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
            placeholder="my-project"
            helperText="Kubernetes namespace name (lowercase alphanumeric and hyphens)"
          />
          <TextField
            margin="dense"
            label="Display Name"
            fullWidth
            value={createDisplayName}
            onChange={(e) => setCreateDisplayName(e.target.value)}
            placeholder="My Project"
          />
          <TextField
            margin="dense"
            label="Description"
            fullWidth
            value={createDescription}
            onChange={(e) => setCreateDescription(e.target.value)}
            placeholder="What is this project for?"
          />
          <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
            You will be added as the Owner of this project.
          </Typography>
          {createError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {createError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={handleCreateClose}>Cancel</Button>
          <Button onClick={handleCreate} variant="contained" disabled={createProjectMutation.isPending}>
            {createProjectMutation.isPending ? 'Creating...' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} fullScreen={isMobile}>
        <DialogTitle>Delete Project</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete project &quot;{deleteTarget}&quot;? This will delete the namespace and all resources within it. This action cannot be undone.
          </DialogContentText>
          {deleteProjectMutation.error && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteProjectMutation.error.message}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteOpen(false)}>Cancel</Button>
          <Button onClick={handleDeleteConfirm} color="error" variant="contained" disabled={deleteProjectMutation.isPending}>
            {deleteProjectMutation.isPending ? 'Deleting...' : 'Delete'}
          </Button>
        </DialogActions>
      </Dialog>

      <Snackbar
        open={createSuccess}
        autoHideDuration={3000}
        onClose={() => setCreateSuccess(false)}
        message="Project created successfully"
      />
      <Snackbar
        open={deleteSuccess}
        autoHideDuration={3000}
        onClose={() => setDeleteSuccess(false)}
        message="Project deleted successfully"
      />
    </>
  )
}
