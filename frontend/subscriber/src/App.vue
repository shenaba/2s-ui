<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'

interface ClientInfo {
  name: string
  remark?: string
  title: string
  enable: boolean
  expiry: number
  remainingDays: number
  volume: number
  up: number
  down: number
  used: number
  remainingTraffic: number
  unlimited: boolean
  expired: boolean
  links: string[]
}

const loading = ref(true)
const error = ref('')
const info = ref<ClientInfo | null>(null)
const copied = ref<number>(-1)

// The page is served at /{subPath}/{clientName}; the info payload lives at the
// same path with ?format=info.
const infoUrl = window.location.pathname + '?format=info'

async function load() {
  loading.value = true
  error.value = ''
  try {
    const res = await fetch(infoUrl, { headers: { Accept: 'application/json' } })
    if (!res.ok) throw new Error('HTTP ' + res.status)
    info.value = (await res.json()) as ClientInfo
  } catch (e) {
    error.value = 'Could not load subscription info.'
  } finally {
    loading.value = false
  }
}

function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 2) + ' ' + units[i]
}

function protocolName(uri: string): string {
  const p = uri.split('://')[0] || 'link'
  const map: Record<string, string> = {
    vmess: 'VMess',
    vless: 'VLESS',
    trojan: 'Trojan',
    ss: 'SS',
    shadowsocks: 'Shadowsocks',
    wiresguard: 'WireGuard',
    hysteria2: 'Hysteria2',
    tuic: 'TUIC',
  }
  return map[p] || p
}

const usagePercent = computed(() => {
  if (!info.value || info.value.unlimited || info.value.volume <= 0) return 0
  const p = (info.value.used / info.value.volume) * 100
  return Math.min(p, 100)
})

const status = computed(() => {
  if (error.value) return { label: 'Unknown', klass: 'unknown' }
  if (!info.value) return { label: '…', klass: 'unknown' }
  // expired comes from the panel, not from remainingDays: that one is an
  // integer division, so it reads 0 for the whole final day of a subscription
  // that still works.
  if (info.value.expired) return { label: 'Expired', klass: 'expired' }
  if (!info.value.enable) return { label: 'Disabled', klass: 'expired' }
  return { label: 'Active', klass: 'active' }
})

async function copyLink(uri: string, idx: number) {
  try {
    await navigator.clipboard.writeText(uri)
    copied.value = idx
    setTimeout(() => (copied.value = -1), 1500)
  } catch {
    // Clipboard may be unavailable (e.g. plain-HTTP context); fall back to
    // selecting the text via a temporary textarea.
    const ta = document.createElement('textarea')
    ta.value = uri
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
    copied.value = idx
    setTimeout(() => (copied.value = -1), 1500)
  }
}

onMounted(load)
</script>

<template>
  <main class="page">
    <div class="card">
      <header class="head">
        <div class="logo">2s</div>
        <div>
          <h1 class="title">{{ info?.title || 'Subscription' }}</h1>
          <p class="subtitle" v-if="info && info.title !== info.name">{{ info.name }}</p>
        </div>
        <span class="pill" :class="status.klass">{{ status.label }}</span>
      </header>

      <div v-if="loading" class="state">Loading…</div>
      <div v-else-if="error" class="state error">{{ error }}</div>

      <template v-else-if="info">
        <section class="stats">
          <div class="stat">
            <span class="stat-label">Remaining</span>
            <!-- Keyed on expiry, not on unlimited: that flag is about traffic
                 volume, so an unmetered client with an expiry date used to be
                 shown as never expiring. -->
            <span class="stat-value">
              {{ info.expiry > 0 ? info.remainingDays + ' days' : '♾ Unlimited' }}
            </span>
          </div>
          <div class="stat">
            <span class="stat-label">Used</span>
            <span class="stat-value">{{ formatBytes(info.used) }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">Total</span>
            <span class="stat-value">{{ info.unlimited ? '♾ Unlimited' : formatBytes(info.volume) }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">Left</span>
            <span class="stat-value">{{ info.unlimited ? '♾' : formatBytes(info.remainingTraffic) }}</span>
          </div>
        </section>

        <section v-if="!info.unlimited && info.volume > 0" class="usage">
          <div class="usage-row">
            <span class="usage-up">↑ {{ formatBytes(info.up) }}</span>
            <span class="usage-down">↓ {{ formatBytes(info.down) }}</span>
          </div>
          <div class="bar">
            <div class="bar-fill" :style="{ width: usagePercent + '%' }"></div>
          </div>
        </section>

        <section class="links" v-if="info.links && info.links.length">
          <h2 class="links-title">Config Links</h2>
          <ul class="link-list">
            <li v-for="(link, i) in info.links" :key="i" class="link-item">
              <span class="proto">{{ protocolName(link) }}</span>
              <span class="uri" :title="link">{{ link }}</span>
              <button class="copy" @click="copyLink(link, i)">
                {{ copied === i ? 'Copied ✓' : 'Copy' }}
              </button>
            </li>
          </ul>
          <p class="hint">Copy a link and paste it into your client app, or import the subscription URL directly.</p>
        </section>
        <section v-else class="hint">
          {{ info.enable ? 'No config links available.' : 'This subscription is not active. Contact your provider.' }}
        </section>
      </template>
    </div>
    <footer class="foot">Powered by 2s-ui</footer>
  </main>
</template>
