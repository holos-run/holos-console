# ADR 014 Analysis: Configuration Management Resource Schema

## Purpose

This document is a principal platform engineering review of [ADR
014](014-config-management-resource-schema.md) and its companion [ADR
015](015-config-management-rbac-levels.md). It evaluates the design against the
needs of a rapidly growing engineering organization (300 today, scaling to
thousands) at a Nasdaq-100 company, with particular attention to:

- Multi-stakeholder collaboration on ResourceSets
- AI-enabled software engineering (agentic workloads)
- API callers (ConnectRPC) and tool integrations (MCP)
- Auditing, logging, and compliance (SOC2, ISO 27001)

## Executive Summary

ADR 014 establishes a strong foundation. The Go-struct-as-source-of-truth
approach, TypeMeta-based version discrimination, CUE unification for
hierarchical template composition, and the platformResources/projectResources
split are sound architectural choices. The design aligns well with patterns
proven at scale by Google Cloud (Organization → Folder → Project), Crossplane
(Compositions vs. Claims), and the emerging "layered ownership" model used
across the CNCF ecosystem (ArgoCD ApplicationSets, Kustomize overlays, Helm
value hierarchies).

However, several gaps exist that would surface under the pressure of a
thousand-engineer organization, agentic workloads, and enterprise compliance
requirements. The sections below identify each gap, explain why it matters,
cite precedent from industry, and describe how the existing design can be
extended to close the gap.

## Strengths

### 1. CUE unification is the right composition model for multi-stakeholder config

Unlike Helm's last-writer-wins value overrides or Kustomize's strategic merge
patches, CUE unification is commutative, associative, and conflict-detecting.
When a security engineer requires `replicas: >=3` at the org level and a
product engineer sets `replicas: 5` at the project level, CUE proves
compatibility. If the product engineer sets `replicas: 1`, CUE rejects it at
evaluation time — before any Kubernetes API call. No other mainstream
configuration language offers this property.

Marcel van Lohuizen (CUE creator, ex-Google, worked on Borg configuration)
designed CUE specifically to address the problems he saw with GCL and Jsonnet in
hierarchical configuration at Google scale. ADR 014's use of unification for
cross-level template composition is precisely the use case CUE was built for.

**Precedent**: Dagger (dagger.io) initially built its CI/CD pipeline system
entirely on CUE (v0.1–0.2, 2022–2023), validating that CUE's type system works
for multi-stakeholder configuration. Dagger later moved to Go/Python/TypeScript
SDKs for end-user authoring (v0.3+, 2023), but retained CUE internally for
schema validation — a lesson this design already incorporates by making Go
structs the authoring boundary and using CUE as the evaluation engine.

### 2. The hierarchy depth limit (3 folder levels) is well-calibrated

Google Cloud IAM supports up to 10 levels of folder nesting but recommends
against more than 3–4 in practice. AWS Organizations limits OUs to 5 levels.
Azure Management Groups allows 6 levels. In every case, the operational
experience is that deeper hierarchies become difficult to reason about and debug.
ADR 014's choice of 3 folder levels matches the empirical sweet spot.

### 3. platformResources/projectResources split creates a clear RBAC boundary

This is analogous to Crossplane's separation between `Compositions` (platform
team authored) and `Claims` (application team authored). The Crossplane model has
been adopted at Deutsche Bahn, Grafana Labs, and Upbound to manage exactly this
boundary — platform teams define _how_ infrastructure is provisioned, application
teams declare _what_ they need. ADR 014's split serves the same purpose for
Kubernetes resource generation.

### 4. Constraint flow is one-directional

Higher levels constrain lower levels, not the reverse. This matches the policy
inheritance model in Google Cloud IAM (organization policies flow down), OPA
Gatekeeper (ConstraintTemplates apply to all matching resources), and Kyverno
(ClusterPolicy applies cluster-wide, Policy applies per-namespace). The design
correctly identifies that policy is hierarchical while data access is
need-to-know (ADR 007 vs. ADR 015 cascade distinction).

## Gaps and Recommendations

### Gap 1: No folderInput for intermediate stakeholders

