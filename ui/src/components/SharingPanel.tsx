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

interface Grant {
  principal: string
  role: Role
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

  const handleUserChange = (index: number, field: 'principal' | 'role', value: string | Role) => {
    const updated = [...editUserGrants]
    if (field === 'principal') {
      updated[index] = { ...updated[index], principal: value as string }
    } else {
      updated[index] = { ...updated[index], role: value as Role }
    }
    setEditUserGrants(updated)
  }

  const handleGroupChange = (index: number, field: 'principal' | 'role', value: string | Role) => {
    const updated = [...editGroupGrants]
    if (field === 'principal') {
      updated[index] = { ...updated[index], principal: value as string }
    } else {
      updated[index] = { ...updated[index], role: value as Role }
    }
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
                      <ListItemText primary={g.principal} secondary={roleName(g.role)} />
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
                      <ListItemText primary={g.principal} secondary={roleName(g.role)} />
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
        <Stack key={i} direction="row" spacing={1} alignItems="center" sx={{ mt: 1 }}>
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
      ))}
      <Button size="small" onClick={handleAddUser} sx={{ mt: 1 }}>
        Add User
      </Button>

      <Typography variant="caption" color="text.secondary" sx={{ mt: 2, display: 'block' }}>
        Groups
      </Typography>
      {editGroupGrants.map((g, i) => (
        <Stack key={i} direction="row" spacing={1} alignItems="center" sx={{ mt: 1 }}>
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
