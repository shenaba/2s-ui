import { Dial } from "./dial"

export interface tls {
  id: number
  name: string
  server: iTls
  client: oTls
}

export interface iTls {
  enabled?: boolean
  server_name?: string
  alpn?: string[]
  min_version?: string
  max_version?: string
  cipher_suites?: string[]
  curve_preferences?: string[]
  certificate?: string[]
  certificate_path?: string
  key?: string[]
  key_path?: string
  client_authentication?: string
  client_certificate?: string[]
  client_certificate_path?: string[]
  client_certificate_public_key_sha256?: string[]
  // A tag referencing an entry in the config's certificate_providers list.
  certificate_provider?: string
  ech?: ech
  reality?: reality
  store?: 'mozilla' | 'chrome'
  kernel_tx?: boolean
  kernel_rx?: boolean
}

// Certificate providers issue and renew the certificates a TLS config serves.
// Since sing-box 1.14 they are declared once at the top level of the config and
// referenced by tag from tls.certificate_provider, so several TLS configs can
// share one, and the panel edits them in a section of their own.
export const CertProviderTypes = {
  Acme: 'acme',
  Tailscale: 'tailscale',
  CloudflareOriginCA: 'cloudflare-origin-ca',
} as const

export type CertProviderType = typeof CertProviderTypes[keyof typeof CertProviderTypes]

interface certProviderBasics {
  type: CertProviderType
  tag: string
}

export interface acme extends certProviderBasics {
  type: 'acme'
  domain: string[]
  data_directory?: string
  default_server_name?: string
  email?: string
  provider?: string
  disable_http_challenge?: boolean
  disable_tls_alpn_challenge?: boolean
  alternative_http_port?: number
  alternative_tls_port?: number
  external_account?: {
    key_id: string
    mac_key: string
  }
  dns01_challenge?: {
    provider: string
    [key: string]: string
  }
  // Tag of a shared client in the config's http_clients list.
  http_client?: string
}

// Reads the certificate Tailscale issues for the node, so it carries no
// material of its own.
export interface tailscaleProvider extends certProviderBasics {
  type: 'tailscale'
  endpoint?: string
}

export interface originCaProvider extends certProviderBasics {
  type: 'cloudflare-origin-ca'
  domain: string[]
  data_directory?: string
  api_token?: string
  origin_ca_key?: string
  request_type?: 'origin-rsa' | 'origin-ecc'
  requested_validity?: number
  // Tag of a shared client in the config's http_clients list.
  http_client?: string
}

export type certProvider = acme | tailscaleProvider | originCaProvider

const defaultProviders: Record<CertProviderType, () => certProvider> = {
  'acme': () => <acme>{ type: 'acme', tag: '', domain: [] },
  'tailscale': () => <tailscaleProvider>{ type: 'tailscale', tag: '' },
  'cloudflare-origin-ca': () => <originCaProvider>{ type: 'cloudflare-origin-ca', tag: '', domain: [] },
}

// Switching type keeps the tag, since it names the provider rather than
// describing it, and drops every field belonging to the type being left.
export function createCertProvider(type: CertProviderType, tag?: string): certProvider {
  const provider = defaultProviders[type]()
  if (tag) provider.tag = tag
  return provider
}

export interface ech {
  enabled: boolean
  key?: string[]
  key_path?: string
}

interface realityHanshake extends Dial {
  server: string
  server_port: number
}

export interface reality {
  enabled: boolean
  handshake: realityHanshake
  private_key: string
  short_id: string[]
  max_time_difference?: string
}

export const defaultInTls: iTls = {
  alpn: ['h3', 'h2', 'http/1.1'],
  min_version: "1.2",
  max_version: "1.3",
  cipher_suites: [],
}

