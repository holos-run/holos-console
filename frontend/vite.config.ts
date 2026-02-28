import { defineConfig } from 'vite'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import path from 'path'
import fs from 'fs'

const backendUrl = 'https://localhost:8443'

// Derive OIDC config from backend URL for Vite dev server
const oidcConfig = {
  authority: `${backendUrl}/dex`,
  client_id: 'holos-console',
  redirect_uri: 'https://localhost:5173/pkce/verify',
  post_logout_redirect_uri: 'https://localhost:5173/',
}

const injectOIDCConfig = (): Plugin => ({
  name: 'inject-oidc-config',
  apply: 'serve',
  transformIndexHtml(html) {
    const script = `<script>window.__OIDC_CONFIG__=${JSON.stringify(oidcConfig)};</script>`
    return html.replace('</head>', `${script}</head>`)
  },
})

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    tailwindcss(),
    TanStackRouterVite({ autoCodeSplitting: true }),
    injectOIDCConfig(),
    react(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: path.resolve(__dirname, '../console/dist'),
    emptyOutDir: true,
  },
  server: {
    https: fs.existsSync(path.resolve(__dirname, '../certs/tls.crt'))
      ? {
          cert: fs.readFileSync(path.resolve(__dirname, '../certs/tls.crt')),
          key: fs.readFileSync(path.resolve(__dirname, '../certs/tls.key')),
        }
      : undefined,
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
