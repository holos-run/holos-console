import { useEffect, useState } from 'react'
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
import { VersionCard } from './components/VersionCard'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

function App() {
  const [route, setRoute] = useState(() => window.location.hash || '#/')

  useEffect(() => {
    if (!window.location.hash) {
      window.location.hash = '#/'
    }

    const handleHashChange = () => {
      setRoute(window.location.hash || '#/')
    }

    window.addEventListener('hashchange', handleHashChange)
    return () => {
      window.removeEventListener('hashchange', handleHashChange)
    }
  }, [])

  const isVersionPage = route === '#/version'

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
              component="a"
              href="#/"
              selected={!isVersionPage}
            >
              <ListItemText primary="Landing" />
            </ListItemButton>
            <ListItemButton
              component="a"
              href="#/version"
              selected={isVersionPage}
            >
              <ListItemText primary="Version" />
            </ListItemButton>
          </List>
        </Box>
        <Box sx={{ flex: 1, p: 4 }}>
          <Stack spacing={3}>
            {isVersionPage ? (
              <VersionCard />
            ) : (
              <Card variant="outlined">
                <CardContent>
                  <Typography variant="h4" component="h1" gutterBottom>
                    Welcome to Holos Console
                  </Typography>
                  <Typography variant="body1" gutterBottom>
                    This is your landing page. Use the sidebar to explore server
                    details and status.
                  </Typography>
                  <Typography variant="body2" color="text.secondary">
                    Start by opening the Version page to verify the backend
                    connection.
                  </Typography>
                </CardContent>
              </Card>
            )}
          </Stack>
        </Box>
      </Box>
    </ThemeProvider>
  )
}

export default App
