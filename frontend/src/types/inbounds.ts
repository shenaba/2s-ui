import { iMultiplex } from "./multiplex"
import { iTls } from "./tls"
import { Dial } from "./dial"
import { Transport } from "./transport"
import RandomUtil from "@/plugins/randomUtil"
import { QuicFields } from './httpClient'

export const InTypes = {
  Direct: 'direct',
  Mixed: 'mixed',
  SOCKS: 'socks',
  HTTP: 'http',
  Shadowsocks: 'shadowsocks',
  Snell: 'snell',
  VMess: 'vmess',
  Trojan: 'trojan',
  Naive: 'naive',
  Hysteria: 'hysteria',
  ShadowTLS: 'shadowtls',
  TUIC: 'tuic',
  Hysteria2: 'hysteria2',
  VLESS: 'vless',
  AnyTls: 'anytls',
  Tun: 'tun',
  Redirect: 'redirect',
  TProxy: 'tproxy',
  Cloudflared: 'cloudflared',
}

type InType = typeof InTypes[keyof typeof InTypes]

export interface Addr {
  server: string
  server_port: number
  tls?: boolean
  insecure?: boolean
  server_name?: string
  remark?: string
}

// The NAT mapping and filtering behaviours 1.14 made configurable. Carried by
// everything that owns a UDP NAT -- tun and tproxy here, the WireGuard endpoint
// in endpoints.ts. Absent means endpoint_independent, sing-box's default.
export interface UdpNatFields {
  udp_mapping?: string
  udp_filtering?: string
  udp_nat_max?: number
}

export interface Listen {
  listen: string
  listen_port: number
  tcp_fast_open?: boolean
  tcp_multi_path?: boolean
  udp_fragment?: boolean
  udp_timeout?: string
  detour?: string
  disable_tcp_keep_alive?: boolean
  tcp_keep_alive?: string
  tcp_keep_alive_interval?: string
}

interface InboundBasics extends Listen {
  id: number
  type: InType
  tag: string
  tls_id: number
  // Set when this inbound is a read-only replica of one living on a node.
  node_id?: number
  addrs?: Addr[]
  out_json?: any
}

interface ShadowTLSHandShake extends Dial {
  server: string
  server_port: number
}

export interface Direct extends InboundBasics {
  network?: "udp" | "tcp"
  override_address?: string
  override_port?: number
}
export interface Mixed extends InboundBasics {}
export interface SOCKS extends InboundBasics {}
export interface HTTP extends InboundBasics {}
export interface Shadowsocks extends InboundBasics {
  method: string
  password: string
  network?: "udp" | "tcp"
  multiplex?: iMultiplex
  managed?: boolean
}
export interface VMess extends InboundBasics {
  tls: iTls
  multiplex?: iMultiplex
  transport?: Transport
}
export interface Trojan extends InboundBasics {
  tls: iTls
  fallback?: {
    server: string
    server_port: number
  }
  multiplex?: iMultiplex
  transport?: Transport
}
export interface Naive extends InboundBasics {
  // Still a sing-box 1.14 option, still written by the Network control in the
  // naive form, and still what util/genLink.go picks the link scheme from.
  network?: "udp" | "tcp"
  tls: iTls,
  quic_congestion_control?: "" | "bbr" | "bbr2" | "cubic" | "reno"
}
// The QUIC fields replace hysteria's own recv_window_conn, recv_window_client,
// max_conn_client and disable_mtu_discovery, which sing-box still reads but has
// deprecated.
export interface Hysteria extends InboundBasics, QuicFields {
  up_mbps: number
  down_mbps: number
  obfs?: string
}
export interface ShadowTLS extends InboundBasics {
  version: 1|2|3
  password?: string
  handshake: ShadowTLSHandShake
  handshake_for_server_name?: {
    [server_name: string]: ShadowTLSHandShake
  }
  strict_mode?: boolean
  wildcard_sni?: string
}
export interface VLESS extends InboundBasics {
  multiplex?: iMultiplex
  transport?: Transport
  tls: iTls
}

export interface AnyTls extends InboundBasics {
  padding_scheme: string[]
  tls: iTls
}
export interface TUIC extends InboundBasics, QuicFields {
  congestion_control: ""|"cubic"|"new_reno"|"bbr"
  auth_timeout?: string
  zero_rtt_handshake?: boolean
  heartbeat?: string
}
export interface Hysteria2 extends InboundBasics, QuicFields {
  up_mbps?: number
  down_mbps?: number
  obfs?: {
    type?: "salamander"
    password: string
  }
  ignore_client_bandwidth?: boolean
  masquerade?: string | {
    type: string
    directory?: string
    url?: string
    rewrite_host?: boolean
    status_code?: number
    headers?: Headers[]
    content?: string
  }
  brutal_debug?: boolean
}
export interface Tun extends InboundBasics, UdpNatFields {
  interface_name?: string
  address?: string[]
  mtu?: number
  udp_timeout?: string
  stack?: string
  // hijack, the default, now also sets the platform's interface DNS and
  // installs platform-level hijacking; native leaves both alone.
  dns_mode?: 'disabled' | 'native' | 'hijack'
  dns_address?: string[]
  include_mac_address?: string[]
  exclude_mac_address?: string[]
  auto_route?: boolean
  strict_route?: boolean
  auto_redirect?: boolean
  exclude_mptcp?: boolean
  auto_redirect_iproute2_fallback_rule_index?: number
  // auto_redirect_input_mark?: string
  // auto_redirect_output_mark?: string
  // route_address?: string[]
  // route_exclude_address?: string[]
  // include_interface?: string[]
  // exclude_interface?: string[]
  // include_uid?: string[]
  // include_uid_range?: string[]
  // exclude_uid?: number[]
  // exclude_uid_range?: string[]
  // include_android_user?: number[]
  // include_package?: string[]
  // exclude_package?: string[]
}
export interface SnellUser {
  name?: string
  userkey: string
}
// Snell picks its extra options from the version: v5 carries obfs, v6 a mode.
//
// Only v6 has a generated client config: sing-box's snell outbound speaks 4 and
// 6 while the inbound speaks 5 and 6, so a v5 listener is reachable by Surge,
// configured by hand, and by nothing the panel can write (see util.snellOut).
export interface Snell extends InboundBasics {
  version: 5 | 6
  psk: string
  users?: SnellUser[]
  obfs_mode?: 'none' | 'http' | 'tls'
  mode?: 'default' | 'unshaped' | 'unsafe-raw'
}
export interface Cloudflared extends InboundBasics {
  token: string
  ha_connections?: number
  protocol?: 'auto' | 'quic' | 'http2' | 'h2mux'
  post_quantum?: boolean
  edge_ip_version?: 0 | 4 | 6
  datagram_version?: 'v2' | 'v3'
  grace_period?: string
  region?: string
}
export interface Redirect extends InboundBasics {}
// Only tproxy carries these: RedirectInboundOptions is ListenOptions and
// nothing else.
export interface TProxy extends InboundBasics, UdpNatFields {
  network?: "udp" | "tcp"
}

