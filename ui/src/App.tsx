import { useState } from 'react'
import {
  ThemeProvider,
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
  Select,
  MenuItem,
  CircularProgress,
  FormControl,
} from '@mui/material'
import MenuIcon from '@mui/icons-material/Menu'
import {
  Link,
  Navigate,
  Route,
  Routes,
  useLocation,
  useNavigate,
} from 'react-router-dom'
import { VersionCard } from './components/VersionCard'
import { AuthDebugPage } from './components/AuthDebugPage'
import { useVersion } from './queries/version'
import { SecretsListPage } from './components/SecretsListPage'
import { SecretPage } from './components/SecretPage'
import { ProjectsListPage } from './components/ProjectsListPage'
import { ProjectPage } from './components/ProjectPage'
import { OrganizationsListPage } from './components/OrganizationsListPage'
import { OrganizationPage } from './components/OrganizationPage'
import { AuthProvider } from './auth'
import { OrgProvider, useOrg } from './OrgProvider'
import { ThemeModeToggle } from './components/ThemeModeToggle'
import theme from './theme'

const DRAWER_WIDTH = 240

function OrgPicker() {
  const { organizations, selectedOrg, setSelectedOrg, isLoading } = useOrg()
  const navigate = useNavigate()

  if (isLoading) {
    return (
      <Box sx={{ px: 2, py: 1, display: 'flex', justifyContent: 'center' }}>
        <CircularProgress size={20} />
      </Box>
    )
  }

  if (organizations.length === 0) {
    return null
  }

  return (
    <FormControl fullWidth size="small" sx={{ px: 2, py: 1 }}>
      <Select
        value={selectedOrg || '__all__'}
        onChange={(e) => {
          const value = e.target.value
          if (value === '__all__') {
            setSelectedOrg(null)
            navigate('/organizations')
          } else {
            setSelectedOrg(value)
            navigate('/projects')
          }
        }}
        displayEmpty
        sx={{ fontSize: '0.875rem' }}
      >
        <MenuItem value="__all__">All Organizations</MenuItem>
        {organizations.map((org) => (
          <MenuItem key={org.name} value={org.name}>
            {org.displayName || org.name}
          </MenuItem>
        ))}
      </Select>
    </FormControl>
  )
}

function SidebarContent({ onNavigate }: { onNavigate?: () => void }) {
  const location = useLocation()
  const isOrganizationsPage = location.pathname.startsWith('/organizations')
  const isProjectsPage = location.pathname.startsWith('/projects') || location.pathname.includes('/projects')
  const isProfilePage = location.pathname.startsWith('/profile')
  const isHomePage = location.pathname.startsWith('/home')
  const isVersionPage = location.pathname.startsWith('/version')
  const { data: versionData } = useVersion()

  return (
    <Box sx={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <Box sx={{ p: 3 }}>
        <Typography variant="h6" component="div">
          Holos Console
        </Typography>
        {versionData?.version && (
          <Typography variant="caption" color="text.secondary">
            {versionData.version}
          </Typography>
        )}
      </Box>
      <Divider />
      <OrgPicker />
      <Divider />
      <List sx={{ px: 1 }}>
        <ListItemButton
          component={Link}
          to="/home"
          selected={isHomePage}
          onClick={onNavigate}
        >
          <ListItemText primary="Home" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/organizations"
          selected={isOrganizationsPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Organizations" />
        </ListItemButton>
        <ListItemButton
          component={Link}
          to="/projects"
          selected={isProjectsPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Projects" />
        </ListItemButton>
      </List>
      <Box sx={{ flexGrow: 1 }} />
      <Divider />
      <List sx={{ px: 1 }}>
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
          to="/version"
          selected={isVersionPage}
          onClick={onNavigate}
        >
          <ListItemText primary="Version" />
        </ListItemButton>
      </List>
      <Divider />
      <Box sx={{ px: 2, py: 1, display: 'flex', justifyContent: 'flex-end' }}>
        <ThemeModeToggle />
      </Box>
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
            <Route path="/" element={<Navigate to="/home" replace />} />
            <Route path="/organizations" element={<OrganizationsListPage />} />
            <Route path="/organizations/:organizationName" element={<OrganizationPage />} />
            <Route path="/projects" element={<ProjectsListPage />} />
            <Route path="/projects/:projectName" element={<ProjectPage />} />
            <Route path="/projects/:projectName/secrets" element={<SecretsListPage />} />
            <Route path="/projects/:projectName/secrets/:name" element={<SecretPage />} />
            <Route path="/profile" element={<AuthDebugPage />} />
            <Route path="/version" element={<VersionCard />} />
            <Route path="/home" element={<AuthDebugPage />} />
            <Route path="*" element={<Navigate to="/home" replace />} />
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
        <OrgProvider>
          <MainLayout />
        </OrgProvider>
      </AuthProvider>
    </ThemeProvider>
  )
}

export default App
