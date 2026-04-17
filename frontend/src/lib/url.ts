/**
 * Returns true only when `value` parses as a URL with an http: or https:
 * scheme. Used to guard rendering of template-authored URLs (such as
 * `output.url` from the render preview or cached on the deployment
 * ConfigMap) into anchor hrefs so that unsafe schemes like `javascript:`,
 * `data:`, `vbscript:`, and `file:` never reach the DOM. Parse failures
 * (malformed URLs) also return false.
 *
 * Shared across the Status tab on the deployment detail page and the
 * deployments listing page so both callsites enforce the same scheme
 * allowlist.
 */
export function isSafeHttpUrl(value: string): boolean {
  try {
    const u = new URL(value)
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}