// Create interfaces dynamically based on InTypes keys
type InterfaceMap = {
  direct: Direct
  mixed: Mixed
  socks: SOCKS
  http: HTTP
  shadowsocks: Shadowsocks
  snell: Snell
  vmess: VMess
  trojan: Trojan
  naive: Naive
  hysteria: Hysteria
  shadowtls: ShadowTLS
  tuic: TUIC
  hysteria2: Hysteria2
  vless: VLESS
  anytls: AnyTls
  tun: Tun
  redirect: Redirect
  tproxy: TProxy
  cloudflared: Cloudflared
}

// Create union type from InterfaceMap
export type Inbound = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<InType, Inbound> = {
  direct: <Direct>{ type: InTypes.Direct },
  mixed: <Mixed>{ type: InTypes.Mixed },
  socks: <SOCKS>{ type: InTypes.SOCKS },
  http: <HTTP>{ type: InTypes.HTTP, tls_id: 0 },
  shadowsocks: <Shadowsocks>{ type: InTypes.Shadowsocks, method: 'none' },
  snell: <Snell>{ type: InTypes.Snell, version: 6 },
  vmess: <VMess>{ type: InTypes.VMess, tls_id: 0, transport: {} },
  trojan: <Trojan>{ type: InTypes.Trojan, tls_id: 0, transport: {} },
  naive: <Naive>{ type: InTypes.Naive, tls_id: 0 },
  hysteria: <Hysteria>{ type: InTypes.Hysteria, up_mbps: 100, down_mbps: 100, tls_id: 0 },
  shadowtls: <ShadowTLS>{ type: InTypes.ShadowTLS, version: 3, handshake: { server_port: 443 }, handshake_for_server_name: {} },
  tuic: <TUIC>{ type: InTypes.TUIC, congestion_control: "cubic", tls_id: 0 },
  hysteria2: <Hysteria2>{ type: InTypes.Hysteria2, tls_id: 0 },
  vless: <VLESS>{ type: InTypes.VLESS, tls_id: 0, transport: {} },
  anytls: <AnyTls>{ type: InTypes.AnyTls, tls_id: 0, padding_scheme: [
    "stop=8",
    "0=30-30",
    "1=100-400",
    "2=400-500,c,500-1000,c,500-1000,c,500-1000,c,500-1000",
    "3=9-9,500-1000",
    "4=500-1000",
    "5=500-1000",
    "6=500-1000",
    "7=500-1000"
  ]},
  tun: <Tun>{ type: InTypes.Tun, mtu: 9000, stack: 'system', udp_timeout: '5m', auto_route: false },
  redirect: <Redirect>{ type: InTypes.Redirect },
  tproxy: <TProxy>{ type: InTypes.TProxy },
  cloudflared: <Cloudflared>{ type: InTypes.Cloudflared, token: '', protocol: 'auto' },
}

// Secrets sing-box requires on the inbound itself (as opposed to per-client
// credentials, which the client system fills in). They are generated whenever
// an inbound of that type is created so switching protocol never leaves an
// inbound that cannot start.
function generateSecrets(inbound: any) {
  switch (inbound.type) {
    case InTypes.Snell:
      // sing-box requires a psk of 12-255 bytes.
      if (!inbound.psk) inbound.psk = RandomUtil.randomSeq(32)
      break
    case InTypes.ShadowTLS:
      // Only v2 carries an inbound-level password; v3 uses users.
      if (inbound.version === 2 && !inbound.password) inbound.password = RandomUtil.randomSeq(16)
      break
  }
}

export function createInbound<T extends Inbound>(type: InType,json?: Partial<T>): Inbound {
  // Deep copy: a shallow spread hands every new inbound the very same nested
  // objects (handshake, transport, padding_scheme), so filling in one form
  // rewrites the defaults the next one starts from. The clone is annotated
  // because spreading JSON.parse's `any` would make the literal `any` too and
  // stop TypeScript from checking it.
  const base: Inbound = JSON.parse(JSON.stringify(defaultValues[type] ?? {}))
  const defaultObject: Inbound = { ...base, ...(json ?? {}) }
  generateSecrets(defaultObject)
  return defaultObject
}