**The problem.** ADR 014 defines `platformInput` (set by platform engineers /
system) and `projectInput` (set by product engineers). But the hierarchy has
three distinct stakeholder levels: org, folder, and project. SRE teams operating
at folder levels have no dedicated input channel — they can only write templates
that reference `platform.*` and `input.*`, but cannot receive folder-specific
parameters.

**Why it matters.** Consider an SRE team managing the "payments" folder. They
want to configure a Datadog dashboard URL, an on-call PagerDuty service key, or
a team-specific Istio traffic policy — values that are neither platform-global
nor project-specific. Without `folderInput`, these values must be hardcoded in
the folder template or smuggled through annotations.

**Precedent.** Humanitec's Platform Orchestrator uses a four-level input model
(Organization → Application → Environment → Workload) where each level can
inject context. Netflix's Archaius configuration system (open-sourced at
github.com/Netflix/archaius) provides environment-specific overrides with
inheritance: global defaults → region → cluster → instance.

**Recommendation.** The ADR acknowledges this gap (Decision 4: "Folder type and
`folderInput` are deferred to validate extensibility in `v1alpha2`"). This is a
reasonable staging decision. When implementing `v1alpha2`, consider a
`FolderInput` type that carries folder-scoped key-value pairs, exposed to
templates as `folder.*` alongside `platform.*` and `input.*`. The CUE evaluation
order would be: `platform` → `folder` (each level in the hierarchy) → `input`.

### Gap 2: No explicit collaboration model for shared templates

**The problem.** The design describes single-author ownership at each hierarchy
level. It does not address how multiple stakeholders collaborate on a single
template at the same level. In a 1000+ engineer org, a folder-level template
for "payments" might need contributions from both the SRE team (health checks,
resource limits) and the security team (network policies, pod security context).

**Why it matters.** Without a mechanism for multiple templates at the same
level, teams either (a) crowd into a single template file with merge conflicts,
(b) create sibling folders to split responsibility (distorting the hierarchy), or
(c) rely on informal coordination.

**Precedent.** Kustomize addresses this with multiple overlay directories that
are merged. ArgoCD ApplicationSets allow multiple generators to contribute
Applications to the same cluster. Crossplane allows multiple Compositions to
match the same Claim type with different match labels.

**The design already supports this.** Decision 8 states that "the console
collects templates from every level in the hierarchy and unifies them into a
single CUE value." If multiple templates can exist at a single folder level, CUE
unification naturally composes them — an SRE's template adds health check
constraints while a security engineer's template adds NetworkPolicy resources,
and unification merges both. ADR 015's cascade table already supports multiple
Editors at a given level.

**Recommendation.** Explicitly document that each hierarchy level supports
multiple templates and that CUE unification composes them. Define ordering
semantics (or confirm that order is irrelevant due to CUE commutativity).
Consider adding an optional `purpose` or `owner-team` annotation on templates to
help teams understand which template does what when debugging evaluation errors.

### Gap 3: API callers (ConnectRPC) and tool integrations (MCP) are under-specified

**The problem.** The ADR focuses on the CUE evaluation model and the
resource schema but does not address how external API callers interact with
ResourceSets. There are three important caller categories:

1. **ConnectRPC API callers** — CI/CD pipelines, custom tooling, or dashboards
   that create/update deployments programmatically through the existing
   ConnectRPC service.
2. **MCP (Model Context Protocol) tool servers** — AI agents that discover and
   invoke console operations through MCP's tool discovery protocol.
3. **AI coding agents** (Claude Code, GitHub Copilot, etc.) — Agents that
   modify CUE templates in git, which are then applied through the console.

**Why it matters.** At a Nasdaq-100 company scaling to thousands of engineers,
the human-in-the-browser path will quickly become the minority of interactions.
CI/CD systems will call ConnectRPC to deploy on merge. AI agents will modify
templates as part of feature branches. MCP servers will expose console
capabilities to agent orchestrators.

**How the design already supports this.** This is fundamentally an RPC layer
concern, not a resource schema concern — and the design handles it correctly:

- The RBAC model in ADR 015 is principal-based (email + OIDC roles), not
  session-based. Any caller that presents a valid Bearer token — whether a
  browser session, a CI/CD service account, or an AI agent with a
  service-account token — goes through the same `LazyAuthInterceptor` in
  `console/rpc/auth.go` and receives the same RBAC evaluation.
- ConnectRPC callers already work. The proto messages define the RPC contract;
  the Go API types define the template evaluation contract. A CI/CD pipeline
  calls `CreateDeployment` with `ProjectInput` values and the backend fills
  `PlatformInput` from the authenticated context — identical to the browser
  path.
- MCP tool servers would wrap ConnectRPC calls. An MCP server exposes console
  operations as MCP tools (e.g., `create_deployment`, `render_template`). The
  MCP server authenticates to the console using a service account token, and
  the console's RBAC evaluates the service account's grants. The resource
  schema is transparent to MCP — it only affects what happens inside the CUE
  evaluator.
- AI agents modifying templates in git interact with the resource schema
  indirectly. The agent edits CUE source in a feature branch, a PR is created,
  CI runs `RenderDeploymentTemplate` (the preview RPC) to validate the
  template, and a human approves. The schema's `TypeMeta` version
  discrimination ensures the renderer handles the template correctly regardless
  of who authored it.

**Recommendation.** The resource schema does not need changes for these use
cases, but the following would strengthen API caller support:

- **Service account identity in PlatformInput.Claims**: Ensure the `Claims`
  struct works for service accounts (which may lack `email` or have a synthetic
  email). The open struct (`...`) in `#Claims` already allows provider-specific
  claims, but the required `email` field (non-optional in ADR 013) could be
  problematic for service accounts that authenticate via client credentials
  rather than OIDC user flows.
- **Rate limiting and quota per principal**: At 1000+ engineers with AI agents,
  the console API could see 10-100x the request volume of a human-only
  organization. The RBAC model authorizes access but does not address capacity.
  Consider per-principal rate limits at the RPC layer.
- **Idempotency keys for agent retries**: AI agents are retry-happy. Ensure
  that `CreateDeployment` and template mutation RPCs are idempotent or accept an
  idempotency key to prevent duplicate resource creation.

### Gap 4: Template change attribution and audit trail

**The problem.** ADR 014 defines `PlatformInput.Claims` which embeds the
deployer's identity in rendered resources (e.g.,
`console.holos.run/deployer-email`). But the design does not address audit
logging for template authoring itself — who created or modified a template, when,
and what changed.

