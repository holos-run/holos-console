import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

const backendUrl = 'https://localhost:8443'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  base: '/ui/',
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
      // Proxy all requests except /ui/ to the Go backend
      '^(?!/ui/).*': {
        target: backendUrl,
        secure: false,
        changeOrigin: true,
      },
    },
  },
})
