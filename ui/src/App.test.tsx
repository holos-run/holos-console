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
  it('links the landing page without a trailing slash', async () => {
    renderApp('/ui/version')
    // Wait for AuthProvider async initialization to complete
    await waitFor(() => {
      const landingLink = screen.getByRole('link', { name: 'Landing' })
      expect(landingLink).toHaveAttribute('href', '/ui')
    })
  })

  it('navigates to the landing page via the landing link', async () => {
    const user = userEvent.setup()
    renderApp('/ui/version')

    // Wait for AuthProvider async initialization to complete
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'Landing' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('link', { name: 'Landing' }))

    expect(
      screen.getByRole('heading', { name: 'Welcome to Holos Console' }),
    ).toBeInTheDocument()
  })
})

describe('responsive layout', () => {
  it('hides sidebar and shows hamburger on mobile', async () => {
    // Mobile: max-width queries match (below md breakpoint)
    const cleanup = mockMatchMedia(/max-width/)
    try {
      renderApp('/ui/')
      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Welcome to Holos Console' })).toBeInTheDocument()
      })

      // Hamburger menu button should be visible
      expect(screen.getByLabelText(/open menu/i)).toBeInTheDocument()

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
      renderApp('/ui/')
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
    renderApp('/ui/')
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'Secrets' })).toBeInTheDocument()
    })

    // Hamburger should not be present
    expect(screen.queryByLabelText(/open menu/i)).toBeNull()

    // Sidebar nav should be visible
    expect(screen.getByRole('link', { name: 'Landing' })).toBeInTheDocument()
  })
})
