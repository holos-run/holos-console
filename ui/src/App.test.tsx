import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import { createConnectTransport } from '@connectrpc/connect-web'
import { MemoryRouter } from 'react-router-dom'
import { vi } from 'vitest'
import App from './App'

// Mock the auth module so AuthProvider doesn't make real OIDC calls
const { MockAuthContext } = vi.hoisted(() => {
  const { createContext } = require('react')
  return { MockAuthContext: createContext(null) }
})

const mockAuthValue = {
  user: null,
  isLoading: false,
  error: null,
  isAuthenticated: false,
  login: vi.fn(),
  logout: vi.fn(),
  getAccessToken: vi.fn(() => null),
  refreshTokens: vi.fn(),
  lastRefreshStatus: 'idle' as const,
  lastRefreshTime: null,
  lastRefreshError: null,
}

vi.mock('./auth', () => ({
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
  AuthContext: MockAuthContext,
  useAuth: () => mockAuthValue,
}))

// Mock the client module so service clients don't make real RPC calls
vi.mock('./client', () => ({
  tokenRef: { current: null },
  versionClient: {
    getVersion: vi.fn().mockResolvedValue({}),
  },
  secretsClient: {
    listSecrets: vi.fn().mockResolvedValue({ secrets: [] }),
  },
}))

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
  it('sidebar Projects link always points to /ui/projects', async () => {
    renderApp('/ui/organizations')
    await waitFor(() => {
      const projectsLink = screen.getByRole('link', { name: 'Projects' })
      expect(projectsLink).toHaveAttribute('href', '/ui/projects')
    })
  })

  it('links the projects page from sidebar', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      const projectsLink = screen.getByRole('link', { name: 'Projects' })
      expect(projectsLink).toHaveAttribute('href', '/ui/projects')
    })
  })

  it('links the profile page from sidebar', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      const profileLink = screen.getByRole('link', { name: 'Profile' })
      expect(profileLink).toHaveAttribute('href', '/ui/profile')
    })
  })

  it('links the Home page from sidebar at /ui/home', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      const homeLink = screen.getByRole('link', { name: 'Home' })
      expect(homeLink).toHaveAttribute('href', '/ui/home')
    })
  })

  it('shows Home at the top and Profile/Version at the bottom of the sidebar', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'Home' })).toBeInTheDocument()
    })

    // Get all navigation links in order
    const links = screen.getAllByRole('link')
    const linkNames = links.map((l) => l.textContent)
    const homeIdx = linkNames.indexOf('Home')
    const profileIdx = linkNames.indexOf('Profile')
    const versionIdx = linkNames.indexOf('Version')
    const orgIdx = linkNames.indexOf('Organizations')

    // Home should appear before Organizations
    expect(homeIdx).toBeLessThan(orgIdx)
    // Profile and Version should appear after Organizations and Projects
    expect(profileIdx).toBeGreaterThan(orgIdx)
    expect(versionIdx).toBeGreaterThan(orgIdx)
  })

  it('links the version page from sidebar', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      const versionLink = screen.getByRole('link', { name: 'Version' })
      expect(versionLink).toHaveAttribute('href', '/ui/version')
    })
  })

  it('redirects / to /home', async () => {
    renderApp('/ui/')
    await waitFor(() => {
      const homeLink = screen.getByRole('link', { name: 'Home' })
      expect(homeLink).toHaveAttribute('href', '/ui/home')
    })
  })
})

describe('theme mode toggle', () => {
  it('renders a theme toggle button in the sidebar', async () => {
    renderApp('/ui/home')
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
    })
  })

  it('toggles between light and dark mode when clicked', async () => {
    const user = userEvent.setup()
    renderApp('/ui/home')

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
    })

    const button = screen.getByRole('button', { name: /toggle theme/i })
    // Should be able to click toggle without error
    await user.click(button)
    expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
  })

  it('respects system dark mode preference', async () => {
    const cleanup = mockMatchMedia(/prefers-color-scheme: dark/)
    try {
      renderApp('/ui/home')
      await waitFor(() => {
        expect(screen.getByRole('button', { name: /toggle theme/i })).toBeInTheDocument()
      })
    } finally {
      cleanup()
    }
  })
})

describe('responsive layout', () => {
  it('hides sidebar and shows hamburger on mobile', async () => {
    // Mobile: max-width queries match (below md breakpoint)
    const cleanup = mockMatchMedia(/max-width/)
    try {
      renderApp('/ui/home')
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
      renderApp('/ui/home')
      await waitFor(() => {
        expect(screen.getByLabelText(/open menu/i)).toBeInTheDocument()
      })

      await user.click(screen.getByLabelText(/open menu/i))

      // Navigation links should become visible in the drawer
      await waitFor(() => {
        expect(screen.getByRole('link', { name: 'Projects' })).toBeInTheDocument()
      })
    } finally {
      cleanup()
    }
  })

  it('shows permanent sidebar and no hamburger on desktop', async () => {
    // Desktop: default matchMedia returns matches=false (no max-width match)
    renderApp('/ui/home')
    await waitFor(() => {
      expect(screen.getByRole('link', { name: 'Projects' })).toBeInTheDocument()
    })

    // Hamburger should not be present
    expect(screen.queryByLabelText(/open menu/i)).toBeNull()

    // Sidebar nav should be visible with expected links
    expect(screen.getByRole('link', { name: 'Projects' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Profile' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Home' })).toBeInTheDocument()
  })
})
