import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

const backendUrl = 'https://localhost:8443'

// Derive OIDC config from backend URL for Vite dev server
const oidcConfig = {
  authority: `${backendUrl}/dex`,
  client_id: 'holos-console',
  redirect_uri: 'https://localhost:5173/ui/callback', // Vite dev server
  post_logout_redirect_uri: 'https://localhost:5173/ui',
}

const injectOIDCConfig = (): Plugin => ({
  name: 'inject-oidc-config',
  apply: 'serve', // Only apply during dev server, not during build
  transformIndexHtml(html) {
    const script = `<script>window.__OIDC_CONFIG__=${JSON.stringify(oidcConfig)};</script>`
    return html.replace('</head>', `${script}</head>`)
  },
})

const uiCanonicalRedirect = (): Plugin => ({
  name: 'ui-canonical-redirect',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      // Redirect /ui/ to /ui (canonical path without trailing slash)
      if (req.url === '/ui/') {
        res.statusCode = 301
        res.setHeader('Location', '/ui')
        res.end()
        return
      }
      next()
    })
  },
})

// https://vite.dev/config/
export default defineConfig({
  plugins: [injectOIDCConfig(), uiCanonicalRedirect(), react()],
  base: '/ui',
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
    exclude: ['**/node_modules/**', '**/e2e/**'],
  },
  build: {
    outDir: path.resolve(__dirname, '../console/ui'),
    emptyOutDir: true,
  },
  server: {
    https: {
      cert: fs.readFileSync(path.resolve(__dirname, '../certs/tls.crt')),
      key: fs.readFileSync(path.resolve(__dirname, '../certs/tls.key')),
    },
    proxy: {
      // Proxy ConnectRPC requests to the Go backend.
      '^/holos\\.console\\.v1\\..*': {
        target: backendUrl,
        secure: false,
        changeOrigin: true,
      },
      // Proxy OIDC requests to the embedded Dex provider.
      '/dex': {
        target: backendUrl,
        secure: false,
        changeOrigin: true,
      },
    },
  },
})
