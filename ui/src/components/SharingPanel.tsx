import { useState } from 'react'
import {
  Alert,
  Box,
  Button,
  IconButton,
  List,
  ListItem,
  ListItemText,
  MenuItem,
  Select,
  Stack,
  TextField,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material'
import DeleteIcon from '@mui/icons-material/Delete'
import { Role } from '../gen/holos/console/v1/rbac_pb'

export interface Grant {
  principal: string
  role: Role
  nbf?: bigint
  exp?: bigint
}

export interface SharingPanelProps {
  userGrants: Grant[]
  roleGrants: Grant[]
  isOwner: boolean
  onSave: (userGrants: Grant[], roleGrants: Grant[]) => Promise<void>
  isSaving: boolean
}

function roleName(role: Role): string {
  switch (role) {
    case Role.OWNER:
      return 'Owner'
    case Role.EDITOR:
      return 'Editor'
    case Role.VIEWER:
      return 'Viewer'
    default:
      return 'Unknown'
  }
}

function formatTimeBound(ts?: bigint): string {
  if (ts == null) return ''
  return new Date(Number(ts) * 1000).toLocaleString()
}

function grantSecondary(role: Role, nbf?: bigint, exp?: bigint): string {
  const parts = [roleName(role)]
  if (nbf != null) {
    parts.push(`from ${formatTimeBound(nbf)}`)
  }
  if (exp != null) {
    parts.push(`until ${formatTimeBound(exp)}`)
  }
  return parts.join(' · ')
}

// Convert a bigint unix timestamp to a datetime-local input value (YYYY-MM-DDTHH:mm)
function timestampToDatetimeLocal(ts?: bigint): string {
  if (ts == null) return ''
  const d = new Date(Number(ts) * 1000)
  // Format as YYYY-MM-DDTHH:mm in local time
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}`
}

// Convert a datetime-local input value to a bigint unix timestamp, or undefined if empty
function datetimeLocalToTimestamp(value: string): bigint | undefined {
  if (!value) return undefined
  const d = new Date(value)
  if (isNaN(d.getTime())) return undefined
  return BigInt(Math.floor(d.getTime() / 1000))
}

export function SharingPanel({ userGrants, roleGrants, isOwner, onSave, isSaving }: SharingPanelProps) {
  const theme = useTheme()
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const [editing, setEditing] = useState(false)
  const [editUserGrants, setEditUserGrants] = useState<Grant[]>([])
  const [editRoleGrants, setEditRoleGrants] = useState<Grant[]>([])
  const [saveError, setSaveError] = useState<string | null>(null)

  const handleEdit = () => {
    setEditUserGrants(userGrants.map((g) => ({ ...g })))
    setEditRoleGrants(roleGrants.map((g) => ({ ...g })))
    setSaveError(null)
    setEditing(true)
  }

  const handleCancel = () => {
    setSaveError(null)
    setEditing(false)
  }

  const handleSave = async () => {
    // Filter out empty principals
    const users = editUserGrants.filter((g) => g.principal.trim() !== '')
    const roles = editRoleGrants.filter((g) => g.principal.trim() !== '')
    try {
      await onSave(users, roles)
      setEditing(false)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err))
    }
  }

  const handleAddUser = () => {
    setEditUserGrants([...editUserGrants, { principal: '', role: Role.VIEWER }])
  }

  const handleAddRole = () => {
    setEditRoleGrants([...editRoleGrants, { principal: '', role: Role.VIEWER }])
  }

  const handleRemoveUser = (index: number) => {
    setEditUserGrants(editUserGrants.filter((_, i) => i !== index))
  }

  const handleRemoveRole = (index: number) => {
    setEditRoleGrants(editRoleGrants.filter((_, i) => i !== index))
  }

  const handleUserChange = (index: number, field: keyof Grant, value: string | Role | bigint | undefined) => {
    const updated = [...editUserGrants]
    updated[index] = { ...updated[index], [field]: value }
    setEditUserGrants(updated)
  }

  const handleRoleChange = (index: number, field: keyof Grant, value: string | Role | bigint | undefined) => {
    const updated = [...editRoleGrants]
    updated[index] = { ...updated[index], [field]: value }
    setEditRoleGrants(updated)
  }

  const hasGrants = userGrants.length > 0 || roleGrants.length > 0

  if (!editing) {
    return (
      <Box sx={{ mt: 3 }}>
        <Stack direction="row" justifyContent="space-between" alignItems="center">
          <Typography variant="subtitle1">Sharing</Typography>
          {isOwner && (
            <Button size="small" onClick={handleEdit}>
              Edit
            </Button>
          )}
        </Stack>
        {!hasGrants ? (
          <Typography variant="body2" color="text.secondary">
            No sharing grants configured.
          </Typography>
        ) : (
          <>
            {userGrants.length > 0 && (
              <>
                <Typography variant="caption" color="text.secondary">
                  Users
                </Typography>
                <List dense>
                  {userGrants.map((g) => (
                    <ListItem key={g.principal} disablePadding>
                      <ListItemText primary={g.principal} secondary={grantSecondary(g.role, g.nbf, g.exp)} />
                    </ListItem>
                  ))}
                </List>
              </>
            )}
            {roleGrants.length > 0 && (
              <>
                <Typography variant="caption" color="text.secondary">
                  Roles
                </Typography>
                <List dense>
                  {roleGrants.map((g) => (
                    <ListItem key={g.principal} disablePadding>
                      <ListItemText primary={g.principal} secondary={grantSecondary(g.role, g.nbf, g.exp)} />
                    </ListItem>
                  ))}
                </List>
              </>
            )}
          </>
        )}
      </Box>
    )
  }

  // Edit mode
  return (
    <Box sx={{ mt: 3 }}>
      <Typography variant="subtitle1">Sharing</Typography>

      <Typography variant="caption" color="text.secondary" sx={{ mt: 1, display: 'block' }}>
        Users
      </Typography>
      {editUserGrants.map((g, i) => (
        <Stack key={i} spacing={1} sx={{ mt: 1 }}>
          <Stack direction={isMobile ? 'column' : 'row'} spacing={1} alignItems={isMobile ? 'stretch' : 'center'}>
            <TextField
              size="small"
              placeholder="Email address"
              value={g.principal}
              onChange={(e) => handleUserChange(i, 'principal', e.target.value)}
              sx={{ flex: 1 }}
            />
            <Select
              size="small"
              value={g.role}
              onChange={(e) => handleUserChange(i, 'role', e.target.value as Role)}
            >
              <MenuItem value={Role.VIEWER}>Viewer</MenuItem>
              <MenuItem value={Role.EDITOR}>Editor</MenuItem>
              <MenuItem value={Role.OWNER}>Owner</MenuItem>
            </Select>
            <IconButton size="small" aria-label="remove" onClick={() => handleRemoveUser(i)}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Stack>
          <Stack direction={isMobile ? 'column' : 'row'} spacing={1}>
            <TextField
              size="small"
              label="Not before"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.nbf)}
              onChange={(e) => handleUserChange(i, 'nbf', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
            <TextField
              size="small"
              label="Expires"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.exp)}
              onChange={(e) => handleUserChange(i, 'exp', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
          </Stack>
        </Stack>
      ))}
      <Button size="small" onClick={handleAddUser} sx={{ mt: 1 }}>
        Add User
      </Button>

      <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
        Roles
      </Typography>
      {editRoleGrants.map((g, i) => (
        <Stack key={i} spacing={1} sx={{ mt: 1 }}>
          <Stack direction={isMobile ? 'column' : 'row'} spacing={1} alignItems={isMobile ? 'stretch' : 'center'}>
            <TextField
              size="small"
              placeholder="Role name"
              value={g.principal}
              onChange={(e) => handleRoleChange(i, 'principal', e.target.value)}
              sx={{ flex: 1 }}
            />
            <Select
              size="small"
              value={g.role}
              onChange={(e) => handleRoleChange(i, 'role', e.target.value as Role)}
            >
              <MenuItem value={Role.VIEWER}>Viewer</MenuItem>
              <MenuItem value={Role.EDITOR}>Editor</MenuItem>
              <MenuItem value={Role.OWNER}>Owner</MenuItem>
            </Select>
            <IconButton size="small" aria-label="remove" onClick={() => handleRemoveRole(i)}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Stack>
          <Stack direction={isMobile ? 'column' : 'row'} spacing={1}>
            <TextField
              size="small"
              label="Not before"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.nbf)}
              onChange={(e) => handleRoleChange(i, 'nbf', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
            <TextField
              size="small"
              label="Expires"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.exp)}
              onChange={(e) => handleRoleChange(i, 'exp', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
          </Stack>
        </Stack>
      ))}
      <Button size="small" onClick={handleAddRole} sx={{ mt: 1 }}>
        Add Role
      </Button>

      {saveError && (
        <Alert severity="error" sx={{ mt: 2 }}>
          {saveError}
        </Alert>
      )}

      <Stack direction="row" spacing={1} sx={{ mt: 2 }}>
        <Button variant="contained" size="small" onClick={handleSave} disabled={isSaving}>
          {isSaving ? 'Saving...' : 'Save'}
        </Button>
        <Button size="small" onClick={handleCancel}>
          Cancel
        </Button>
      </Stack>
    </Box>
  )
}
