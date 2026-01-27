import {
  ThemeProvider,
  createTheme,
  CssBaseline,
  Typography,
  Box,
  Stack,
  List,
  ListItemButton,
  ListItemText,
  Divider,
  Card,
  CardContent,
} from '@mui/material'
import {
  Link,
  Navigate,
  Route,
  Routes,
  useLocation,
} from 'react-router-dom'
import { VersionCard } from './components/VersionCard'
import { ProfilePage } from './components/ProfilePage'
import { AuthDebugPage } from './components/AuthDebugPage'
import { SecretPage } from './components/SecretPage'
import { AuthProvider, Callback } from './auth'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

function MainLayout() {
  const location = useLocation()
  const isVersionPage = location.pathname.startsWith('/version')
  const isProfilePage = location.pathname.startsWith('/profile')
  const isAuthDebugPage = location.pathname.startsWith('/auth-debug')
  const isSecretsPage = location.pathname.startsWith('/secrets')

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      <Box
        component="nav"
        sx={{
          width: 240,
          flexShrink: 0,
          borderRight: 1,
          borderColor: 'divider',
          bgcolor: 'background.paper',
        }}
      >
        <Box sx={{ p: 3 }}>
          <Typography variant="h6" component="div">
            Holos Console
          </Typography>
        </Box>
        <Divider />
        <List sx={{ px: 1 }}>
          <ListItemButton
            component={Link}
            to="/"
            selected={!isVersionPage && !isProfilePage && !isAuthDebugPage && !isSecretsPage}
          >
            <ListItemText primary="Landing" />
          </ListItemButton>
          <ListItemButton
            component={Link}
            to="/version"
            selected={isVersionPage}
          >
            <ListItemText primary="Version" />
          </ListItemButton>
          <ListItemButton
            component={Link}
            to="/profile"
            selected={isProfilePage}
          >
            <ListItemText primary="Profile" />
          </ListItemButton>
          <ListItemButton
            component={Link}
            to="/auth-debug"
            selected={isAuthDebugPage}
          >
            <ListItemText primary="Auth Debug" />
          </ListItemButton>
          <ListItemButton
            component={Link}
            to="/secrets/dummy-secret"
            selected={isSecretsPage}
          >
            <ListItemText primary="Secrets" />
          </ListItemButton>
        </List>
      </Box>
      <Box sx={{ flex: 1, p: 4 }}>
        <Stack spacing={3}>
          <Routes>
            <Route
              path="/"
              element={
                <Card variant="outlined">
                  <CardContent>
                    <Typography variant="h4" component="h1" gutterBottom>
                      Welcome to Holos Console
                    </Typography>
                    <Typography variant="body1" gutterBottom>
                      This is your landing page. Use the sidebar to explore
                      server details and status.
                    </Typography>
                    <Typography variant="body2" color="text.secondary">
                      Start by opening the Version page to verify the backend
                      connection.
                    </Typography>
                  </CardContent>
                </Card>
              }
            />
            <Route path="/version" element={<VersionCard />} />
            <Route path="/profile" element={<ProfilePage />} />
            <Route path="/auth-debug" element={<AuthDebugPage />} />
            <Route path="/secrets/:name" element={<SecretPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Stack>
      </Box>
    </Box>
  )
}

function App() {
  const location = useLocation()

  // Callback route renders without the main layout
  if (location.pathname === '/callback') {
    return (
      <ThemeProvider theme={theme}>
        <CssBaseline />
        <AuthProvider>
          <Callback />
        </AuthProvider>
      </ThemeProvider>
    )
  }

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <AuthProvider>
        <MainLayout />
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App
