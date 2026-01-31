import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { TransportProvider } from '@connectrpc/connect-query'
import App from './App.tsx'
import { PKCEVerify } from './auth/PKCEVerify.tsx'
import { transport } from './client.ts'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
    },
  },
})

// Handle /pkce/verify outside React Router since it's not under the /ui basename
if (window.location.pathname === '/pkce/verify') {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <PKCEVerify />
    </StrictMode>,
  )
} else {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <TransportProvider transport={transport}>
        <QueryClientProvider client={queryClient}>
          <BrowserRouter basename="/ui">
            <App />
          </BrowserRouter>
        </QueryClientProvider>
      </TransportProvider>
    </StrictMode>,
  )
}