export interface oTls {
  enabled?: boolean
  disable_sni?: boolean
  server_name?: string
  insecure?: boolean
  alpn?: string[]
  min_version?: string
  max_version?: string
  cipher_suites?: string[]
  curve_preferences?: string[]
  certificate?: string
  certificate_path?: string
  certificate_public_key_sha256?: string[]
  client_certificate?: string[]
  client_certificate_path?: string
  client_key?: string[]
  client_key_path?: string
  fragment?: boolean
  fragment_fallback_delay?: string
  record_fragment?: boolean
  ech?: {
    enabled: boolean
    config?: string[]
    config_path?: string
    query_server_name?: string
  },
  utls?: {
    enabled: boolean
    fingerprint: string
  },
  reality?: {
    enabled: boolean
    public_key: string
    short_id: string
  }
}

export const defaultOutTls: oTls = {
  alpn: ['h3', 'h2', 'http/1.1'],
  min_version: "1.2",
  max_version: "1.3",
  cipher_suites: [],
  utls: {
    enabled: true,
    fingerprint: "chrome",
  },
  reality: {
    enabled: true,
    public_key: "",
    short_id: "",
  },
  ech: {
    enabled: true,
    config_path: "",
  }
}
// Option tables for the TLS forms. Kept here rather than in each form because
// three of them carried byte-identical copies under four different names
// (cipher_suites / cipherSuiteItems / cipherSuites / fingerprints); a new uTLS
// fingerprint or cipher from sing-box now lands in one place.
export const ALPN_OPTIONS = ['h3', 'h2', 'http/1.1']

export const TLS_VERSIONS = ['1.0', '1.1', '1.2', '1.3']

export const CIPHER_SUITES = [
  { title: 'RSA-AES128-CBC-SHA', value: 'TLS_RSA_WITH_AES_128_CBC_SHA' },
  { title: 'RSA-AES256-CBC-SHA', value: 'TLS_RSA_WITH_AES_256_CBC_SHA' },
  { title: 'RSA-AES128-GCM-SHA256', value: 'TLS_RSA_WITH_AES_128_GCM_SHA256' },
  { title: 'RSA-AES256-GCM-SHA384', value: 'TLS_RSA_WITH_AES_256_GCM_SHA384' },
  { title: 'AES128-GCM-SHA256', value: 'TLS_AES_128_GCM_SHA256' },
  { title: 'AES256-GCM-SHA384', value: 'TLS_AES_256_GCM_SHA384' },
  { title: 'CHACHA20-POLY1305-SHA256', value: 'TLS_CHACHA20_POLY1305_SHA256' },
  { title: 'ECDHE-ECDSA-AES128-CBC-SHA', value: 'TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA' },
  { title: 'ECDHE-ECDSA-AES256-CBC-SHA', value: 'TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA' },
  { title: 'ECDHE-RSA-AES128-CBC-SHA', value: 'TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA' },
  { title: 'ECDHE-RSA-AES256-CBC-SHA', value: 'TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA' },
  { title: 'ECDHE-ECDSA-AES128-GCM-SHA256', value: 'TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256' },
  { title: 'ECDHE-ECDSA-AES256-GCM-SHA384', value: 'TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384' },
  { title: 'ECDHE-RSA-AES128-GCM-SHA256', value: 'TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256' },
  { title: 'ECDHE-RSA-AES256-GCM-SHA384', value: 'TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384' },
  { title: 'ECDHE-ECDSA-CHACHA20-POLY1305-SHA256', value: 'TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256' },
  { title: 'ECDHE-RSA-CHACHA20-POLY1305-SHA256', value: 'TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256' },
]

export const UTLS_FINGERPRINTS = [
  { title: 'Chrome', value: 'chrome' },
  { title: 'Firefox', value: 'firefox' },
  { title: 'Microsoft Edge', value: 'edge' },
  { title: 'Apple Safari', value: 'safari' },
  { title: '360', value: '360' },
  { title: 'QQ', value: 'qq' },
  { title: 'Apple IOS', value: 'ios' },
  { title: 'Android', value: 'android' },
  { title: 'Random', value: 'random' },
  { title: 'Randomized', value: 'randomized' },
]
