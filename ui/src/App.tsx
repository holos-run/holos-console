import { useState } from 'react'
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
  AppBar,
  Toolbar,
  IconButton,
  Drawer,
  useMediaQuery,
} from '@mui/material'
import MenuIcon from '@mui/icons-material/Menu'
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
import { SecretsListPage } from './components/SecretsListPage'
import { SecretPage } from './components/SecretPage'
import { AuthProvider, Callback } from './auth'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

const DRAWER_WIDTH = 240

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const location = useLocation()
  const isVersionPage = location.pathname.startsWith('/version')
  const isProfilePage = location.pathname.startsWith('/profile')
  const isAuthDebugPage = location.pathname.startsWith('/auth-debug')
  const isSecretsPage = location.pathname.startsWith('/secrets')

  return (
    <>
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
          onClick={onNavigate}
        >
          <ListItemText primary="Landing" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/version"
          selected={isVersionPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Version" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/profile"
          selected={isProfilePage}
          onClick={onNavigate}
        >
          <ListItemText primary="Profile" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/auth-debug"
          selected={isAuthDebugPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Auth Debug" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/secrets"
          selected={isSecretsPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Secrets" />
        </ListItemButton>
      </List>
    </>
  )
}

function MainLayout() {
  const isMobile = useMediaQuery(theme.breakpoints.down('md'))
  const [mobileOpen, setMobileOpen] = useState(false)

  return (
    <Box sx={{ display: 'flex', minHeight: '100vh' }}>
      {/* Mobile AppBar */}
      {isMobile && (
        <AppBar position="fixed">
          <Toolbar>
            <IconButton
              color="inherit"
              aria-label="open menu"
              edge="start"
              onClick={() => setMobileOpen(true)}
              sx={{ mr: 2 }}
            >
              <MenuIcon />
            </IconButton>
            <Typography variant="h6" noWrap component="div">
              Holos Console
            </Typography>
          </Toolbar>
        </AppBar>
      )}

      {/* Mobile drawer (temporary) */}
      {isMobile && (
        <Drawer
          variant="temporary"
          open={mobileOpen}
          onClose={() => setMobileOpen(false)}
          ModalProps={{ keepMounted: true }}
          sx={{
            '& .MuiDrawer-paper': { boxSizing: 'border-box', width: DRAWER_WIDTH },
          }}
        >
          <SidebarContent onNavigate={() => setMobileOpen(false)} />
        </Drawer>
      )}

      {/* Desktop drawer (permanent) */}
      {!isMobile && (
        <Drawer
          variant="permanent"
          sx={{
            width: DRAWER_WIDTH,
            flexShrink: 0,
            '& .MuiDrawer-paper': {
              width: DRAWER_WIDTH,
              boxSizing: 'border-box',
            },
          }}
          open
        >
          <SidebarContent />
        </Drawer>
      )}

      {/* Main content */}
      <Box sx={{ flex: 1, p: 4 }}>
        {/* Toolbar spacer on mobile to push content below AppBar */}
        {isMobile && <Toolbar />}
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
            <Route path="/secrets" element={<SecretsListPage />} />
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
