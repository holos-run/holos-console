import {
  Card,
  CardContent,
  Typography,
  Stack,
  Box,
  Button,
  Accordion,
  AccordionSummary,
  AccordionDetails,
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'
import { useLocation } from 'react-router-dom'
import { useAuth } from '../auth'

export function ProfilePage() {
  const location = useLocation()
  const { user, isLoading, isAuthenticated, login, logout } = useAuth()

  if (isLoading) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Typography variant="body2">Loading...</Typography>
        </CardContent>
      </Card>
    )
  }

  if (!isAuthenticated) {
    return (
      <Card variant="outlined">
        <CardContent>
          <Typography variant="h6" gutterBottom>
            Profile
          </Typography>
          <Typography variant="body2" sx={{ mb: 2 }}>
            Sign in to view your profile.
          </Typography>
          <Button variant="contained" onClick={() => login(location.pathname)}>
            Sign In
          </Button>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card variant="outlined">
      <CardContent>
        <Typography variant="h6" gutterBottom>
          Profile
        </Typography>
        <Stack spacing={1.5}>
          <Box>
            <Typography variant="overline" display="block">
              Name
            </Typography>
            <Typography variant="body1">
              {user?.profile.name ?? 'Not provided'}
            </Typography>
          </Box>
          <Box>
            <Typography variant="overline" display="block">
              Email
            </Typography>
            <Typography variant="body1">
              {user?.profile.email ?? 'Not provided'}
            </Typography>
          </Box>
          <Box>
            <Typography variant="overline" display="block">
              Subject
            </Typography>
            <Typography variant="body1">{user?.profile.sub}</Typography>
          </Box>
          <Box sx={{ pt: 1 }}>
            <Button variant="outlined" onClick={() => logout()}>
              Sign Out
            </Button>
          </Box>
        </Stack>
        <Accordion sx={{ mt: 2 }}>
          <AccordionSummary expandIcon={<ExpandMoreIcon />}>
            <Typography variant="subtitle2">ID Token Claims</Typography>
          </AccordionSummary>
          <AccordionDetails>
            <Box
              component="pre"
              sx={{
                fontFamily: 'monospace',
                fontSize: '0.75rem',
                backgroundColor: 'grey.100',
                p: 2,
                borderRadius: 1,
                overflow: 'auto',
                maxHeight: 400,
              }}
            >
              {JSON.stringify(user?.profile, null, 2)}
            </Box>
          </AccordionDetails>
        </Accordion>
      </CardContent>
    </Card>
  )
}
