<!--
Copyright 2026 The Holos Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
-->

# Architecture Decision Records (holos-console)

Most ADRs for the `holos-console` code base live in the companion
[`holos-console-docs`](https://github.com/holos-run/holos-console-docs/tree/main/docs/adrs)
repository. That repository is the canonical home for architecture
decisions that govern the console and its siblings.

This directory (`docs/adrs/` in `holos-run/holos-console`) holds ADRs
that are **colocated with the binary they govern** — in other words,
ADRs for binaries whose source lives in this repository and whose
review boundary matches the `CODEOWNERS` boundary in this repository.

## Index

| ADR | Title | Status | Binary |
|-----|-------|--------|--------|
| [031](031-secret-injection-service.md) | Secret Injection Service — Architecture Pre-Decisions (HOL-674) | Accepted | `holos-secret-injector` |
| [032](032-template-release-crd.md) | TemplateRelease as a sibling CRD (HOL-693) | Accepted | `holos-console` |

## Why colocate?

[ADR 031](031-secret-injection-service.md) is the first ADR colocated
in this repository. It is colocated rather than placed in
`holos-console-docs` because:

1. The binary it governs (`holos-secret-injector`) is built from this
   repository, and the CODEOWNERS for that binary live here.
2. The ADR's "Conventions specific to the injector" section documents
   concrete paths inside this repository (`api/secrets/v1alpha1/`,
   `internal/secretinjector/`, `cmd/secret-injector/`,
   `config/secret-injector/`). Keeping the ADR next to the tree it
   describes minimises cross-repo drift when paths move.
3. A bidirectional copy of the same ADR lives in `holos-console-docs`
   for discoverability from the docs-first reader's entry point; that
   copy is the canonical copy for governance. The two files should
   stay identical at land time. If they diverge, the
   `holos-console-docs` copy is authoritative and this copy is updated
   to match.

Future ADRs that govern cross-binary behaviour, the console's UI, or
storage contracts continue to land in
[`holos-console-docs/docs/adrs/`](https://github.com/holos-run/holos-console-docs/tree/main/docs/adrs).
