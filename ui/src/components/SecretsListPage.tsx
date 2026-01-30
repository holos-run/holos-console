import { useState, useEffect } from 'react'
import { Link as RouterLink } from 'react-router-dom'
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
  FormControl,
  FormControlLabel,
  Checkbox,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Snackbar,
  Stack,
  TextField,
  Tooltip,
  Chip,
} from '@mui/material'
import LockIcon from '@mui/icons-material/Lock'
import DeleteIcon from '@mui/icons-material/Delete'
import { useAuth } from '../auth'
import { secretsClient } from '../client'
import type { SecretMetadata } from '../gen/holos/console/v1/secrets_pb'

export function SecretsListPage() {
  const { isAuthenticated, isLoading: authLoading, login, getAccessToken } = useAuth()

  const [secrets, setSecrets] = useState<SecretMetadata[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  // Create dialog state
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState('')
  const [createData, setCreateData] = useState('')
  const [createRoles, setCreateRoles] = useState<string[]>(['editor'])
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
      login('/secrets')
    }
  }, [authLoading, isAuthenticated, login])

  // Fetch secrets list when authenticated
  const fetchSecrets = async () => {
    if (!isAuthenticated) return
    setIsLoading(true)
    setError(null)

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

      setSecrets(response.secrets)
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    fetchSecrets()
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
      await secretsClient.deleteSecret(
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
      fetchSecrets()
    } catch (err) {
      setDeleteError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsDeleting(false)
    }
  }

  const handleCreateOpen = () => {
    setCreateName('')
    setCreateData('')
    setCreateRoles(['editor'])
    setCreateError(null)
    setCreateOpen(true)
  }

  const handleCreateClose = () => {
    setCreateOpen(false)
  }

  const handleRoleToggle = (role: string) => {
    setCreateRoles((prev) =>
      prev.includes(role) ? prev.filter((r) => r !== role) : [...prev, role],
    )
  }

  const handleCreate = async () => {
    if (!createName.trim()) {
      setCreateError('Secret name is required')
      return
    }
    if (createRoles.length === 0) {
      setCreateError('At least one role is required')
      return
    }

    setIsCreating(true)
    setCreateError(null)

    try {
      const token = getAccessToken()
      const encoder = new TextEncoder()
      const data: Record<string, Uint8Array> = {}
      for (const line of createData.split('\n')) {
        const trimmed = line.trim()
        if (trimmed === '' || trimmed.startsWith('#')) continue
        const eqIndex = trimmed.indexOf('=')
        if (eqIndex === -1) continue
        const key = trimmed.slice(0, eqIndex)
        const value = trimmed.slice(eqIndex + 1)
        data[key] = encoder.encode(value)
      }

      await secretsClient.createSecret(
        {
          name: createName.trim(),
          data,
          allowedRoles: createRoles,
        },
        {
          headers: {
            Authorization: `Bearer ${token}`,
          },
        },
      )

      setCreateOpen(false)
      setCreateSuccess(true)
      fetchSecrets()
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : String(err))
    } finally {
      setIsCreating(false)
    }
  }

  // Show loading while checking auth or fetching secrets
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

  // Show secrets list
  return (
    <>
      <Card variant="outlined">
        <CardContent>
          <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 1 }}>
            <Typography variant="h6">Secrets</Typography>
            <Button variant="contained" size="small" onClick={handleCreateOpen}>
              Create Secret
            </Button>
          </Stack>
          {secrets.length === 0 ? (
            <Typography color="text.secondary">
              No secrets available. Secrets must have the label{' '}
              <code>app.kubernetes.io/managed-by=console.holos.run</code> to appear here.
            </Typography>
          ) : (
            <List>
              {secrets.map((secret) => (
                <ListItem
                  key={secret.name}
                  disablePadding
                  secondaryAction={
                    !secret.accessible ? (
                      <Tooltip
                        title={
                          secret.allowedGroups.length > 0
                            ? `Access restricted to: ${secret.allowedGroups.join(', ')}`
                            : 'No groups have access to this secret'
                        }
                      >
                        <Chip
                          icon={<LockIcon />}
                          label="No access"
                          size="small"
                          color="default"
                          variant="outlined"
                        />
                      </Tooltip>
                    ) : (
                      <IconButton
                        edge="end"
                        aria-label={`delete ${secret.name}`}
                        onClick={() => handleDeleteOpen(secret.name)}
                        size="small"
                      >
                        <DeleteIcon />
                      </IconButton>
                    )
                  }
                >
                  <ListItemButton
                    component={RouterLink}
                    to={`/secrets/${secret.name}`}
                    disabled={!secret.accessible}
                  >
                    <ListItemText primary={secret.name} />
                  </ListItemButton>
                </ListItem>
              ))}
            </List>
          )}
        </CardContent>
      </Card>

      <Dialog open={createOpen} onClose={handleCreateClose} maxWidth="sm" fullWidth>
        <DialogTitle>Create Secret</DialogTitle>
        <DialogContent>
          <TextField
            autoFocus
            margin="dense"
            label="Name"
            fullWidth
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
            placeholder="my-secret"
            helperText="Lowercase alphanumeric and hyphens only"
          />
          <TextField
            margin="dense"
            label="Data (env format)"
            fullWidth
            multiline
            minRows={4}
            value={createData}
            onChange={(e) => setCreateData(e.target.value)}
            placeholder={'KEY=value\nANOTHER_KEY=another-value'}
            slotProps={{
              input: {
                sx: { fontFamily: 'monospace' },
              },
            }}
          />
          <FormControl component="fieldset" sx={{ mt: 2 }}>
            <Typography variant="subtitle2" gutterBottom>
              Allowed Roles
            </Typography>
            {['viewer', 'editor', 'owner'].map((role) => (
              <FormControlLabel
                key={role}
                control={
                  <Checkbox
                    checked={createRoles.includes(role)}
                    onChange={() => handleRoleToggle(role)}
                  />
                }
                label={role.charAt(0).toUpperCase() + role.slice(1)}
              />
            ))}
          </FormControl>
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

      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)}>
        <DialogTitle>Delete Secret</DialogTitle>
        <DialogContent>
          <DialogContentText>
            Are you sure you want to delete secret &quot;{deleteTarget}&quot;? This action cannot be undone.
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
        message="Secret created successfully"
      />
      <Snackbar
        open={deleteSuccess}
        autoHideDuration={3000}
        onClose={() => setDeleteSuccess(false)}
        message="Secret deleted successfully"
      />
    </>
  )
}
