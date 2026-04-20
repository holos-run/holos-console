/**
 * Console configuration injected by the Go server into index.html
 * via window.__CONSOLE_CONFIG__.
 *
 * The `namespacePrefix` / `organizationPrefix` / `folderPrefix` / `projectPrefix`
 * fields mirror the backend resolver flags (`--namespace-prefix`,
 * `--organization-prefix`, `--folder-prefix`, `--project-prefix`). The frontend
 * must consume these at runtime so operators who customize the namespace
 * layout see the UI match the server.
 */
interface ConsoleConfig {
  devToolsEnabled: boolean
  namespacePrefix: string
  organizationPrefix: string
  folderPrefix: string
  projectPrefix: string
}

declare global {
  interface Window {
    __CONSOLE_CONFIG__?: Partial<ConsoleConfig>
  }
}

/**
 * Default prefix values mirror the Go server's defaults (see `console/console.go`
 * Config struct: NamespacePrefix="holos-", OrganizationPrefix="org-",
 * FolderPrefix="fld-", ProjectPrefix="prj-"). These apply when the global is
 * not injected (tests, static preview) or when a field is absent.
 */
const DEFAULT_CONFIG: ConsoleConfig = {
  devToolsEnabled: false,
  namespacePrefix: 'holos-',
  organizationPrefix: 'org-',
  folderPrefix: 'fld-',
  projectPrefix: 'prj-',
}

/**
 * Returns the console configuration, falling back to safe defaults
 * when the global is not injected (e.g., during tests or static preview)
 * or when the server omits individual fields.
 */
export function getConsoleConfig(): ConsoleConfig {
  const injected = window.__CONSOLE_CONFIG__ ?? {}
  return {
    devToolsEnabled: injected.devToolsEnabled ?? DEFAULT_CONFIG.devToolsEnabled,
    namespacePrefix: injected.namespacePrefix ?? DEFAULT_CONFIG.namespacePrefix,
    organizationPrefix:
      injected.organizationPrefix ?? DEFAULT_CONFIG.organizationPrefix,
    folderPrefix: injected.folderPrefix ?? DEFAULT_CONFIG.folderPrefix,
    projectPrefix: injected.projectPrefix ?? DEFAULT_CONFIG.projectPrefix,
  }
}
