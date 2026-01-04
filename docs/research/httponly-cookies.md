# HttpOnly Cookies: A Platform Engineer's Guide

This document explains HttpOnly cookies for platform engineers who work primarily on backend
systems and Kubernetes infrastructure, but need to understand frontend authentication patterns.

## What is HttpOnly?

HttpOnly is a flag set on cookies by the server. When a cookie has this flag:

- **The browser will send the cookie** with every HTTP request to the matching domain
- **JavaScript cannot read the cookie** via `document.cookie`
- **JavaScript cannot modify or delete the cookie** directly

This is a security feature to prevent XSS (Cross-Site Scripting) attacks from stealing
session cookies.

## How Cookies Are Set

When a server responds to a request, it can include a `Set-Cookie` header:

```http
Set-Cookie: _oauth2_proxy=<encrypted-session>; Path=/; HttpOnly; Secure; SameSite=Lax
```

Breaking this down:

| Attribute | Meaning |
|-----------|---------|
| `_oauth2_proxy=<value>` | Cookie name and value |
| `Path=/` | Cookie sent for all paths |
| `HttpOnly` | JavaScript cannot access |
| `Secure` | Only sent over HTTPS |
| `SameSite=Lax` | CSRF protection (sent on navigation, not cross-site POST) |

## Why This Matters for Frontend Development

### The Problem

In a typical SPA authentication flow, the frontend needs to know:

1. Is the user authenticated?
2. Who is the user (email, name, roles)?
3. When does the session expire?

If tokens are stored in `localStorage` or `sessionStorage`, JavaScript can read this
information directly. But with HttpOnly cookies, the frontend is "blind" to the cookie.

### What JavaScript CAN Do

```javascript
// Send cookies with requests (credentials: "include")
fetch('/api/data', { credentials: 'include' })
  .then(response => response.json())

// The browser automatically includes HttpOnly cookies
// JavaScript doesn't need to read or attach them manually
```

### What JavaScript CANNOT Do

```javascript
// This will NOT show HttpOnly cookies
console.log(document.cookie)
// Output: "other_cookie=value" (only non-HttpOnly cookies)

// This will NOT work for HttpOnly cookies
document.cookie = "_oauth2_proxy=; expires=Thu, 01 Jan 1970 00:00:00 GMT"
// HttpOnly cookies can only be cleared by the server
```

## Implications for ConnectRPC

