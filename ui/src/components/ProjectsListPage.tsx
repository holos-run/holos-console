import { useState, useEffect } from 'react'
import { Link as RouterLink, useNavigate, useParams } from 'react-router-dom'
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
import { projectsClient } from '../client'
import { useOrg } from '../OrgProvider'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import type { Project } from '../gen/holos/console/v1/projects_pb'

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
  const { organizationName } = useParams<{ organizationName?: string }>()
  const { selectedOrg } = useOrg()
  const effectiveOrg = organizationName || selectedOrg || ''
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const navigate = useNavigate()
  const { user, isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [projects, setProjects] = useState<Project[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createDisplayName, setCreateDisplayName] = useState('')
  const [createDescription, setCreateDescription] = useState('')
  const [createError, setCreateError] = useState<string | null>(null)
  const [isCreating, setIsCreating] = useState(false)
  const [createSuccess, setCreateSuccess] = useState(false)

  // Delete state
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)
  const [deleteSuccess, setDeleteSuccess] = useState(false)

  // Redirect to login if not authenticated
  useEffect(() => {
    if (!authLoading && !isAuthenticated) {
      login(effectiveOrg ? `/organizations/${effectiveOrg}/projects` : '/projects')
    }
  }, [authLoading, isAuthenticated, login, effectiveOrg])

  // Fetch projects list when authenticated
  const fetchProjects = async () => {
    if (!isAuthenticated) return
    setIsLoading(true)
    setError(null)

    try {
      const token = getAccessToken()
      const response = await projectsClient.listProjects(
        { organization: effectiveOrg },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )

      setProjects(response.projects)
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchProjects()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuthenticated, getAccessToken, effectiveOrg])

  const handleDeleteOpen = (name: string) => {
    setDeleteTarget(name)
    setDeleteError(null)
    setDeleteOpen(true)
  }

  const handleDeleteConfirm = async () => {
    if (!deleteTarget) return
    setIsDeleting(true)
    setDeleteError(null)

    try {
      const token = getAccessToken()
      await projectsClient.deleteProject(
        { name: deleteTarget },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )
      setDeleteOpen(false)
      setDeleteTarget(null)
      setDeleteSuccess(true)
      fetchProjects()
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsDeleting(false)
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

    setIsCreating(true)
    setCreateError(null)

    try {
      const token = getAccessToken()
      await projectsClient.createProject(
        {
          name: createName.trim(),
          displayName: createDisplayName.trim(),
          description: createDescription.trim(),
          organization: effectiveOrg,
          userGrants: [{ principal: (user?.profile?.email as string) || '', role: Role.OWNER }],
          groupGrants: [],
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )

      setCreateOpen(false)
      setCreateSuccess(true)
      navigate(`/projects/${createName.trim()}`)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsCreating(false)
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
          <Button onClick={handleCreate} variant="contained" disabled={isCreating}>
            {isCreating ? 'Creating...' : 'Create'}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} fullScreen={isMobile}>
        <DialogTitle>Delete Project</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete project &quot;{deleteTarget}&quot;? This will delete the namespace and all resources within it. This action cannot be undone.
          </DialogContentText>
          {deleteError && (
            <Alert severity="error" sx={{ mt: 2 }}>
              {deleteError}
            </Alert>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDeleteOpen(false)}>Cancel</Button>
          <Button onClick={handleDeleteConfirm} color="error" variant="contained" disabled={isDeleting}>
            {isDeleting ? 'Deleting...' : 'Delete'}
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
