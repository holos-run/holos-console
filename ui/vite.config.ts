import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

const backendUrl = 'https://localhost:8443'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
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
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (req.url === '/ui') {
          res.statusCode = 302
          res.setHeader('Location', '/ui/')
          res.end()
          return
        }
        next()
      })
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
