# Development Server Configuration

This document explains how to run the frontend and backend together during development.

## Overview

During development, two servers run simultaneously:

1. **Go backend** (`make run`) - Serves the API at `https://localhost:8443`
2. **Vite dev server** (`make dev`) - Serves the React frontend at `https://localhost:5173/ui/` with hot module reloading

The Vite dev server proxies all non-`/ui/` requests to the Go backend, allowing the frontend to make RPC calls seamlessly.

## Starting Development Servers

In one terminal, start the Go backend:

```bash
make run
```

In another terminal, start the Vite dev server:

```bash
make dev
```

Open your browser to `https://localhost:5173/ui/` to access the frontend with hot reloading.

## How the Proxy Works

The Vite configuration in `ui/vite.config.ts` proxies requests:

- Requests to `/ui/*` are handled by Vite (React app with HMR)
- All other requests (e.g., `/holos.console.v1.*` RPC calls) are proxied to `https://localhost:8443`

This allows the frontend to call backend RPCs using relative URLs (e.g., `/holos.console.v1.VersionService/GetVersion`).

## TLS Certificates

Both servers use the same mkcert-generated certificates from `certs/`:

- `certs/tls.crt` - TLS certificate
- `certs/tls.key` - TLS private key

Generate certificates with:

```bash
make certs
```

## Production Build

In production, the React app is built and embedded in the Go binary. The Go server serves both the API and the static frontend files at `/ui/`. No separate dev server is needed.

Build the production binary:

```bash
make generate  # Builds frontend and generates code
make build     # Builds Go binary with embedded frontend
make run       # Runs the server
```
