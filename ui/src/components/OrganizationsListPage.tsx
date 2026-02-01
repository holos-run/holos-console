import { useState, useEffect } from 'react'
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
  useMediaQuery,
  useTheme,
} from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import { useAuth } from '../auth'
import { organizationsClient } from '../client'
import { Role } from '../gen/holos/console/v1/rbac_pb'
import type { Organization } from '../gen/holos/console/v1/organizations_pb'

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

export function OrganizationsListPage() {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const navigate = useNavigate()
  const { user, isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [organizations, setOrganizations] = useState<Organization[]>([])
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
      login('/organizations')
    }
  }, [authLoading, isAuthenticated, login])

  // Fetch organizations list when authenticated
  const fetchOrganizations = async () => {
    if (!isAuthenticated) return
    setIsLoading(true)
    setError(null)

    try {
      const token = getAccessToken()
      const response = await organizationsClient.listOrganizations(
        {},
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )

      setOrganizations(response.organizations)
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchOrganizations()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuthenticated, getAccessToken])

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
      await organizationsClient.deleteOrganization(
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
      fetchOrganizations()
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
      setCreateError('Organization name is required')
      return
    }

    setIsCreating(true)
    setCreateError(null)

    try {
      const token = getAccessToken()
      await organizationsClient.createOrganization(
        {
          name: createName.trim(),
          displayName: createDisplayName.trim(),
          description: createDescription.trim(),
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
      navigate(`/organizations/${createName.trim()}`)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsCreating(false)
    }
  }

  // Show loading while checking auth or fetching organizations
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

  // Show organizations list
  return (
    <>
      <Card variant="outlined">
        <CardContent>
          <Stack direction={{ xs: 'column', sm: 'row' }} justifyContent="space-between" alignItems={{ xs: 'stretch', sm: 'center' }} spacing={1} sx={{ mb: 1 }}>
            <Typography variant="h6">Organizations</Typography>
            <Button variant="contained" size="small" onClick={handleCreateOpen}>
              Create Organization
            </Button>
          </Stack>
          {organizations.length === 0 ? (
            <Typography color="text.secondary">
              No organizations available.
            </Typography>
          ) : (
            <List>
              {organizations.map((org) => (
                <ListItem
                  key={org.name}
                  disablePadding
                  secondaryAction={
                    org.userRole === Role.OWNER ? (
                      <IconButton
                        edge="end"
                        aria-label={`delete ${org.name}`}
                        onClick={() => handleDeleteOpen(org.name)}
                        size="small"
                      >
                        <DeleteIcon />
                      </IconButton>
                    ) : undefined
                  }
                >
                  <ListItemButton
                    component={RouterLink}
                    to={`/organizations/${org.name}`}
                  >
                    <ListItemText
                      primary={org.displayName || org.name}
                      secondary={org.description || org.name}
                    />
                    <Chip
                      label={roleName(org.userRole)}
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
        <DialogTitle>Create Organization</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Name"
            fullWidth
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
            placeholder="my-org"
            helperText="Lowercase alphanumeric and hyphens"
          />
          <TextField
            margin="dense"
            label="Display Name"
            fullWidth
            value={createDisplayName}
            onChange={(e) => setCreateDisplayName(e.target.value)}
            placeholder="My Organization"
          />
          <TextField
            margin="dense"
            label="Description"
            fullWidth
            value={createDescription}
            onChange={(e) => setCreateDescription(e.target.value)}
            placeholder="What is this organization for?"
          />
          <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
            You will be added as the Owner of this organization.
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
        <DialogTitle>Delete Organization</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete organization &quot;{deleteTarget}&quot;? This action cannot be undone.
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
        message="Organization created successfully"
      />
      <Snackbar
        open={deleteSuccess}
        autoHideDuration={3000}
        onClose={() => setDeleteSuccess(false)}
        message="Organization deleted successfully"
      />
    </>
  )
}
