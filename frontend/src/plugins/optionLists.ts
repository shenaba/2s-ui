/**
 * The small value lists behind the settings page's <Select> options.
 *
 * Deliberately their own module rather than living beside the subscription
 * catalogs: Basics.vue needs only `levels`, and importing it from a module that
 * also holds ~5 KB of geosite/Clash defaults made the bundler give /basics a
 * second chunk and a second request to fetch seven strings.
 *
 * Frozen for the same reason the catalogs are — these are shared for the whole
 * SPA session, so an accidental sort() or push() would follow the user around
 * until reload.
 */

/** sing-box log levels (`log.level`). */
export const levels = Object.freeze(
  ["trace", "debug", "info", "warn", "error", "fatal", "panic"],
)

/**
 * The DNS server types the subscription builder offers. A deliberate subset of
 * types/dns.ts's DnsTypes: the rest (hosts, dhcp, fakeip, tailscale, resolved)
 * need fields this two-input form has no place for.
 */
export const dnsTypes = Object.freeze(['udp', 'tcp', 'local', 'tls', 'quic', 'h3'])

/** mihomo/Clash log levels — a different set from sing-box's. */
export const clashLevels = Object.freeze(['debug', 'info', 'warning', 'error'])