**Why it matters for compliance.** SOC2 CC6.1 (Logical and Physical Access
Controls) requires that access to configuration is restricted to authorized
individuals and that changes are auditable. SOC2 CC7.2 (System Monitoring)
requires detection of anomalous configuration changes. ISO 27001 A.12.4
(Logging and Monitoring) requires event logging, protection of log information,
and administrator/operator activity logs.

**How to leverage existing Kubernetes infrastructure.** The design already stores
templates as Kubernetes objects (ConfigMaps in namespaces). This means every
template mutation flows through the Kubernetes API server, which provides:

1. **Kubernetes audit logging** — When configured at `RequestResponse` level
   (https://kubernetes.io/docs/tasks/debug/debug-cluster/auditing/), the K8s
   audit log captures every API call including the authenticated user, the
   resource modified, the request body, and the response. Since template CRUD
   maps to ConfigMap CRUD, the audit trail is automatic.
2. **Resource versioning** — Every Kubernetes object carries a `resourceVersion`
   field. The console can record the `resourceVersion` before and after a
   template mutation to create a precise change record.
3. **Managed fields** — Kubernetes server-side apply tracks which field manager
   last set each field. If templates are applied with distinct field managers per
   hierarchy level, Kubernetes itself tracks which level "owns" each field.

**Recommendation.** Add the following to the design:

- **Template mutation events**: Define a convention for recording template
  changes. Options include (a) Kubernetes Events attached to the template
  ConfigMap, (b) annotations on the ConfigMap recording `last-modified-by` and
  `last-modified-at`, or (c) a dedicated audit log stream.
- **Immutable audit storage**: Ship Kubernetes audit logs to immutable storage
  (S3 with Object Lock, or a SIEM) to satisfy ISO 27001 A.12.4.2 (protection
  of log information).
- **Change diff in audit records**: For compliance, auditors want to see what
  changed, not just that something changed. Consider storing the previous
  template source in an annotation or a separate ConfigMap before overwriting.

### Gap 5: No admission control / policy-as-code integration point

**The problem.** ADR 014's CUE-level constraint enforcement (Decision 9) is
powerful but operates only within the CUE evaluation pipeline. There is no
integration point for external policy engines like OPA Gatekeeper or Kyverno to
evaluate the rendered output before it is applied to the cluster.

**Why it matters.** In a Nasdaq-100 company, the security team typically operates
an independent policy layer that applies regardless of how resources are
generated. OPA Gatekeeper ConstraintTemplates (e.g., `K8sRequiredLabels`,
`K8sContainerLimits`, `K8sBlockNodePort`) are the standard mechanism for this.
These constraints need to evaluate the _rendered_ Kubernetes manifests, not the
CUE source.

**How the design supports this.** The rendered output is applied via the
Kubernetes API, which means Gatekeeper admission webhooks evaluate it
automatically. The layered enforcement model in Decision 9 already describes CUE
constraints as the first layer and Go validation as the second. Kubernetes
admission control is an implicit third layer.

**Recommendation.** Make the three-layer enforcement model explicit in the ADR:

| Layer | What it enforces | When it runs | Who manages it |
|-------|-----------------|--------------|----------------|
| CUE platform template | Allowed Kinds, required labels, structural constraints | CUE evaluation time | Platform engineers |
| Go renderer (`apply.go`) | Hard-coded Kind allowlist, GVR mapping | After CUE evaluation | Console developers |
| Kubernetes admission (Gatekeeper/Kyverno) | Organization-wide security policies | At Kubernetes API apply time | Security engineers |

This makes the security team's independent enforcement point visible in the
architecture and clarifies that CUE constraints are a _developer experience
optimization_ (fast feedback) while admission control is the _security
enforcement boundary_ (hard stop).

### Gap 6: No dry-run or plan-before-apply workflow for agentic safety

**The problem.** The `RenderDeploymentTemplate` preview RPC evaluates a template
and returns the rendered output. But there is no equivalent of `terraform plan`
that shows the diff between the current cluster state and the proposed changes
before applying them.

**Why it matters for AI agents.** The emerging safety pattern for agentic
workloads is: (1) agent generates configuration, (2) static analysis / linting,
(3) policy engine evaluates, (4) plan/diff shows what will change, (5) human
approves. Steps 1–3 are covered by the current design (agent writes CUE, CUE
evaluates, Gatekeeper validates). Step 4 is missing. Step 5 is a product
decision (auto-approve for low-risk changes, human-in-the-loop for high-risk).

**Precedent.** Terraform's `plan` command is the canonical example. Spacelift
(spacelift.io) and env0 (env0.com) add policy-as-code gates that evaluate plans
before allowing apply. ArgoCD's "sync preview" shows the diff between the
desired and live state. Kubernetes itself supports `--dry-run=server` for
server-side dry runs.

**Recommendation.** Consider adding a `PlanResourceSet` RPC that:

1. Evaluates the template (existing `Render` path).
2. Fetches the current state of each resource from the cluster.
3. Computes a diff (created, updated, deleted, unchanged).
4. Returns the diff without applying.

This enables AI agents to generate a plan, post it to a PR comment for review,
and wait for approval before applying — the same workflow that has proven safe
for Terraform at scale.

### Gap 7: Scalability of the hierarchy walk at 1000+ projects

**The problem.** ADR 015 Decision 7 describes the authorization walk: for each
template operation, the system reads Namespace objects from the target level up
to the organization (up to 5 API calls). With per-request caching, this is
bounded within a single request.

**Why it matters at scale.** At 1000+ projects with 3 levels of folders, the
console could be making thousands of Kubernetes API calls per second for
authorization alone. While Namespace reads are fast (in-memory etcd), the
cumulative load could become significant during peak deployment activity (e.g.,
post-merge CI/CD storms where hundreds of agents deploy simultaneously).

**Recommendation.** Consider:

- **Hierarchy caching with watch-based invalidation**: Cache the hierarchy tree
  in memory and use a Kubernetes watch on Namespace objects with the
  `console.holos.run/resource-type` label to invalidate on changes. The
  hierarchy changes rarely (org/folder structure is relatively static); caching
  eliminates the per-request API walk for the common case.
- **Grant caching**: Cache grant annotations with a short TTL (30–60 seconds)
  or watch-based invalidation. Grants change infrequently compared to template
  evaluations.

### Gap 8: securityResources collection for the security engineer persona

**The problem.** The design defines two resource collections:
`platformResources` (platform/SRE) and `projectResources` (product engineers).
Security engineers are mentioned throughout both ADRs as a key stakeholder, but
they share `platformResources` with platform engineers and SREs. There is no
dedicated collection that security engineers own and that platform engineers
cannot modify.

**Why it matters.** In a Nasdaq-100 company, the security team typically operates
with independence from the platform team. A security engineer who defines a
NetworkPolicy or a PodSecurityPolicy replacement (Pod Security Admission labels)
needs assurance that a platform engineer cannot accidentally remove or weaken it.
With a shared `platformResources` collection, a platform engineer's template at
the same hierarchy level could unify with — and potentially conflict with — a
security engineer's NetworkPolicy.

**Precedent.** The v1alpha2 extension path (Decision 12) mentions
`securityResources` as a future addition. This validates that the design team
has identified this need.

**Recommendation.** Prioritize `securityResources` in v1alpha2 planning. The
collection should have the same structure as `platformResources` but with a
separate RBAC scope — only principals with a dedicated security permission can
write to it. This mirrors how Kubernetes itself separates RBAC
(ClusterRole/ClusterRoleBinding) from workload management (Deployments/Services).

## Compliance Mapping

The following table maps ADR 014/015 design elements to SOC2 and ISO 27001
controls, identifying where the design satisfies controls and where additional
work is needed.

### SOC2 Trust Services Criteria

| Control | Requirement | How ADR 014/015 satisfies it | Gap |
|---------|------------|------------------------------|-----|
| CC6.1 — Logical access | Restrict access to authorized individuals; audit changes | RBAC with three roles at four levels (ADR 015); grants stored as K8s annotations | Need explicit audit logging of template mutations |
| CC6.2 — Prior to issuing credentials | Credentials reviewed before access granted | Grants require Owner role to issue (ADR 015 Decision 2) | Consider time-bounded grants (nbf/exp already in grant schema) |
| CC6.3 — Based on authorization | Access based on job function | Hierarchy-based RBAC maps to org structure: platform eng → org, SRE → folder, product eng → project | Consider OIDC group-based grants for team-level access |
| CC7.2 — Monitoring | Detect anomalous activity | Template evaluation errors are caught by CUE; rendered output validated by Go renderer | Need alerting on unusual template mutation patterns (e.g., mass changes, off-hours modifications) |
| CC8.1 — Change management | Configuration changes are authorized, tested, and approved | Template preview RPC enables pre-deployment validation; RBAC controls who can modify templates | Need formal approval workflow for org-level template changes (these affect all projects) |

### ISO 27001 Annex A Controls

| Control | Requirement | How ADR 014/015 satisfies it | Gap |
|---------|------------|------------------------------|-----|
| A.9.2.3 — Privileged access | Restrict and control allocation of privileged access | Owner role required for template deletion and grant management; org-level templates require explicit Owner grant | Document the privilege escalation path (how to request org-level Owner) |
| A.12.1.2 — Change management | Formal change management procedures | Templates are versioned via TypeMeta; CUE evaluation provides deterministic output | Need change history / version log for templates |
| A.12.4.1 — Event logging | Log user activities, exceptions, and security events | K8s audit log captures all API calls including ConfigMap mutations (template storage) | Ensure K8s audit policy covers the template namespaces at RequestResponse level |
| A.12.4.3 — Admin logs | Log system administrator and operator activities | PlatformInput.Claims embeds deployer identity; K8s audit log captures the API caller | Correlate template authoring identity with deployment identity in audit records |
| A.14.2.2 — System change control | Control changes to systems within the development lifecycle | Template preview RPC validates before apply; CUE constraints enforce structural requirements | Need a promotion workflow for templates moving from dev → staging → prod |

## AI-Enabled Software Engineering Support

### How the design supports agentic workloads

The ADR 014/015 design is well-positioned for AI-enabled software engineering:

**1. Schema-driven safety.** Go structs with CUE tags define a machine-readable
contract. An AI agent can discover the schema (via generated CUE files or proto
definitions), generate a compliant template, and validate it via the
`RenderDeploymentTemplate` preview RPC — all without human intervention. The
TypeMeta version discrimination ensures the agent targets the correct schema
version.

**2. Constraint-based guardrails.** CUE's closed-struct mechanism (Decision 9)
means an AI agent that generates a template at the project level is
automatically constrained by org/folder-level policy. If the agent tries to
create a `ClusterRoleBinding`, the CUE evaluation fails. The agent receives a
clear error message and can self-correct. This is the "policy-as-code" guardrail
pattern recommended for agentic workloads by the emerging consensus across
Spacelift, env0, Pulumi AI, and Terraform Stacks.

**3. Preview before apply.** The `RenderDeploymentTemplate` RPC provides the
"plan" step in the agent safety loop: agent generates template → agent calls
preview → agent posts rendered output for review → human approves → agent
calls deploy. This mirrors GitHub Copilot Workspace's "plan before code"
pattern and Terraform's `plan` before `apply`.

**4. Principal-agnostic RBAC.** The RBAC model evaluates Bearer tokens, not
session types. An AI agent authenticating with a service account OIDC token
receives the same RBAC evaluation as a human. The agent's effective permissions
are determined by the grants on its service account principal — if it has
Editor on a project, it can create and modify templates in that project; if it
has no folder-level grants, it cannot modify platform templates. This is the
correct scoping: AI agents should have the minimum permissions required for
their task.

### Recommendations for strengthening agentic support

**1. Machine-readable error responses.** CUE evaluation errors are currently
text strings. For AI agents, structured error responses (field path, constraint
violated, expected vs. actual) would enable automated remediation. Consider
returning evaluation errors as a structured proto message alongside the text.

**2. Template linting RPC.** A lightweight RPC that validates CUE syntax and
checks for common mistakes (referencing undefined fields, missing required
fields) without full evaluation. This would give agents fast feedback during
iterative template editing.

**3. Scoped service accounts for agents.** Document the recommended pattern for
creating service accounts for AI agents: one service account per agent task,
granted Editor on the specific project, with time-bounded grants (the `exp`
field in the grant schema). This prevents credential sprawl and limits blast
radius.

## Summary of Recommendations

| # | Gap | Severity | Recommendation | Target version |
|---|-----|----------|---------------|----------------|
| 1 | No folderInput | Medium | Add FolderInput type in v1alpha2 | v1alpha2 |
| 2 | Shared template collaboration | Low | Document multi-template support per level; add owner-team annotation | v1alpha1 |
| 3 | API caller / MCP support | Medium | Address service account Claims; add rate limiting; add idempotency | v1alpha1 |
| 4 | Audit trail | High | Add template mutation logging; ship K8s audit logs to immutable storage | v1alpha1 |
| 5 | Admission control integration | Low | Document three-layer enforcement explicitly in ADR | v1alpha1 |
| 6 | Plan-before-apply | Medium | Add PlanResourceSet RPC for diff computation | v1alpha2 |
| 7 | Hierarchy walk scalability | Medium | Add watch-based hierarchy and grant caching | v1alpha1 |
| 8 | securityResources | Medium | Add dedicated security collection in v1alpha2 | v1alpha2 |

## Conclusion

ADR 014 and 015 provide a strong, principled foundation for configuration
management at enterprise scale. The use of CUE unification for hierarchical
template composition is a differentiating architectural choice — it provides
conflict detection that no override-based system (Helm, Kustomize, Jsonnet) can
match. The platformResources/projectResources split with renderer-enforced RBAC
boundaries is clean and aligns with proven patterns from Crossplane and Google
Cloud.

The most critical gap is audit trail completeness (Gap 4). A Nasdaq-100 company
undergoing SOC2 Type II or ISO 27001 certification will need to demonstrate that
every template change is logged, attributed, and stored immutably. The good news
is that the Kubernetes-native storage model (ConfigMaps in namespaces) means the
audit infrastructure already exists — it just needs to be configured and
documented.

For AI-enabled engineering, the design is ahead of most alternatives. The
schema-driven contract, CUE constraint guardrails, and preview RPC form a
natural "generate → validate → plan → apply" pipeline that matches the emerging
consensus for safe agentic infrastructure management. The principal-agnostic
RBAC model means ConnectRPC callers, MCP tool servers, and AI agents are
first-class citizens — they authenticate, they're authorized, and the resource
schema is transparent to the transport mechanism.
