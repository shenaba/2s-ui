import { Dial } from "./dial"
import { UdpNatFields } from "./inbounds"

export const EpTypes = {
  Wireguard: 'wireguard',
  Warp: 'warp',
  Tailscale: 'tailscale',
  OpenConnect: 'openconnect',
  OpenVPNClient: 'openvpn-client',
  OpenVPNServer: 'openvpn-server',
}

type EpType = typeof EpTypes[keyof typeof EpTypes]

interface EndpointBasics {
  id: number
  type: EpType
  tag: string
  // References a panel TLS config. Only the endpoint types that use TLS read
  // it; the panel core projects it into the shape that type accepts.
  tls_id?: number
}

export interface WgPeer {
  address: string
  port: number
  public_key: string
  pre_shared_key?: string
  allowed_ips?: string[]
  persistent_keepalive_interval?: number
  reserved?: number[]
}

export interface WireGuard extends EndpointBasics, Dial, UdpNatFields {
  system?: boolean
  name?: string
  mtu?: number
  address: string[]
  private_key: string
  listen_port: number
  peers: WgPeer[]
  udp_timeout?: string
  workers?: number
  ext: any
}

export interface Warp extends WireGuard {}

export interface Tailscale extends EndpointBasics, Dial {
  state_directory?: string
  auth_key?: string
  control_url?: string
  ephemeral?: boolean
  hostname?: string
  accept_routes?: boolean
  exit_node?: string
  exit_node_allow_lan_access?: boolean
  advertise_routes?: string[]
  advertise_exit_node?: boolean
  relay_server_port?: number
  relay_server_static_endpoints?: string[]
  system_interface?: boolean
  system_interface_name?: string
  system_interface_mtu?: number
  udp_timeout?: string
  listen_port?: number
  advertise_tags?: string[]
  taildrop_directory?: string
  // Serving SSH over the tailnet, with the three things it can be told not to
  // offer.
  ssh_server?: {
    enabled?: boolean
    disable_pty?: boolean
    disable_sftp?: boolean
    disable_forwarding?: boolean
  }
}

// OpenConnect and OpenVPN expose far more options than are modelled here.
// Anything not listed is preserved as-is when an endpoint is edited, so a
// config written by hand keeps working.
//
// None of them carry a `tls` block: these protocols each define their own TLS
// options, so the panel core projects the referenced TLS config (tls_id) into
// the shape the endpoint accepts instead of the UI writing one.
export interface OpenConnect extends EndpointBasics, Dial {
  server: string
  flavor?: 'anyconnect' | 'gp' | 'fortinet' | 'f5' | 'pulse' | 'nc'
  username?: string
  password?: string
  auth_group?: string
  cookie?: string
  name?: string
  system?: boolean
  mtu?: number
  no_udp?: boolean
  udp_timeout?: string
  ipv6_disabled?: boolean
  allow_insecure_crypto?: boolean
}

export interface OpenVPNClient extends EndpointBasics, Dial {
  server: string
  server_port: number
  mode?: 'tls' | 'static_key'
  // Required in static_key mode; in tls mode the server pushes them.
  address?: string[]
  peer_address?: string
  peer_address_ipv6?: string
  network?: 'udp' | 'udp4' | 'udp6' | 'tcp' | 'tcp4' | 'tcp6'
  username?: string
  password?: string
  name?: string
  system?: boolean
  mtu?: number
  cipher?: string
  auth?: string
  static_key_path?: string
  key_direction?: 'server' | 'client'
}

export interface OpenVPNServer extends EndpointBasics {
  listen: string
  listen_port: number
  mode?: 'tls' | 'static_key'
  network?: 'tcp' | 'udp'
  address: string[]
  max_clients?: number
  duplicate_cn?: boolean
  users?: { username: string; password: string }[]
  name?: string
  system?: boolean
  mtu?: number
  cipher?: string
  auth?: string
  static_key_path?: string
  key_direction?: 'server' | 'client'
}

// Create interfaces dynamically based on EpTypes keys
type InterfaceMap = {
  [Key in keyof typeof EpTypes]: {
    type: string
    [otherProperties: string]: any // You can add other properties as needed
  }
}

// Create union type from InterfaceMap
export type Endpoint = InterfaceMap[keyof InterfaceMap]

// Create defaultValues object dynamically
const defaultValues: Record<EpType, Endpoint> = {
  // No `ext` here on purpose: EndpointDrawer seeds it at each use site, since an
  // endpoint loaded from the API may not carry one either.
  wireguard: { type: EpTypes.Wireguard, address: ['10.0.0.2/32','fe80::2/128'], private_key: '', listen_port: 0 },
  warp: { type: EpTypes.Warp, address: [], private_key: '', listen_port: 0, mtu: 1420, peers: [{ address: '', port: 0, public_key: ''}] },
  tailscale: { type: EpTypes.Tailscale, domain_resolver: 'local' },
  openconnect: { type: EpTypes.OpenConnect, server: '', flavor: 'anyconnect', tls_id: 0 },
  'openvpn-client': { type: EpTypes.OpenVPNClient, server: '', server_port: 1194, mode: 'tls', network: 'udp', tls_id: 0 },
  'openvpn-server': { type: EpTypes.OpenVPNServer, mode: 'tls', network: 'udp', address: ['10.8.0.1/24'], tls_id: 0 },
}

export function createEndpoint<T extends Endpoint>(type: string,json?: Partial<T>): Endpoint {
  // Deep copy: a shallow spread hands every new endpoint the very same nested
  // address and peers arrays, so filling in one form rewrites the defaults the
  // next one starts from. The clone is annotated because spreading JSON.parse's
  // `any` would make the literal `any` too and stop TypeScript from checking it.
  const base: Endpoint = JSON.parse(JSON.stringify(defaultValues[type] ?? {}))
  const defaultObject: Endpoint = { ...base, ...(json || {}) }
  return defaultObject
}