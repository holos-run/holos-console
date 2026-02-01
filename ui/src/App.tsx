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
import { AuthDebugPage } from './components/AuthDebugPage'
import { SecretsListPage } from './components/SecretsListPage'
import { SecretPage } from './components/SecretPage'
import { ProjectsListPage } from './components/ProjectsListPage'
import { ProjectPage } from './components/ProjectPage'
import { AuthProvider } from './auth'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

const DRAWER_WIDTH = 240

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const location = useLocation()
  const isProjectsPage = location.pathname.startsWith('/projects')
  const isProfilePage = location.pathname.startsWith('/profile')
  const isVersionPage = location.pathname.startsWith('/version')

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3 }}>
        <Typography variant="h6" component="div">
          Holos Console
        </Typography>
      </Box>
      <Divider />
      <List sx={{ px: 1 }}>
        <ListItemButton
          component={Link}
          to="/projects"
          selected={isProjectsPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Projects" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/profile"
          selected={isProfilePage}
          onClick={onNavigate}
        >
          <ListItemText primary="Profile" />
        </ListItemButton>
      </List>
      <Box sx={{ flexGrow: 1 }} />
      <Divider />
      <List sx={{ px: 1 }}>
        <ListItemButton
          component={Link}
          to="/version"
          selected={isVersionPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Version" />
        </ListItemButton>
      </List>
    </Box>
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
            <Route path="/" element={<Navigate to="/projects" replace />} />
            <Route path="/projects" element={<ProjectsListPage />} />
            <Route path="/projects/:projectName" element={<ProjectPage />} />
            <Route path="/projects/:projectName/secrets" element={<SecretsListPage />} />
            <Route path="/projects/:projectName/secrets/:name" element={<SecretPage />} />
            <Route path="/profile" element={<AuthDebugPage />} />
            <Route path="/version" element={<VersionCard />} />
            <Route path="*" element={<Navigate to="/projects" replace />} />
          </Routes>
        </Stack>
      </Box>
    </Box>
  )
}

function App() {
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
