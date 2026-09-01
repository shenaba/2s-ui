import { Dial } from './dial'
import { oTls } from './tls'

// Transport tuning shared by HTTP/2 and QUIC. sing-box flattens these into
// whatever carries them, so they sit beside the other options rather than in a
// nested object. Byte sizes take a number or a string with a unit ("8mb").
export interface Http2Fields {
  idle_timeout?: string
  keep_alive_period?: string
  stream_receive_window?: number | string
  connection_receive_window?: number | string
  max_concurrent_streams?: number
}

// QUIC carries every HTTP/2 field plus two of its own.
export interface QuicFields extends Http2Fields {
  initial_packet_size?: number
  disable_path_mtu_discovery?: boolean
}

export const httpVersions = [0, 1, 2, 3] as const
export type HttpVersion = typeof httpVersions[number]

// Which field group applies is decided by the HTTP version: 1 has neither, 3
// is QUIC, and everything else is HTTP/2.
export function httpFieldGroup(version?: HttpVersion): 'none' | 'http2' | 'quic' {
  if (version === 1) return 'none'
  if (version === 3) return 'quic'
  return 'http2'
}

// The options an HTTP client can carry, as accepted inline wherever one is
// referenced.
export interface HttpClientOptions extends Dial, QuicFields {
  engine?: 'go' | 'apple'
  version?: HttpVersion
  disable_version_fallback?: boolean
  headers?: Record<string, string | string[]>
  tls?: oTls
}

// A shared client declared in the config's http_clients list.
export interface HttpClient extends HttpClientOptions {
  tag: string
}

// Every place that accepts an http_client takes either the tag of a shared
// client or the options inline.
export type HttpClientRef = string | HttpClientOptions

export function createHttpClient(tag: string): HttpClient {
  return { tag }
}
