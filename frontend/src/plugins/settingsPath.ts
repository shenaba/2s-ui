/**
 * Path / URL helpers for the settings page: rewriting the panel and
 * subscription public addresses after a proxy or path change, and getting the
 * operator back onto a working address once the panel restarts.
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
 *
 * normalizeProxyPath only decides the generated nginx location. The path the
 * panel actually *serves* on comes from GetWebPath, which used to skip the
 * TrimSpace this does — whitespace in Base URI then split the served path from
 * the advertised one and 404'd the whole panel. GetWebPath/SetWebPath and the
 * settings write path now trim too; keep all four in step.
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

/**
 * Path component of a hand-typed public URI: `''` when the field is empty,
 * `null` when it is not a parseable absolute URL.
 *
 * `null` rather than `''` because the callers read `''` as "nothing to check":
 * a URI missing its scheme would sail through the path guard and then be handed
 * to location.replace, which resolves it *relative* to the current page.
 */
export function uriPathOf(uri: string): string | null {
  const v = (uri ?? '').trim()
  if (!v) return ''
  let pathname: string
  try {
    pathname = new URL(v).pathname
  } catch {
    return null
  }
  // pathname is percent-encoded while webPath is stored verbatim, so a Base URI
  // with a space or any non-ASCII character would never compare equal and the
  // mismatch guard would block every save. Malformed escapes keep the raw form.
  try {
    pathname = decodeURIComponent(pathname)
  } catch {
    // leave pathname as-is
  }
  return normalizePath(pathname)
}

/**
 * The public address the panel fills in for itself once a reverse proxy
 * terminates TLS for it — the service only knows its own LAN host:port.
 *
 * Every caller must build it the same way, byte for byte: save() decides
 * "did we autofill this, or did the operator type it?" by comparing the stored
 * URI against this string. A second, slightly different spelling of the formula
 * makes that comparison fail, and then turning the proxy off can no longer clear
 * the now-dead address it left behind.
 */
export function autoURI(domain: string, path: string): string {
  return 'https://' + domain + normalizePath(path)
}

export const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

const probeTimeout = 2000
const probeBudget = 30000

/**
 * Poll a URL until the restarted panel answers, then let the caller redirect.
 *
 * Bare no-cors fetch on purpose: during the restart every request fails, and
 * HttpUtils would flash an error toast for each one; and when the redirect
 * crosses into https the response is cross-origin anyway, so an opaque response
 * that merely resolves is the signal.
 *
 * Each attempt is capped separately. When packets are silently dropped instead
 * of refused — switching to HTTPS through a changing firewall rule does that —
 * fetch hangs until the browser's own connect timeout (90s+), so a fixed retry
 * count degrades into minutes of a frozen page. A total budget plus a per-try
 * AbortSignal bounds the worst case at probeBudget + probeTimeout. Returning
 * after the budget is deliberate: the caller redirects regardless and lets the
 * browser produce the final error.
 */
export async function waitReachable(url: string) {
  const deadline = Date.now() + probeBudget
  while (Date.now() < deadline) {
    try {
      await fetch(url, { mode: 'no-cors', cache: 'no-store', signal: AbortSignal.timeout(probeTimeout) })
      return
    } catch {
      await sleep(800)
    }
  }
}
