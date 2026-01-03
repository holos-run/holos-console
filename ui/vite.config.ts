import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import fs from 'fs'
import path from 'path'

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
  },
})
