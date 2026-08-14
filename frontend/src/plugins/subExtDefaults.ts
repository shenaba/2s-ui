/**
 * Default snippets for the JSON / Clash subscription builders on the
 * settings page. Pure data — no Vue, no i18n — so it lives outside the SFC.
 *
 * These are module-scoped catalogs: clone one of them before putting it into
 * reactive form state, or the form edits it in place and every later read of
 * the "default" returns the previous operator's edits (and unlike the old
 * setup()-local consts, remounting Settings no longer recreates them).
 *
 * That rule is enforced rather than documented — see deepFreeze below.
 */
import { toRaw } from 'vue'

/**
 * Clone a catalog entry for the reactive form. toRaw first: structuredClone
 * throws DataCloneError on a Vue proxy, so a caller that hands us something
 * already in the form state would otherwise blow up mid-setter.
 */
export function cloneDefault<T>(v: T): T {
  return structuredClone(toRaw(v))
}

/**
 * Every catalog below is wrapped in this, so forgetting cloneDefault fails
 * loudly at the first write (ES modules are strict mode, so a write through
 * Vue's reactive proxy throws TypeError) instead of silently poisoning the
 * catalog for the rest of the session. The frozen-ness does not survive
 * structuredClone, so the copies handed to the form stay writable.
 *
 * Wrapped at each declaration rather than in one pass at the end of the file:
 * a trailing forEach over an export list is a second, hand-maintained copy of
 * that list which silently stops covering whatever someone forgets to add to
 * it, and being a top-level side effect it also pins every catalog against
 * tree-shaking. The PURE annotation on each call tells the bundler it may drop
 * a catalog nobody imports.
 */
function deepFreeze<T>(v: T): T {
  if (v && typeof v === 'object') Object.values(v).forEach(deepFreeze)
  return Object.freeze(v)
}

export const defaultLog = /* @__PURE__ */ deepFreeze({
  "level": "info",
  "timestamp": true
})

export const defaultInb = /* @__PURE__ */ deepFreeze([
  {
    "type": "tun",
    "address": [
      "172.19.0.1/30",
      "fdfe:dcba:9876::1/126"
    ],
    "mtu": 9000,
    "auto_route": true,
    "strict_route": false,
    "endpoint_independent_nat": false,
    "stack": "mixed",
    "exclude_package": [] as string[],
    "platform": {
      "http_proxy": {
        "enabled": true,
        "server": "127.0.0.1",
        "server_port": 2080
      }
    }
  },
  {
    "type": "mixed",
    "listen": "127.0.0.1",
    "listen_port": 2080,
    "users": []
  }
])

export const defaultExp = /* @__PURE__ */ deepFreeze({
  "clash_api": {
    "external_controller": "127.0.0.1:9090",
    "external_ui": "ui",
    "secret": "",
    "external_ui_download_url": "https://mirror.ghproxy.com/https://github.com/MetaCubeX/Yacd-meta/archive/gh-pages.zip",
    "external_ui_download_detour": "direct",
    "default_mode": "rule"
  },
  "cache_file": {
    "enabled": true,
    "store_fakeip": false
  }
})

export const defaultDns = /* @__PURE__ */ deepFreeze({
  "servers": [
    {
      "type": "tcp",
      "tag": "proxy-dns",
      "server": "8.8.8.8",
      "server_port": 53,
      "detour": "proxy",
      "domain_resolver": "local-dns",
    },
    {
      "tag": "direct-dns",
      "type": "local",
    },
    {
      "tag": "local-dns",
      "type": "local",
    }
  ],
  "rules": [
    {
      "clash_mode": "Global",
      "source_ip_cidr": [
        "172.19.0.0/30",
        "fdfe:dcba:9876::1/126"
      ],
      "action": "route",
      "server": "proxy-dns"
    },
    {
      "clash_mode": "Direct",
      "action": "route",
      "server": "direct-dns"
    },
    {
      "source_ip_cidr": [
        "172.19.0.0/30",
        "fdfe:dcba:9876::1/126"
      ],
      "action": "route",
      "server": "proxy-dns"
    },
  ],
  "final": "local-dns",
  "strategy": "prefer_ipv4"
})

