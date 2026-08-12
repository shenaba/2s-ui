/**
 * Path / URL helpers used by the settings page when rewriting panel and
 * subscription public addresses after a proxy or path change.
 *
 * Kept out of Settings.vue so the pure bits are importable and the Vue
 * SFC stays focused on the form state.
 */

/** Fields whose change rewrites the nginx vhost and requires a panel restart. */
export const proxyInputs = [
  'webNginx', 'webDomain', 'webPort', 'webPath', 'webListen', 'webCertFile', 'webKeyFile',
  'subNginx', 'subDomain', 'subPort', 'subPath', 'subListen', 'subCertFile', 'subKeyFile',
] as const

/** Listen addresses treated as loopback when warning about plain-HTTP exposure. */
export const loopbackListens = ['127.0.0.1', 'localhost', '::1', '[::1]']

/**
 * Normalise a path so it starts and ends with `/`, matching the backend's
 * normalizeProxyPath. Both sides must produce the same public URL so we can
 * tell "we autofilled this" from "the operator typed it".
 */
export function normalizePath(p: string): string {
  let s = (p ?? '').trim()
  if (s === '') s = '/'
  if (!s.startsWith('/')) s = '/' + s
  if (!s.endsWith('/')) s += '/'
  return s
}

/**
 * Whether the panel itself speaks TLS after restart. Priority matches web.go:
 * webNginx short-circuits first (panel is plain HTTP behind nginx); otherwise
 * any cert path means HTTPS. The post-restart redirect depends on this.
 */
export function panelIsTLS(s: {
  webNginx: string
  webCertMode: string
  webCertFile: string
  webKeyFile: string
}): boolean {
  return s.webNginx !== "true" &&
    (s.webCertMode === "acme" || s.webCertFile !== "" || s.webKeyFile !== "")
}

/** Build the settings-page URL after a panel restart. */
export function buildURL(host: string, port: string, isTLS: boolean, path: string): string {
  if (!host || host.length == 0) host = window.location.hostname
  if (!port || port.length == 0) port = window.location.port

  const protocol = isTLS ? "https:" : "http:"

  if (port === "" || (isTLS && port === "443") || (!isTLS && port === "80")) {
    port = ""
  } else {
    port = `:${port}`
  }

  return `${protocol}//${host}${port}${path}settings`
}

/** Path component of a hand-typed public URI; empty if not an absolute URL. */
export function uriPathOf(uri: string): string {
  const v = (uri ?? '').trim()
  if (!v) return ''
  try {
    return normalizePath(new URL(v).pathname)
  } catch {
    return ''
  }
}
