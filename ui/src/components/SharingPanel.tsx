import { useState } from 'react'
import {
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
  groupGrants: Grant[]
  isOwner: boolean
  onSave: (userGrants: Grant[], groupGrants: Grant[]) => Promise<void>
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

export function SharingPanel({ userGrants, groupGrants, isOwner, onSave, isSaving }: SharingPanelProps) {
  const [editing, setEditing] = useState(false)
  const [editUserGrants, setEditUserGrants] = useState<Grant[]>([])
  const [editGroupGrants, setEditGroupGrants] = useState<Grant[]>([])

  const handleEdit = () => {
    setEditUserGrants(userGrants.map((g) => ({ ...g })))
    setEditGroupGrants(groupGrants.map((g) => ({ ...g })))
    setEditing(true)
  }

  const handleCancel = () => {
    setEditing(false)
  }

  const handleSave = async () => {
    // Filter out empty principals
    const users = editUserGrants.filter((g) => g.principal.trim() !== '')
    const groups = editGroupGrants.filter((g) => g.principal.trim() !== '')
    await onSave(users, groups)
    setEditing(false)
  }

  const handleAddUser = () => {
    setEditUserGrants([...editUserGrants, { principal: '', role: Role.VIEWER }])
  }

  const handleAddGroup = () => {
    setEditGroupGrants([...editGroupGrants, { principal: '', role: Role.VIEWER }])
  }

  const handleRemoveUser = (index: number) => {
    setEditUserGrants(editUserGrants.filter((_, i) => i !== index))
  }

  const handleRemoveGroup = (index: number) => {
    setEditGroupGrants(editGroupGrants.filter((_, i) => i !== index))
  }

  const handleUserChange = (index: number, field: keyof Grant, value: string | Role | bigint | undefined) => {
    const updated = [...editUserGrants]
    updated[index] = { ...updated[index], [field]: value }
    setEditUserGrants(updated)
  }

  const handleGroupChange = (index: number, field: keyof Grant, value: string | Role | bigint | undefined) => {
    const updated = [...editGroupGrants]
    updated[index] = { ...updated[index], [field]: value }
    setEditGroupGrants(updated)
  }

  const hasGrants = userGrants.length > 0 || groupGrants.length > 0

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
            {groupGrants.length > 0 && (
              <>
                <Typography variant="caption" color="text.secondary">
                  Groups
                </Typography>
                <List dense>
                  {groupGrants.map((g) => (
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
          <Stack direction="row" spacing={1} alignItems="center">
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
          <Stack direction="row" spacing={1}>
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
        Groups
      </Typography>
      {editGroupGrants.map((g, i) => (
        <Stack key={i} spacing={1} sx={{ mt: 1 }}>
          <Stack direction="row" spacing={1} alignItems="center">
            <TextField
              size="small"
              placeholder="Group name"
              value={g.principal}
              onChange={(e) => handleGroupChange(i, 'principal', e.target.value)}
              sx={{ flex: 1 }}
            />
            <Select
              size="small"
              value={g.role}
              onChange={(e) => handleGroupChange(i, 'role', e.target.value as Role)}
            >
              <MenuItem value={Role.VIEWER}>Viewer</MenuItem>
              <MenuItem value={Role.EDITOR}>Editor</MenuItem>
              <MenuItem value={Role.OWNER}>Owner</MenuItem>
            </Select>
            <IconButton size="small" aria-label="remove" onClick={() => handleRemoveGroup(i)}>
              <DeleteIcon fontSize="small" />
            </IconButton>
          </Stack>
          <Stack direction="row" spacing={1}>
            <TextField
              size="small"
              label="Not before"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.nbf)}
              onChange={(e) => handleGroupChange(i, 'nbf', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
            <TextField
              size="small"
              label="Expires"
              type="datetime-local"
              value={timestampToDatetimeLocal(g.exp)}
              onChange={(e) => handleGroupChange(i, 'exp', datetimeLocalToTimestamp(e.target.value))}
              slotProps={{ inputLabel: { shrink: true } }}
              sx={{ flex: 1 }}
            />
          </Stack>
        </Stack>
      ))}
      <Button size="small" onClick={handleAddGroup} sx={{ mt: 1 }}>
        Add Group
      </Button>

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
