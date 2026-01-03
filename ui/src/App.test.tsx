import { render, screen } from '@testing-library/react'
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
        <MemoryRouter basename="/ui/" initialEntries={[initialEntry]}>
          <App />
        </MemoryRouter>
      </QueryClientProvider>
    </TransportProvider>,
  )
}

describe('navigation', () => {
  it('links the landing page with a trailing slash', () => {
    renderApp('/ui/version')
    const landingLink = screen.getByRole('link', { name: 'Landing' })
    expect(landingLink).toHaveAttribute('href', '/ui/')
  })

  it('navigates to the landing page via the landing link', async () => {
    const user = userEvent.setup()
    renderApp('/ui/version')

    await user.click(screen.getByRole('link', { name: 'Landing' }))

    expect(
      screen.getByRole('heading', { name: 'Welcome to Holos Console' }),
    ).toBeInTheDocument()
  })
})
