<script setup lang="ts">
import { ref, computed, onMounted, watchEffect } from 'vue'
import { LANGS, useI18n, type Lang } from './i18n'

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

const { lang, t, days, setLang } = useI18n()

const loading = ref(true)
// A flag rather than the message, so the text follows a language switch.
const error = ref(false)
const info = ref<ClientInfo | null>(null)
const copied = ref<number>(-1)

// The page is served at /{subPath}/{clientName}; the info payload lives at the
// same path with ?format=info.
const infoUrl = window.location.pathname + '?format=info'

async function load() {
  loading.value = true
  error.value = false
  try {
    const res = await fetch(infoUrl, { headers: { Accept: 'application/json' } })
    if (!res.ok) throw new Error('HTTP ' + res.status)
    info.value = (await res.json()) as ClientInfo
  } catch (e) {
    error.value = true
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
    mierus: 'Mieru',
  }
  return map[p] || p
}

const usagePercent = computed(() => {
  if (!info.value || info.value.unlimited || info.value.volume <= 0) return 0
  const p = (info.value.used / info.value.volume) * 100
  return Math.min(p, 100)
})

const status = computed(() => {
  if (error.value) return { label: t.value.unknown, klass: 'unknown' }
  if (!info.value) return { label: '…', klass: 'unknown' }
  // expired comes from the panel, not from remainingDays: that one is an
  // integer division, so it reads 0 for the whole final day of a subscription
  // that still works.
  if (info.value.expired) return { label: t.value.expired, klass: 'expired' }
  if (!info.value.enable) return { label: t.value.disabled, klass: 'expired' }
  return { label: t.value.active, klass: 'active' }
})

watchEffect(() => {
  document.title = info.value?.title || t.value.subscription
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
    <div class="toolbar">
      <label class="lang">
        <svg class="lang-icon" viewBox="0 0 24 24" aria-hidden="true">
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9s1.3-6.4 3.8-9z" />
        </svg>
        <select
          class="lang-select"
          :value="lang"
          :aria-label="t.language"
          @change="setLang(($event.target as HTMLSelectElement).value as Lang)"
        >
          <option v-for="l in LANGS" :key="l.value" :value="l.value">{{ l.title }}</option>
        </select>
      </label>
    </div>
    <div class="card">
      <header class="head">
        <div class="logo">2s</div>
        <div>
          <h1 class="title">{{ info?.title || t.subscription }}</h1>
          <p class="subtitle" v-if="info && info.title !== info.name">{{ info.name }}</p>
        </div>
        <span class="pill" :class="status.klass">{{ status.label }}</span>
      </header>

      <div v-if="loading" class="state">{{ t.loading }}</div>
      <div v-else-if="error" class="state error">{{ t.loadFailed }}</div>

      <!-- A disabled client is answered with its state and nothing else, so
           there are no figures to show: rendering the block anyway would put
           zeroed counters on screen, and volume 0 reads as "unlimited". -->
      <template v-else-if="info && !info.enable">
        <section class="hint">{{ t.inactive }}</section>
      </template>

      <template v-else-if="info">
        <section class="stats">
          <div class="stat">
            <span class="stat-label">{{ t.remaining }}</span>
            <!-- Keyed on expiry, not on unlimited: that flag is about traffic
                 volume, so an unmetered client with an expiry date used to be
                 shown as never expiring. -->
            <span class="stat-value">
              {{ info.expiry > 0 ? days(info.remainingDays) : '♾ ' + t.unlimited }}
            </span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t.used }}</span>
            <span class="stat-value">{{ formatBytes(info.used) }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t.total }}</span>
            <span class="stat-value">{{ info.unlimited ? '♾ ' + t.unlimited : formatBytes(info.volume) }}</span>
          </div>
          <div class="stat">
            <span class="stat-label">{{ t.left }}</span>
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
          <h2 class="links-title">{{ t.configLinks }}</h2>
          <ul class="link-list">
            <li v-for="(link, i) in info.links" :key="i" class="link-item">
              <span class="proto">{{ protocolName(link) }}</span>
              <!-- A URI reads left to right whatever the page direction. -->
              <span class="uri" dir="ltr" :title="link">{{ link }}</span>
              <button class="copy" @click="copyLink(link, i)">
                {{ copied === i ? t.copied : t.copy }}
              </button>
            </li>
          </ul>
          <p class="hint">{{ t.linksHint }}</p>
        </section>
        <section v-else class="hint">{{ t.noLinks }}</section>
      </template>
    </div>
    <footer class="foot">{{ t.poweredBy }}</footer>
  </main>
</template>
