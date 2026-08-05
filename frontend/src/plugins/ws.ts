// Panel push channel: one native WebSocket carries the topics the UI used to
// poll over HTTP ('load' 10s / 'status' 2s / 'stats' 10s). Deliberately
// websocket-only — there is no polling fallback, so while the socket is down
// the UI holds its last data until the reconnect loop gets through.
//
// The server answers every subscribe with an immediate snapshot, which makes
// resubscribe() double as "apply new params now" (status resource list, stats
// period) and keeps reconnects cheap (the load topic's lu gate skips the full
// config payload when nothing changed).
import HttpUtils from '@/plugins/httputil'

export interface LiveSub {
  stop(): void
  // Returns false when the socket was down and the request never left the
  // browser — the caller decides what to show instead of silently rendering
  // stale data under new labels.
  resubscribe(): boolean
  connected(): boolean
}

interface SubOptions {
  topic: 'load' | 'status' | 'stats'
  // Evaluated at every (re)subscribe so lu / resource lists stay current.
  params?: () => any
  onData: (data: any) => void
}

const RECONNECT_MAX_MS = 30000
// The load topic pushes at least every 10s while the panel runs; a socket
// that is open but silent this long is a half-open TCP carcass.
const WATCHDOG_MS = 35000
const WATCHDOG_TICK_MS = 5000
// A middlebox that completes the TCP handshake but swallows the HTTP upgrade
// leaves the socket in CONNECTING, where neither onopen nor onclose ever fires
// and the browser's own timeout is minutes away. Give up ourselves.
const HANDSHAKE_TIMEOUT_MS = 10000

const subs = new Set<SubOptions>()
let socket: WebSocket | null = null
let failures = 0
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let watchdog: ReturnType<typeof setInterval> | null = null
let lastMsgAt = 0

const wsUrl = () => {
  // BASE_URL is the runtime web-path setting injected by web.go ('/app/' only
  // as index.html's dev fallback) — never hardcode the path here.
  const base = (window as any).BASE_URL || '/app/'
  const proto = location.protocol === 'https:' ? 'wss://' : 'ws://'
  return proto + location.host + base + 'ws'
}

const send = (payload: any): boolean => {
  if (socket?.readyState !== WebSocket.OPEN) return false
  try {
    socket.send(JSON.stringify(payload))
    return true
  } catch {
    // The socket died between the readyState check and send; onclose recovers.
    return false
  }
}

const sendSubscribe = (sub: SubOptions): boolean =>
  send({ op: 'subscribe', topic: sub.topic, params: sub.params?.() ?? {} })

const stopWatchdog = () => {
  if (watchdog) {
    clearInterval(watchdog)
    watchdog = null
  }
}

const startWatchdog = () => {
  stopWatchdog()
  lastMsgAt = Date.now()
  watchdog = setInterval(() => {
    if (socket?.readyState === WebSocket.OPEN && Date.now() - lastMsgAt > WATCHDOG_MS) {
      socket.close() // onclose schedules the reconnect
    }
  }, WATCHDOG_TICK_MS)
}

const connect = () => {
  if (socket || subs.size === 0) return
  let s: WebSocket
  try {
    s = new WebSocket(wsUrl())
  } catch {
    scheduleReconnect()
    return
  }
  socket = s
  // Nothing else rescues a socket wedged in CONNECTING: the watchdog only
  // inspects OPEN sockets and connect() refuses to run while `socket` is set.
  const handshakeTimer = setTimeout(() => {
    if (socket === s && s.readyState === WebSocket.CONNECTING) {
      socket = null
      s.onclose = null // this close is ours; drive the retry directly
      try { s.close() } catch { /* already tearing down */ }
      scheduleReconnect()
    }
  }, HANDSHAKE_TIMEOUT_MS)
  s.onopen = () => {
    clearTimeout(handshakeTimer)
    if (socket !== s) return
    failures = 0
    startWatchdog()
    for (const sub of subs) sendSubscribe(sub)
  }
  s.onmessage = (ev) => {
    lastMsgAt = Date.now()
    let env: any
    try {
      env = JSON.parse(String(ev.data))
    } catch {
      return
    }
    if (!env?.topic) return
    for (const sub of [...subs]) {
      if (sub.topic === env.topic) sub.onData(env.data)
    }
  }
  s.onclose = () => {
    clearTimeout(handshakeTimer)
    if (socket !== s) return
    socket = null
    stopWatchdog()
    scheduleReconnect()
  }
  // Errors are always followed by close; nothing to do here.
  s.onerror = () => {}
}

const scheduleReconnect = () => {
  if (subs.size === 0 || reconnectTimer) return
  failures++
  // A failed handshake looks the same to the browser whether the backend is
  // down or the session cookie expired (401). Probe over HTTP every third
  // failure: an expired session hits httputil's "Invalid login" handler and
  // logs the browser out, which tears these subscriptions down via the router
  // guard.
  if (failures % 3 === 0) {
    HttpUtils.get('api/status', { r: 'sbd' })
  }
  const backoff = Math.min(RECONNECT_MAX_MS, 1000 * Math.pow(2, failures - 1))
  const delay = backoff * (0.7 + Math.random() * 0.6)
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null
    connect()
  }, delay)
}

// Cut the backoff short when the network comes back.
window.addEventListener('online', () => {
  if (subs.size === 0 || socket) return
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  connect()
})

const maybeTeardown = () => {
  if (subs.size > 0) return
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  failures = 0
  stopWatchdog()
  if (socket) {
    const s = socket
    socket = null
    s.onclose = null // a deliberate close must not trigger a reconnect
    s.close()
  }
}

// liveSubscribe opens the shared socket on the first subscription and closes
// it when the last one stops (logout navigates to /login, the router guard
// stops the load subscription, and the socket goes away with it).
export function liveSubscribe(opts: SubOptions): LiveSub {
  const sub: SubOptions = { ...opts }
  subs.add(sub)
  if (socket?.readyState === WebSocket.OPEN) sendSubscribe(sub)
  else connect()
  return {
    connected: () => socket?.readyState === WebSocket.OPEN,
    resubscribe() {
      return subs.has(sub) ? sendSubscribe(sub) : false
    },
    stop() {
      if (!subs.delete(sub)) return
      if (![...subs].some((other) => other.topic === sub.topic)) {
        send({ op: 'unsubscribe', topic: sub.topic })
      }
      maybeTeardown()
    },
  }
}
