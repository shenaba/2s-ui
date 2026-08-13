/**
 * Pure helpers for the JSON-subscription rule builder on the settings page.
 *
 * Everything the chip selectors and the DNS toggle do to `route.rules` and
 * `route.rule_set` reduces to these functions, so the sharp edges are in one
 * place: the JSON editor accepts any parseable JSON, and the backend hands the
 * result to sing-box verbatim, so a shape assumption here becomes a broken
 * subscription or a blank settings page.
 */

import { cloneDefault } from './subExtDefaults'

export type RuleSetEntry = { tag: string, [k: string]: any }

/**
 * Read a value the JSON editor may have replaced with any shape.
 *
 * The editor does a bare JSON.parse, so `rules` and `dns.servers` can be an
 * object, a string, anything. Calling .findIndex/.map on that throws during
 * render, and the chip selectors that would throw are rendered unconditionally
 * — the whole Settings subtree dies, taking the "editor" button with it, so the
 * bad config can no longer be fixed from the UI at all.
 */
export function asArray(v: unknown): any[] {
  return Array.isArray(v) ? v : []
}

/**
 * Whether `rules` holds nothing but the bare sniff rule the DNS toggle adds.
 *
 * Turning the toggle on inserts `{action:'sniff'}` when there were no rules at
 * all; turning it off has to take that back. Leaving it makes the subscription
 * carry a `rules` array, and the backend treats any `rules` as "the template
 * defines its own" and replaces the defaults wholesale — the clash_mode
 * Direct/Global routes disappear and clients silently stop honouring the mode.
 */
export function isOnlySniff(rules: unknown): boolean {
  const list = asArray(rules)
  return list.length === 1 &&
    list[0]?.action === 'sniff' && Object.keys(list[0]).length === 1
}

/** Every rule_set tag referenced by the DNS rules and the routing rules. */
export function collectRuleSetTags(...ruleLists: unknown[]): string[] {
  const tags: string[] = []
  for (const list of ruleLists) {
    asArray(list).forEach((r: any) => { if (r.rule_set) tags.push(...asArray(r.rule_set)) })
  }
  return tags
}

/**
 * Rebuild the `rule_set` definitions for the currently selected tags.
 * Returns null when the key should be dropped entirely.
 *
 * Two things have to survive a chip click. Anything the operator added by hand
 * in the JSON editor is carried over — the list is rebuilt from the selectors,
 * so otherwise one click silently drops it. And a catalog entry the operator
 * already edited wins over the catalog, so swapping the jsDelivr URL for a
 * mirror sticks; the trade-off is that later catalog fixes no longer reach them.
 *
 * `catalog` is the only authority for what a tag means: a tag selected in the UI
 * with no matching catalog row emits a rule_set reference with no definition,
 * and sing-box rejects the whole config at startup ("rule-set not found").
 */
export function mergeRuleSets(tags: string[], existing: unknown, catalog: readonly RuleSetEntry[]): any[] | null {
  const list = asArray(existing)
  const byTag = new Map(list.map((rs: any) => [rs.tag, rs]))
  const custom = list.filter((rs: any) => !catalog.some(g => g.tag == rs.tag))
  if (tags.length === 0 && custom.length === 0) return null
  return [
    ...catalog.filter(g => tags.includes(g.tag)).map(g => byTag.get(g.tag) ?? cloneDefault(g)),
    ...custom,
  ]
}
