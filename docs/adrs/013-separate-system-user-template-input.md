# ADR 013: Separate System and User Input Trust Boundary in CUE Templates

## Status

Accepted

## Context

The current CUE deployment template interface exposes a single `input: #Input`
field that mixes values from two distinct sources with different trust levels:

```cue
#Input: {
    name:      string & =~"^[a-z][a-z0-9-]*$"  // user-provided via deployment form
    image:     string                            // user-provided via deployment form
    tag:       string                            // user-provided via deployment form
    project:   string                            // resolved by backend from session
    namespace: string                            // resolved by backend from K8s resolver
    command?: [...string]                        // user-provided via deployment form
    args?:    [...string]                        // user-provided via deployment form
    env:      [...#EnvVar] | *[]                 // user-provided via deployment form
    port:     int & >0 & <=65535 | *8080         // user-provided via deployment form
}
```

The `project` and `namespace` fields are set exclusively by the backend from the
authenticated session and the namespace resolver (`Resolver.ProjectNamespace()`).
They are not supplied by the user and cannot be forged by the user in a
well-implemented API handler. The remaining fields (`name`, `image`, `tag`,
`command`, `args`, `env`, `port`) originate from the user via the deployment
creation form or the template preview editor.

This mixing creates two problems:

1. **No trust boundary for template authors.** A template author cannot
   distinguish values the backend guarantees from values the user supplied.
   Template logic that relies on `input.namespace` being a verified project
   namespace currently works by convention but is not architecturally enforced.
   As templates become more expressive and support security-sensitive annotations
   or role bindings, this ambiguity becomes a correctness and security risk.

2. **No access to authenticated user identity.** Templates have no way to
   embed deployer identity in resources. Use cases like annotating resources with
   `console.holos.run/deployer-email` from the OIDC `email` claim, or scoping a
   `Role` to the deploying user's subject, are impossible with the current
   interface. The backend possesses the verified OIDC ID token claims but has no
   channel to pass them to the template.

The `RenderDeploymentTemplate` preview RPC currently accepts a single
`cue_input` field (raw CUE source for the entire `input` struct), which allows
the preview caller to freely compose both system and user values.

## Decision

Split the template input into two distinct top-level CUE fields:

```cue
system: #SystemInput   // trusted — set exclusively by the console backend
input:  #Input         // user-provided deployment parameters
```

### `#SystemInput` Schema

```cue
#SystemInput: {
    project:   string    // parent project name, resolved from authenticated session
    namespace: string    // Kubernetes namespace, resolved via Resolver.ProjectNamespace()
    claims:    #Claims   // OIDC ID token claims from the authenticated user's JWT
}
```

### `#Claims` Schema

```cue
#Claims: {
    iss:            string       // token issuer URL
    sub:            string       // subject identifier (unique per user per issuer)
    iat:            int          // issued-at time (Unix epoch seconds)
    exp:            int          // expiry time (Unix epoch seconds)
    email:          string       // user's email address
    email_verified: bool         // whether the issuer has verified the email
    name?:          string       // human-readable display name (optional)
    groups?:        [...string]  // group memberships (optional, matches --roles-claim)
    ...                          // open struct: allows provider-specific claims
}
```

The open struct (`...`) is intentional. Different OIDC providers include
provider-specific claims (e.g., Dex adds `federated_claims`). Closing the struct
would require maintaining an allowlist of every claim any supported provider
might send.

### `#Input` Schema (after split)

The `project` and `namespace` fields move out of `#Input` into `#SystemInput`.
`#Input` retains only user-controlled fields:

```cue
#Input: {
    name:      string & =~"^[a-z][a-z0-9-]*$"
    image:     string
    tag:       string
    command?: [...string]
    args?:    [...string]
    env:      [...#EnvVar] | *[]
    port:     int & >0 & <=65535 | *8080
}
```

### RenderDeploymentTemplate Preview RPC