export const geoList = /* @__PURE__ */ deepFreeze([
  { title: "Site-Private", value: "geosite-private" },
  { title: "IP-Private", value: "geoip-private" },
  { title: "Site-Ads", value: "geosite-ads" },
  { title: "🇮🇷 Site-Iran", value: "geosite-ir" },
  { title: "🇮🇷 IP-Iran", value: "geoip-ir" },
  { title: "🇨🇳 Site-China", value: "geosite-cn" },
  { title: "🇨🇳 IP-China", value: "geoip-cn" },
  { title: "🇻🇳 Site-Vietnam", value: "geosite-vn" },
  { title: "🇻🇳 IP-Vietnam", value: "geoip-vn" },
])

// Derived, not a third hand-written copy of the same tags: `geo` is the only
// authority (updateRuleSets builds the rule_set definitions by filtering it), so
// a tag that exists in a selector but not in `geo` ships a subscription whose
// rule_set reference has no definition — sing-box aborts at startup on that.
export const geositeList = /* @__PURE__ */ deepFreeze(
  geoList
    .filter(g => g.value.startsWith('geosite-'))
    .map(g => ({ title: g.title.replace('Site-', ''), value: g.value })),
)

export const geo = /* @__PURE__ */ deepFreeze([
  {
    tag: "geosite-ads",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ads-all.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-private",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/private.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-ir",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ir.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-cn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-vn",
    type: "remote",
    format: "binary",
    url: "https://github.com/Thaomtam/Geosite-vn/raw/rule-set/Geosite-vn.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-private",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/private.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-ir",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/ir.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-cn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-vn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/vn.srs",
    download_detour: "direct"
  }
])

export const defaultConfig: any = /* @__PURE__ */ deepFreeze({
  "mixed-port": 7890,
  "allow-lan": false,
  "mode": "rule",
  "log-level": "info",
  "external-controller": "127.0.0.1:9090",
  "tun": {
    "enable": true,
    "stack": "system",
    "auto-route": true,
    "auto-detect-interface": true,
    "dns-hijack": ["any:53"],
  },
  "dns": {
    "enable": true,
    "ipv6": false,
    "enhanced-mode": "fake-ip",
    "fake-ip-range": "198.18.0.1/16",
    "default-nameserver": ["8.8.8.8", "1.1.1.1"],
    "nameserver": [
      "https://doh.pub/dns-query",
      "https://1.0.0.1/dns-query"
    ],
    "fallback": ["tcp://9.9.9.9:53"],
    "fake-ip-filter": ["*.lan", "localhost", "*.local"]
  },
  "rules": [
    "GEOIP,Private,DIRECT",
    "MATCH,Proxy"
  ]
})

export const rulesIP = /* @__PURE__ */ deepFreeze([
  { title: 'Private-Direct', value: 'GEOIP,Private,DIRECT' },
  { title: 'Private-Block', value: 'GEOIP,Private,REJECT' },
  { title: 'LAN-Direct', value: 'GEOIP,LAN,DIRECT' },
  { title: 'LAN-Block', value: 'GEOIP,LAN,REJECT' },
  { title: 'Ads-Direct', value: 'GEOIP,Ads,DIRECT' },
  { title: 'Ads-Block', value: 'GEOIP,Ads,REJECT' },
  { title: '🇨🇳 China-Direct', value: 'GEOIP,CN,DIRECT' },
  { title: '🇨🇳 China-Block', value: 'GEOIP,CN,REJECT' },
  { title: '🇮🇷 Iran-Direct', value: 'GEOIP,CATEGORY-IR,DIRECT' },
  { title: '🇮🇷 Iran-Block', value: 'GEOIP,CATEGORY-IR,REJECT' },
  { title: '🇻🇳 Vietnam-Direct', value: 'GEOIP,CATEGORY-VN,DIRECT' },
  { title: '🇻🇳 Vietnam-Block', value: 'GEOIP,CATEGORY-VN,REJECT' },
  { title: '🇯🇵 Japan-Direct', value: 'GEOIP,JP,DIRECT' },
  { title: '🇯🇵 Japan-Block', value: 'GEOIP,JP,REJECT' },
])

