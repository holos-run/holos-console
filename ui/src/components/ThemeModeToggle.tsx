import { IconButton } from '@mui/material'
import { useColorScheme } from '@mui/material/styles'
import LightModeIcon from '@mui/icons-material/LightMode'
import DarkModeIcon from '@mui/icons-material/DarkMode'

export function ThemeModeToggle() {
  const { mode, setMode } = useColorScheme()

  return (
    <IconButton
      aria-label="toggle theme"
      onClick={() => setMode(mode === 'dark' ? 'light' : 'dark')}
      size="small"
    >
      {mode === 'dark' ? <LightModeIcon /> : <DarkModeIcon />}
    </IconButton>
  )
}