[ConnectRPC](https://connectrpc.com/) is a modern RPC framework that uses `fetch` under
the hood for browser clients.

### Configuring Credentials

To send cookies with ConnectRPC requests, configure the transport:

```typescript
import { createConnectTransport } from "@connectrpc/connect-web";

const transport = createConnectTransport({
  baseUrl: "https://api.example.com",
  // This is required to send HttpOnly cookies
  fetch: (input, init) => fetch(input, { ...init, credentials: "include" }),
});
```

### How It Works

1. Browser makes RPC request with `credentials: "include"`
2. Browser automatically attaches all cookies for the domain (including HttpOnly)
3. Server receives request with `Cookie: _oauth2_proxy=...` header
4. Server validates session and processes request
5. JavaScript never sees the cookie value

### Authentication Errors

When a session expires or is invalid, the server returns an error (typically 401 or 403).
Your ConnectRPC error handling should detect this and redirect to login:

```typescript
try {
  const response = await client.someMethod(request);
} catch (err) {
  if (err instanceof ConnectError && err.code === Code.Unauthenticated) {
    // Redirect to login
    window.location.href = '/oauth2/start';
  }
  throw err;
}
```

## Implications for TanStack Query

[TanStack Query](https://tanstack.com/query) (formerly React Query) is a data-fetching
library. It doesn't handle fetching directly—it wraps your fetch functions.

### Key Point

TanStack Query is agnostic to authentication. It just calls whatever function you provide.
The authentication concern is in your fetch function, not TanStack Query itself.

### Example with Credentials

```typescript
import { useQuery } from '@tanstack/react-query';

function useUserProfile() {
  return useQuery({
    queryKey: ['user', 'profile'],
    queryFn: async () => {
      const response = await fetch('/api/profile', {
        credentials: 'include', // Required for HttpOnly cookies
      });
      if (!response.ok) {
        if (response.status === 401) {
          // Handle unauthenticated - redirect to login
          window.location.href = '/oauth2/start';
        }
        throw new Error('Failed to fetch profile');
      }
      return response.json();
    },
  });
}
```

### With ConnectRPC and TanStack Query

If using ConnectRPC with TanStack Query, the transport handles credentials:

```typescript
import { useQuery } from '@tanstack/react-query';
import { createClient } from './gen/api/v1/service-UserService_connectquery';

const client = createClient(transport); // transport configured with credentials

function useUserProfile() {
  return useQuery({
    queryKey: ['user', 'profile'],
    queryFn: () => client.getProfile({}),
  });
}
```

## Detecting Authentication State

Since JavaScript can't read HttpOnly cookies, how does the frontend know if the user is
authenticated?

### Pattern 1: Backend Endpoint (Recommended)

Create an endpoint that returns the current user's info:

```go
// Backend: /api/userinfo
func handleUserInfo(w http.ResponseWriter, r *http.Request) {
    // oauth2-proxy sets these headers for authenticated requests
    user := r.Header.Get("X-Forwarded-User")
    email := r.Header.Get("X-Forwarded-Email")

    if user == "" {
        http.Error(w, "Not authenticated", http.StatusUnauthorized)
        return
    }

    json.NewEncoder(w).Encode(map[string]string{
        "user":  user,
        "email": email,
    })
}
```

```typescript
// Frontend: Check auth on app load
async function checkAuth(): Promise<User | null> {
  try {
    const response = await fetch('/api/userinfo', { credentials: 'include' });
    if (response.ok) {
      return response.json();
    }
    return null; // Not authenticated
  } catch {
    return null;
  }
}
```

### Pattern 2: Server-Injected Configuration

The backend can inject auth state into the HTML:

```html
<script>
  window.__AUTH_STATE__ = {"authenticated": true, "user": "alice@example.com"};
</script>
```

This is set by the backend when serving `index.html`, based on whether the request has a
valid session cookie.

### Pattern 3: Dual-Cookie Approach

Some systems use two cookies:
- HttpOnly cookie for the session (secure)
- Non-HttpOnly cookie as a flag (readable by JS)

oauth2-proxy doesn't support this natively, but you could add middleware.

## Common Mistakes

### Mistake 1: Trying to Read HttpOnly Cookies

```typescript
// WRONG: This won't work
if (document.cookie.includes('_oauth2_proxy')) {
  // User is authenticated
}
```

### Mistake 2: Forgetting Credentials

```typescript
// WRONG: Cookies won't be sent
fetch('/api/data')

// RIGHT: Cookies will be sent
fetch('/api/data', { credentials: 'include' })
```

### Mistake 3: CORS Misconfiguration

For cross-origin requests with credentials, the server must:

```go
// Backend CORS configuration
w.Header().Set("Access-Control-Allow-Origin", "https://app.example.com") // NOT "*"
w.Header().Set("Access-Control-Allow-Credentials", "true")
```

Using `*` for `Access-Control-Allow-Origin` is not allowed with credentials.

### Mistake 4: Client-Side Logout

```typescript
// WRONG: This won't clear HttpOnly cookies
document.cookie = "_oauth2_proxy=; expires=Thu, 01 Jan 1970 00:00:00 GMT";

// RIGHT: Redirect to server-side logout endpoint
window.location.href = '/oauth2/sign_out';
```

## oauth2-proxy Specific Notes

### Cookie Configuration

oauth2-proxy's session cookie is HttpOnly by default:

```bash
# Default (secure, recommended)
--cookie-httponly=true

# Insecure (allows JavaScript access, NOT recommended)
--cookie-httponly=false
```

### Forwarded Headers

When oauth2-proxy authenticates a request, it adds headers:

| Header | Content |
|--------|---------|
| `X-Forwarded-User` | User identifier (email or subject) |
| `X-Forwarded-Email` | User's email address |
| `X-Forwarded-Access-Token` | Access token (if configured) |

Your backend reads these headers to identify the user—the frontend never sees them.

### Login and Logout URLs

| Endpoint | Purpose |
|----------|---------|
| `/oauth2/start` | Initiates OIDC login flow |
| `/oauth2/callback` | OIDC redirect URI (handled by proxy) |
| `/oauth2/sign_out` | Clears session and optionally logs out of IdP |
| `/oauth2/userinfo` | Returns user info (if enabled) |

## Summary

| Topic | Key Takeaway |
|-------|--------------|
| **HttpOnly** | Browser sends cookie automatically; JavaScript can't read it |
| **ConnectRPC** | Use `credentials: "include"` in transport configuration |
| **TanStack Query** | Not auth-aware; ensure your fetch functions include credentials |
| **Detecting Auth** | Use a backend endpoint like `/api/userinfo`, not cookie inspection |
| **Logout** | Must redirect to server endpoint; can't clear HttpOnly cookies client-side |
| **CORS** | Must specify exact origin (not `*`) when using credentials |

## References

- [MDN: Using HTTP cookies](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Cookies)
- [OWASP: HttpOnly](https://owasp.org/www-community/HttpOnly)
- [ConnectRPC: Choosing a Protocol](https://connectrpc.com/docs/web/choosing-a-protocol/)
- [TanStack Query: Authentication](https://tanstack.com/query/latest/docs/framework/react/guides/authentication)
- [oauth2-proxy: Session Storage](https://oauth2-proxy.github.io/oauth2-proxy/configuration/session_storage/)
- [Fetch API: credentials](https://developer.mozilla.org/en-US/docs/Web/API/fetch#credentials)
