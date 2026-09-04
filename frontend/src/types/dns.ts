export interface Dns {
  servers: DnsServer[]
  rules: dnsRule[]
  final?: string
  strategy?: string
  // How long a query may take before it is given up on.
  timeout?: string
  disable_cache?: boolean,
  disable_expire?: boolean,
  cache_capacity?: number,
  // Answer from an expired cache entry while the refresh runs, rather than
  // making the client wait for it.
  optimistic?: {
    enabled?: boolean
    timeout?: string
  },
  reverse_mapping?: boolean,
  client_subnet?: string,
}

export const DnsTypes = {
  Local: 'local',
  MDNS: 'mdns',
  Hosts: 'hosts',
  TCP: 'tcp',
  UDP: 'udp',
  TLS: 'tls',
  QUIC: 'quic',
  HTTPS: 'https',
  HTTP3: 'h3',
  DHCP: 'dhcp',
  FakeIP: 'fakeip',
  Tailscale: 'tailscale',
  Resolved: 'resolved',
  // Resolve through the DNS an OpenConnect or OpenVPN endpoint was handed by
  // its server, the same shape tailscale's server already has.
  OpenConnect: 'openconnect',
  OpenVPN: 'openvpn',
}

export type DnsType = typeof DnsTypes[keyof typeof DnsTypes]

type InterfaceMap = {
  [Key in keyof typeof DnsTypes]: {
    type: string
    [otherProperties: string]: any
  }
}

export type DnsServer = InterfaceMap[keyof InterfaceMap]

const defaultValues: Record<DnsType, DnsServer> = {
  local: { type: 'local' },
  mdns: { type: 'mdns' },
  hosts: { type: 'hosts', path: ['/etc/hosts'] },
  tcp: { type: 'tcp', server_port: 53 },
  udp: { type: 'udp', server_port: 53 },
  tls: { type: 'tls', server_port: 853, tls: {} },
  quic: { type: 'quic', server_port: 853, tls: {} },
  https: { type: 'https', server_port: 443, tls: {}, headers: {} },
  h3: { type: 'h3', server_port: 443, tls: {}, headers: {} },
  predefined: { type: 'predefined', rcode: 'NOERROR' },
  dhcp: { type: 'dhcp' },
  fakeip: { type: 'fakeip', inet4_range: '198.18.0.0/15', inet6_range: 'fc00::/18' },
  tailscale: { type: 'tailscale' },
  resolved: { type: 'resolved' },
  openconnect: { type: 'openconnect' },
  openvpn: { type: 'openvpn' },
}
export function createDnsServer<T extends DnsServer>(type: string, json?: Partial<T>): DnsServer {
  // Deep copy: a shallow spread hands every new server the very same nested
  // objects (tls, headers, path), so filling in one form rewrites the defaults
  // the next one starts from. The clone is annotated because spreading
  // JSON.parse's `any` would make the literal `any` too and stop TypeScript
  // from checking it.
  const base: DnsServer = JSON.parse(JSON.stringify(defaultValues[type] ?? {}))
  const defaultObject: DnsServer = { ...base, ...(json || {}) }
  return defaultObject
}

interface generalDnsRule {
  invert: boolean
  // evaluate fetches a response now so later rules can match it; respond
  // returns the response an earlier evaluate fetched, and takes no options of
  // its own -- sing-box rejects any other key on it.
  action: 'route' | 'evaluate' | 'respond' | 'route-options' | 'reject' | 'predefined'
  // Let response-dependent rules run in parallel, first match wins.
  race?: boolean
  server?: string
  // evaluate only: names this response so a later match_response can pick it
  // out from several.
  tag?: string
  // Start the query while race rules are still pending.
  speculative?: boolean
  timeout?: string
  disable_optimistic_cache?: boolean
  remove_client_subnet?: boolean
  // Deprecated in sing-box 1.14, removed in 1.16, and rejected outright when
  // the same config sets ip_version or query_type anywhere. Kept so a stored
  // rule still round-trips and can be cleared; the drawer no longer offers it
  // to a rule that has none.
  strategy?: string
  disable_cache?: boolean
  rewrite_ttl?: number
  client_subnet?: string
  method?: string
  no_drop?: boolean
  rcode?: string
  answer?: string[]
  ns?: string[]
  extra?: string[]
}

export const actionDnsRuleKeys = [
  'invert',
  'action',
  'race',
  'server',
  'tag',
  'speculative',
  'timeout',
  'disable_optimistic_cache',
  'remove_client_subnet',
  'strategy',
  'disable_cache',
  'rewrite_ttl',
  'client_subnet',
  'method',
  'no_drop',
  'rcode',
  'answer',
  'ns',
  'extra',
]
export interface logicalDnsRule extends generalDnsRule {
  type: 'logical' | 'simple'
  mode: 'and' | 'or'
  rules: dnsRule[]
}

export interface dnsRule extends generalDnsRule {
  inbound?: string[]
  ip_version?: 4 | 6
  query_type?: string[]
  query_client_subnet?: string[]
  query_dnssec?: boolean
  preferred_by?: string[]
  source_mac_address?: string[]
  source_hostname?: string[]
  package_name_regex?: string[]
  // Gate for the response fields below: true matches whatever the preceding
  // evaluate fetched, a string picks the response carrying that tag. sing-box
  // rejects an empty string, so the drawer stores true when no tag is given.
  match_response?: boolean | string
  response_rcode?: string
  response_answer?: string[]
  response_ns?: string[]
  response_extra?: string[]
  network?: string[]
  auth_user?: string[]
  protocol?: string[]
  domain?: string[]
  domain_suffix?: string[]
  domain_keyword?: string[]
  domain_regex?: string[]
  source_ip_cidr?: string[]
  source_ip_is_private?: boolean
  ip_cidr?: string[]
  ip_is_private: boolean
  ip_accept_any: boolean
  source_port?: number[]
  source_port_range?: string[]
  port?: number[]
  port_range?: string[]
  process_name?: string[]
  process_path?: string[]
  process_path_regex?: string[]
  package_name?: string[]
  user?: string[]
  user_id?: number[]
  clash_mode?: string
  rule_set?: string[]
  rule_set_ip_cidr_match_source?: boolean
}