The `RenderDeploymentTemplate` RPC gains a separate `cue_system_input` field
alongside the existing `cue_input` field:

```proto
message RenderDeploymentTemplateRequest {
    string cue_template     = 1;  // raw CUE template source
    string cue_input        = 2;  // CUE source for the `input` field (user parameters)
    string cue_system_input = 3;  // CUE source for the `system` field (trusted context)
}
```

The frontend template editor preview splits into two input areas: one for
`input` (user-controlled parameters) and one for `system` (backend-injected
context), giving template authors a way to simulate both sides during
development.

### Backend Injection

At deployment create/update time, the backend constructs `#SystemInput` from:
- `project`: the project name from the API request, validated against the
  authenticated session's project access.
- `namespace`: resolved from the project name via `Resolver.ProjectNamespace()`.
- `claims`: the full `map[string]interface{}` of claims extracted from the
  verified OIDC ID token in the request context.

The `system` field is filled before CUE evaluation using the same
`FillPath("system")` mechanism as the existing `FillPath("input")`.

## Consequences

### Positive

- **Clear trust boundary.** Template authors can rely on `system.namespace` and
  `system.project` as backend-verified values and treat `input.*` as
  user-provided values requiring appropriate CUE constraints.

- **Deployer identity in templates.** Templates can annotate resources with
  deployer identity, enabling audit trails and identity-based access:
  ```cue
  metadata: annotations: {
      "console.holos.run/deployer-email": system.claims.email
      "console.holos.run/deployer-sub":   system.claims.sub
  }
  ```

- **Standard OIDC claim validation.** The `#Claims` schema validates well-known
  OIDC claims (`iss`, `sub`, `iat`, `exp`, `email`, `email_verified`) at CUE
  evaluation time. A missing or malformed claim causes a CUE evaluation error
  before any Kubernetes apply, catching token structure issues early.

- **Foundation for platform input.** The two-field pattern (`system`, `input`)
  establishes the convention for a future third field (`platform: #PlatformInput`)
  for platform-mandated configuration, consistent with the direction indicated in
  ADR 012 and the planned extensions section of the CUE template guide.

### Negative

- **Template migration required.** All existing templates must be updated:
  - Replace `input.project` → `system.project`
  - Replace `input.namespace` → `system.namespace`
  - Add `system: #SystemInput` declaration
  - Remove `project` and `namespace` from `#Input`

  Since the product is pre-release there is no user-facing migration burden, but
  the default template and any test fixtures must be updated as part of this
  change.

- **Preview editor split.** The template editor must render two input panels,
  slightly increasing UI complexity. The benefit — template authors can test
  system input separately from user input — justifies this cost.

- **`cue_system_input` required for preview.** Callers of
  `RenderDeploymentTemplate` must provide a `cue_system_input` to exercise
  templates that reference `system.*`. A sensible default (empty struct or a
  fixture with placeholder claims) can be pre-populated in the editor.

### Risks

- **Open `#Claims` struct.** The open struct (`...`) allows provider-specific
  claims through. If a template inadvertently references an optional
  provider-specific claim without marking it optional in CUE, evaluation will
  fail on providers that omit that claim. Template authors should mark
  provider-specific claim references optional (e.g., `system.claims.realm_roles?`).

- **Claims freshness.** The claims embedded in the CUE context are from the
  bearer token in the API request. They are valid at request time but may be
  stale if the user's identity changes between token issuance and use. This is
  inherent to JWT-based authentication and is not a new risk introduced by this
  change.

## References

- [ADR 012: Structured Resource Output for CUE Templates](012-structured-resource-output.md)
- [CUE Template Guide](../cue-template-guide.md) — current `#Input` schema and render pipeline
- [Issue #446](https://github.com/holos-run/holos-console/issues/446) — this decision
- [Issue #445](https://github.com/holos-run/holos-console/issues/445) — parent: separate system and user input in CUE template interface
