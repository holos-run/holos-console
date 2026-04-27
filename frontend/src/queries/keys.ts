export const keys = {
  connect: {
    all: () => ['connect-query'] as const,
    getOrganization: (name: string) =>
      ['connect-query', 'getOrganization', name] as const,
    getOrganizationRaw: (name: string) =>
      ['connect-query', 'getOrganizationRaw', name] as const,
    getProject: (name: string) => ['connect-query', 'getProject', name] as const,
  },
  deployments: {
    list: (project: string) => ['deployments', 'list', project] as const,
    get: (project: string, name: string) =>
      ['deployments', 'get', project, name] as const,
    status: (project: string, name: string) =>
      ['deployments', 'status', project, name] as const,
    statusSummary: (project: string, name: string) =>
      ['deployments', 'status-summary', project, name] as const,
    logs: (
      project: string,
      name: string,
      container?: string,
      tailLines?: number,
      previous?: boolean,
    ) =>
      ['deployments', 'logs', project, name, container, tailLines, previous] as const,
    renderPreview: (project: string, name: string) =>
      ['deployments', 'render-preview', project, name] as const,
    policyState: (project: string, name: string) =>
      ['deployments', 'policy-state', project, name] as const,
    namespaceSecrets: (project: string) =>
      ['deployments', 'namespace-secrets', project] as const,
    namespaceConfigMaps: (project: string) =>
      ['deployments', 'namespace-configmaps', project] as const,
    preflightCheck: (project: string, plannedDeploymentNames: string[]) =>
      ['deployments', 'preflight-check', project, ...plannedDeploymentNames] as const,
    dependencyEdgeCascadeDelete: (
      project: string,
      kind: string,
      namespace: string,
      name: string,
    ) =>
      ['deployments', 'dependency-edge-cascade-delete', project, kind, namespace, name] as const,
  },
  folders: {
    list: (organization: string, parentType?: number, parentName?: string) =>
      ['folders', 'list', organization, parentType, parentName] as const,
  },
  organizations: {
    list: () => ['organizations', 'list'] as const,
    get: (name: string) => keys.connect.getOrganization(name),
    raw: (name: string) => keys.connect.getOrganizationRaw(name),
  },
  permissions: {
    // Bulk SelfSubjectAccessReview lookup. The cache key is intentionally
    // shaped from the same deterministic permission keys the backend
    // returns (verb:group/resource[:namespace[:name]]), so two identical
    // queries with attributes in different declaration order still hit
    // the same cache entry.
    list: (permissionKeys: string[]) =>
      ['permissions', 'list', ...[...permissionKeys].sort()] as const,
  },
  projectSettings: {
    get: (project: string) => ['project-settings', 'get', project] as const,
    raw: (project: string) => ['project-settings', 'raw', project] as const,
  },
  projects: {
    listByParent: (
      organization: string,
      parentType?: number,
      parentName?: string,
    ) => ['projects', 'listByParent', organization, parentType, parentName] as const,
    get: (name: string) => keys.connect.getProject(name),
  },
  secrets: {
    list: (project: string) => ['secrets', 'list', project] as const,
    get: (project: string, name: string) =>
      ['secrets', 'get', project, name] as const,
    raw: (project: string, name: string) =>
      ['secrets', 'raw', project, name] as const,
    fanout: (project: string) => ['secrets', 'list', project, 'fanout'] as const,
  },
  templatePolicies: {
    list: (namespace: string) => ['templatePolicies', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templatePolicies', 'get', namespace, name] as const,
    linkable: (namespace: string) =>
      ['templatePolicies', 'linkable', namespace] as const,
  },
  templatePolicyBindings: {
    list: (namespace: string) =>
      ['templatePolicyBindings', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templatePolicyBindings', 'get', namespace, name] as const,
  },
  templateDependencies: {
    templateDependents: (namespace: string, name: string) =>
      ['templateDependencies', 'templateDependents', namespace, name] as const,
    deploymentDependents: (namespace: string, name: string) =>
      ['templateDependencies', 'deploymentDependents', namespace, name] as const,
    list: (namespace: string) =>
      ['templateDependencies', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templateDependencies', 'get', namespace, name] as const,
  },
  templateRequirements: {
    list: (namespace: string) =>
      ['templateRequirements', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templateRequirements', 'get', namespace, name] as const,
  },
  templateGrants: {
    list: (namespace: string) =>
      ['templateGrants', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templateGrants', 'get', namespace, name] as const,
  },
  templates: {
    list: (namespace: string) => ['templates', 'list', namespace] as const,
    get: (namespace: string, name: string) =>
      ['templates', 'get', namespace, name] as const,
    linkable: (namespace: string, includeSelfScope: boolean) =>
      ['templates', 'linkable', namespace, includeSelfScope] as const,
    examples: () => ['templates', 'examples'] as const,
    search: (
      namespace: string,
      name: string,
      displayNameContains: string,
      organization: string,
    ) =>
      ['templates', 'search', namespace, name, displayNameContains, organization] as const,
    defaults: (namespace: string, name: string) =>
      ['templates', 'defaults', namespace, name] as const,
    policyState: (namespace: string, name: string) =>
      ['templates', 'policy-state', namespace, name] as const,
    policyStateScope: (namespace: string) =>
      ['templates', 'policy-state', namespace] as const,
    render: (namespace: string, cueTemplate: string, cueInput: string) =>
      ['templates', 'render', namespace, cueTemplate, cueInput] as const,
  },
} as const
