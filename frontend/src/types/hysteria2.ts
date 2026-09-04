// Shapes hysteria2 carries on both sides of the connection, so the inbound and
// outbound types and their forms share one definition rather than two that
// drift.

// sing-box flattens the gecko-only sizes into the obfs object rather than
// nesting them, which is what its custom MarshalJSON does with GeckoOptions.
export interface Hysteria2Obfs {
  type: 'salamander' | 'gecko'
  password?: string
  min_packet_size?: number
  max_packet_size?: number
}

// UPnP / NAT-PMP mapping the client maintains on its own gateway while it holds
// a realm session.
export interface Hysteria2RealmPortMapping {
  enabled?: boolean
  timeout?: string
  lifetime?: string
}

// The rendezvous point for NAT traversal: a server behind NAT registers its
// STUN-discovered addresses with the realm, and a client asks the realm for
// them before hole-punching. This is the client half of the hysteria-realm
// service the panel can run under Services -- server_url points at one, and
// realm_id names the rendezvous both sides meet at.
//
// http_client and (on an inbound) stun_domain_resolver are left out: both are
// references to objects the protocol forms are not handed, so they would need
// tags threaded through the drawers first.
export interface Hysteria2Realm {
  server_url: string
  realm_id: string
  token?: string
  stun_servers?: string[]
  // 0 leaves it to sing-box; 4 and 6 restrict realm connections to one family
  // and are refused when they contradict the inbound's own listen address.
  ip_version?: 0 | 4 | 6
  port_mapping?: Hysteria2RealmPortMapping
}

export const bbrProfiles = ['standard', 'conservative', 'aggressive'] as const
export type BBRProfile = typeof bbrProfiles[number]

export function createHysteria2Realm(): Hysteria2Realm {
  return { server_url: '', realm_id: '' }
}
