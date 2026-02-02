import { createTheme } from '@mui/material'

const theme = createTheme({
  cssVariables: true,
  colorSchemes: { light: true, dark: true },
  colorSchemeSelector: 'data-color-scheme',
})

export default theme
