// Shared color mappings from the design prototype
// (drawer.jsx protoColor / clients.jsx groupColor / pages3.jsx outColor)

export const protoColor = (t: string): string =>
  ({
    vless: 'var(--brand)',
    vmess: 'var(--violet)',
    trojan: 'var(--emerald)',
    shadowsocks: 'var(--amber)',
    hysteria2: 'var(--cyan)',
    hysteria: 'var(--cyan)',
    tuic: 'var(--pink)',
    naive: 'var(--sky)',
    shadowtls: 'var(--amber)',
    anytls: 'var(--emerald)',
    mieru: 'var(--violet)',
    mixed: 'var(--text-3)',
  } as Record<string, string>)[t] || 'var(--text-3)'

export const outColor = (t: string): string =>
  ({
    direct: 'var(--emerald)',
    block: 'var(--rose)',
    dns: 'var(--cyan)',
    selector: 'var(--brand)',
    urltest: 'var(--violet)',
  } as Record<string, string>)[t] || 'var(--amber)'

export const dnsColor = (t: string): string =>
  ({
    local: 'var(--text-3)',
    tls: 'var(--brand)',
    https: 'var(--violet)',
    udp: 'var(--cyan)',
    fakeip: 'var(--amber)',
  } as Record<string, string>)[t] || 'var(--cyan)'

export const actionColor = (a: string): string =>
  ({
    route: 'var(--brand)',
    reject: 'var(--rose)',
    'hijack-dns': 'var(--cyan)',
    'route-options': 'var(--violet)',
  } as Record<string, string>)[a] || 'var(--text-3)'
