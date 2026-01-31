import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createConnectTransport } from '@connectrpc/connect-web'
import { MemoryRouter } from 'react-router-dom'
import App from './App'

function renderApp(initialEntry: string) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        enabled: false,
        retry: false,
      },
    },
  })
  const transport = createConnectTransport({ baseUrl: 'http://localhost' })

  return render(
    <TransportProvider transport={transport}>
      <QueryClientProvider client={queryClient}>
        <MemoryRouter basename="/ui" initialEntries={[initialEntry]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

/**
 * Override window.matchMedia so that queries matching the given pattern
 * return matches=true while all others return matches=false.
 * Call the returned cleanup function to restore the previous implementation.
 */
function mockMatchMedia(matchPattern: RegExp): () => void {
  const original = window.matchMedia
  window.matchMedia = (query: string) => ({
    matches: matchPattern.test(query),
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  })
  return () => {
    window.matchMedia = original
  }
}

describe('navigation', () => {
  it('links the secrets page from sidebar', async () => {
    renderApp('/ui/version')
    await waitFor(() => {
      const secretsLink = screen.getByRole('link', { name: 'Secrets' })
      expect(secretsLink).toHaveAttribute('href', '/ui/secrets')
    })
  })

  it('links the profile page from sidebar', async () => {
    renderApp('/ui/version')
    await waitFor(() => {
      const profileLink = screen.getByRole('link', { name: 'Profile' })
      expect(profileLink).toHaveAttribute('href', '/ui/profile')
    })
  })

  it('links the version page from sidebar', async () => {
    renderApp('/ui/version')
    await waitFor(() => {
      const versionLink = screen.getByRole('link', { name: 'Version' })
      expect(versionLink).toHaveAttribute('href', '/ui/version')
    })
  })
})

describe('responsive layout', () => {
  it('hides sidebar and shows hamburger on mobile', async () => {
    // Mobile: max-width queries match (below md breakpoint)
    const cleanup = mockMatchMedia(/max-width/)
    try {
      renderApp('/ui/version')
      await waitFor(() => {
        expect(screen.getByLabelText(/open menu/i)).toBeInTheDocument()
      })

      // The permanent sidebar nav should not be rendered
      expect(screen.queryByRole('navigation', { hidden: false })).toBeNull()
    } finally {
      cleanup()
    }
  })

  it('opens temporary drawer when hamburger is clicked on mobile', async () => {
    const user = userEvent.setup()
    const cleanup = mockMatchMedia(/max-width/)
    try {
      renderApp('/ui/version')
      await waitFor(() => {
        expect(screen.getByLabelText(/open menu/i)).toBeInTheDocument()
      })

      await user.click(screen.getByLabelText(/open menu/i))

      // Navigation links should become visible in the drawer
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'Secrets' })).toBeInTheDocument()
      })
    } finally {
      cleanup()
    }
  })

  it('shows permanent sidebar and no hamburger on desktop', async () => {
    // Desktop: default matchMedia returns matches=false (no max-width match)
    renderApp('/ui/version')
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'Secrets' })).toBeInTheDocument()
    })

    // Hamburger should not be present
    expect(screen.queryByLabelText(/open menu/i)).toBeNull()

    // Sidebar nav should be visible with expected links
    expect(screen.getByRole('link', { name: 'Secrets' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Profile' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Version' })).toBeInTheDocument()
  })
})
