import { HttpClientRef } from './httpClient'

interface generalRule {
  invert: boolean
  action: 'route' | 'route-options' | 'reject' | 'hijack-dns' | 'sniff' | 'resolve' | 'bypass'
  outbound?: string
  override_address?: string
  override_port?: number
  udp_disable_domain_unmapping?: boolean
  udp_connect?: boolean
  udp_timeout?: string
  method?: string
  no_drop?: boolean
  sniffer: string[]
  timeout: string
  strategy: string
  server: string
  // Break the TLS handshake across records, and -- new in 1.14 -- send a forged
  // ClientHello carrying tls_spoof's SNI ahead of the real one, so an
  // SNI-filtering middlebox sees a hostname it permits. The spoof needs
  // elevated privileges and only works on Linux, macOS and Windows.
  tls_fragment?: boolean
  tls_fragment_fallback_delay?: string
  tls_record_fragment?: boolean
  tls_spoof?: string
  tls_spoof_method?: string
}

export const actionKeys = [
  'invert',
  'action',
  'outbound',
  'override_address',
  'override_port',
  'udp_disable_domain_unmapping',
  'udp_connect',
  'udp_timeout',
  'method',
  'no_drop',
  'sniffer',
  'timeout',
  'strategy',
  'server',
  'tls_fragment',
  'tls_fragment_fallback_delay',
  'tls_record_fragment',
  'tls_spoof',
  'tls_spoof_method'
]
export interface logicalRule extends generalRule {
  type: 'logical' | 'simple'
  mode: 'and' | 'or'
  rules: rule[]
}

export interface rule extends generalRule {
  inbound?: string[]
  ip_version?: 4 | 6
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
  ip_is_private?: boolean
  source_port?: number[]
  source_port_range?: string[]
  port?: number[]
  port_range?: string[]
  process_name?: string[]
  process_path?: string[]
  process_path_regex?: string[]
  package_name?: string[]
  package_name_regex?: string[]
  source_mac_address?: string[]
  source_hostname?: string[]
  user?: string[]
  user_id?: number[]
  clash_mode?: string
  rule_set?: string[]
  rule_set_ip_cidr_match_source?: boolean
  preferred_by?: string[]
  interface_address?: string[]
  network_interface_address?: string[]
  default_interface_address?: string[]
}

// Transport used to download a remote rule-set. Replaces the download_detour
// option deprecated in sing-box 1.14. Leaving it out downloads over the
// default outbound, which is what a detour to a plain direct outbound meant.
export type { HttpClientRef } from './httpClient'

export interface ruleset {
  type: 'local' | 'remote'
  tag: string
  format: 'source' | 'binary'
  path?: string
  url?: string
  // Read once at startup when nothing is cached yet, so the first download does
  // not hold the core up; the set is refreshed in the background either way.
  initial_path?: string
  http_client?: HttpClientRef
  update_interval?: string
}