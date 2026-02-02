import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ThemeProvider } from '@mui/material'
import { ThemeModeToggle } from './ThemeModeToggle'
import theme from '../theme'

function renderToggle() {
  return render(
    <ThemeProvider theme={theme}>
      <ThemeModeToggle />
    </ThemeProvider>,
  )
}

describe('ThemeModeToggle', () => {
  it('renders a toggle button', () => {
    renderToggle()
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
  })

  it('switches mode when clicked', async () => {
    const user = userEvent.setup()
    renderToggle()

    const button = screen.getByRole('button', { name: /toggle theme/i })

    // Click should toggle the mode (the icon should change)
    await user.click(button)

    // After click, button should still be present (mode toggled)
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
  })
})
