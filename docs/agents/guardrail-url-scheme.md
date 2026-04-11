# Guardrail: URL Scheme

**Top-level resources** (organizations, folders, projects) are globally namespaced (ADR 022) and have dedicated URL prefixes reflecting their global identity:

| Resource type | URL pattern | Example |
|---------------|-------------|---------|
| Organization | `/orgs/$orgName/...` | `/orgs/acme/settings` |
| Folder | `/folders/$folderName/...` | `/folders/engineering/templates` |
| Project | `/projects/$projectName/...` | `/projects/frontend/secrets` |

**Sub-resources** (secrets, deployments, project-scoped templates) are scoped under their parent project: `/projects/$projectName/secrets/$secretName`.

**Org-scoped navigation** views (folder list, project list) live under `/orgs/$orgName/...` because they filter by organization context: `/orgs/$orgName/folders`, `/orgs/$orgName/projects`.

**Rule**: Never nest a top-level resource under another top-level resource in the URL. A folder detail page is `/folders/$folderName`, not `/orgs/$orgName/folders/$folderName`.

**Triggers**: Apply this rule when adding new route files to `frontend/src/routes/` or modifying navigation links.

## Incorrect Patterns

| Pattern | Why it is wrong |
|---------|-----------------|
| `/orgs/$orgName/folders/$folderName` | Embeds redundant org context; folder ID is globally unique |
| `/orgs/$orgName/projects/$projectName` | Embeds redundant org context; project ID is globally unique |

## Related

- [ADR 023: Top-Level URL Scheme](../adrs/023-top-level-url-scheme.md) -- full rationale and URL pattern table
- [Resource Naming](guardrail-resource-naming.md) -- slug-based identifiers that make global uniqueness possible
