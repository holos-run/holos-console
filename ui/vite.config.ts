import { defineConfig } from 'vitest/config'
import type { Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

const backendUrl = 'https://localhost:8443'
const uiTrailingSlashRedirect = (): Plugin => ({
  name: 'ui-trailing-slash-redirect',
  configureServer(server) {
    server.middlewares.use((req, res, next) => {
      if (req.url === '/ui') {
        res.statusCode = 301
        res.setHeader('Location', '/ui/')
        res.end()
        return
      }
      next()
    })
  },
})

// https://vite.dev/config/
export default defineConfig({
  plugins: [uiTrailingSlashRedirect(), react()],
  base: '/ui/',
  test: {
    environment: 'jsdom',
    setupFiles: './src/test/setup.ts',
    globals: true,
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
    },
  },
})
