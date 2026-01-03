import { ThemeProvider, createTheme, CssBaseline } from '@mui/material'
import { Container, Typography, Box } from '@mui/material'

const theme = createTheme({
  palette: {
    mode: 'light',
  },
})

function App() {
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <Container maxWidth="md">
        <Box sx={{ my: 4 }}>
          <Typography variant="h4" component="h1" gutterBottom>
            Holos Console
          </Typography>
          <Typography variant="body1">
            Welcome to the Holos Console.
          </Typography>
        </Box>
      </Container>
    </ThemeProvider>
  )
}

export default App
