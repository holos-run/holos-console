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
import { Link, Navigate, Route, Routes, useLocation } from 'react-router-dom'
import { VersionCard } from './components/VersionCard'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

function App() {
  const location = useLocation()
  const isVersionPage = location.pathname.startsWith('/version')

  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
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
              selected={!isVersionPage}
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
              <Route path="*" element={<Navigate to="/" replace />} />
            </Routes>
          </Stack>
        </Box>
      </Box>
    </ThemeProvider>
  )
}

export default App
