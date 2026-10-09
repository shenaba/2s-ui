<template>
  <Modal :open="visible" :title="$t('ui.sessTitle')" :width="900" @close="$emit('close')">
    <div style="padding: 16px 20px;">
      <div class="sess-bar">
        <Chip color="brand"><span class="mono">{{ name }}</span></Chip>
        <span v-if="loaded" class="mono" style="font-size: 12.5px; color: var(--text-3);">{{ sessions.length }}</span>
        <div style="flex: 1;" />
        <SwitchLabel v-model="autoRefresh" :label="$t('ui.sessAutoRefresh')" />
        <Btn sm variant="subtle" :disabled="loading" @click="loadData">
          <Ico name="refresh" :size="14" /> {{ $t('ui.ipRefresh') }}
        </Btn>
      </div>

      <div v-for="(err, node) in errors" :key="node" class="sess-warn">
        {{ $t('ui.sessNodeError', { node, err }) }}
      </div>

      <div v-if="!loaded" style="padding: 20px 0;">
        <EmptyState icon="link" :title="$t('loading')" />
      </div>
      <div v-else-if="sessions.length == 0" style="padding: 20px 0;">
        <EmptyState icon="link" :title="$t('ui.sessNone')" />
      </div>
      <template v-else>
        <!-- desktop -->
        <div class="sess-table">
          <table class="dtable">
            <thead>
              <tr>
                <th v-if="hasNodes">{{ $t('ui.sessNode') }}</th>
                <th>{{ $t('ui.sessInbound') }}</th>
                <th>{{ $t('ui.sessSource') }}</th>
                <th>{{ $t('ui.sessDest') }}</th>
                <th>{{ $t('ui.sessOutbound') }}</th>
                <th style="text-align: end;">{{ $t('ui.sessUsage') }}</th>
                <th style="text-align: end;">{{ $t('ui.sessAge') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="s in shown" :key="(s.node ?? '') + s.id">
                <td v-if="hasNodes" class="cell">
                  <span class="trunc tag" :title="s.node || $t('ui.sessLocal')">{{ s.node || $t('ui.sessLocal') }}</span>
                </td>
                <td class="cell">
                  <span class="trunc tag" :title="s.inbound">{{ s.inbound }}</span>
                  <div class="sub">{{ s.network }}</div>
                </td>
                <td class="cell">
                  <span class="trunc addr mono" dir="ltr" :title="s.source">{{ s.source }}</span>
                </td>
                <td class="cell">
                  <span class="trunc addr mono" dir="ltr" :title="destTitle(s)">{{ s.domain || s.destination }}</span>
                  <div v-if="s.domain && s.destination && s.destination !== hostPort(s)" class="sub trunc addr mono" dir="ltr">{{ s.destination }}</div>
                </td>
                <td class="cell">
                  <span class="trunc tag" :title="s.outbound">{{ s.outbound }}</span>
                  <div v-if="s.rule" class="sub trunc tag" :title="s.rule">{{ s.rule }}</div>
                </td>
                <td class="cell" style="text-align: end;" :title="usageTitle(s)">
                  <span class="mono" style="font-weight: 600;">{{ size(s.up + s.down) }}</span>
                  <div class="sub mono" dir="ltr">↑{{ size(s.up) }} ↓{{ size(s.down) }}</div>
                </td>
                <td class="cell mono" style="text-align: end; white-space: nowrap;" :title="since(s.createdAt)">{{ age(s.createdAt) }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <!-- mobile ≤820px -->
        <div class="sess-cards">
          <div v-for="s in shown" :key="(s.node ?? '') + s.id" class="sess-card">
            <div class="mono trunc" dir="ltr" style="font-weight: 600; font-size: 13px;" :title="destTitle(s)">{{ s.domain || s.destination }}</div>
            <div class="sess-kv">
              <span>{{ $t('ui.sessInbound') }}</span>
              <span class="trunc">{{ s.inbound }} · {{ s.network }}<template v-if="hasNodes"> · {{ s.node || $t('ui.sessLocal') }}</template></span>
            </div>
            <div class="sess-kv"><span>{{ $t('ui.sessSource') }}</span><span class="mono trunc" dir="ltr">{{ s.source }}</span></div>
            <div class="sess-kv"><span>{{ $t('ui.sessOutbound') }}</span><span class="trunc">{{ s.outbound }}</span></div>
            <div class="sess-kv">
              <span>{{ $t('ui.sessUsage') }}</span>
              <span class="mono" dir="ltr">↑{{ size(s.up) }} ↓{{ size(s.down) }} · {{ age(s.createdAt) }}</span>
            </div>
          </div>
        </div>

        <div v-if="sessions.length > shown.length" class="sub" style="padding-top: 10px;">
          {{ $t('ui.sessCapped', { n: shown.length, total: sessions.length }) }}
        </div>
      </template>
    </div>

    <template #footer>
      <span class="sub" style="flex: 1; align-self: center;">{{ $t('ui.sessDisconnectHint') }}</span>
      <Btn sm :disabled="closing || sessions.length == 0" style="color: var(--rose);" @click="disconnect">
        {{ $t('ui.sessDisconnect') }}
      </Btn>
    </template>
  </Modal>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { push } from 'notivue'
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import { i18n, intlLocale } from '@/locales'
import Modal from '@/components/ui/Modal.vue'
import Chip from '@/components/ui/Chip.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

const props = defineProps<{
  visible: boolean
  name: string
}>()
defineEmits<{ close: [] }>()

type Session = {
  id: string
  inbound?: string
  user?: string
  outbound?: string
  network: string
  source?: string
  destination?: string
  domain?: string
  rule?: string
  createdAt: number
  up: number
  down: number
  node?: string
}

// A busy client can hold thousands of connections; the newest are the ones an
// operator is looking for, and rendering all of them every 5s is the cost.
const MAX_ROWS = 300
const REFRESH_MS = 5000
const AUTO_KEY = '2sui-sessions-auto'

const loading = ref(false)
const loaded = ref(false)
const closing = ref(false)
const sessions = ref<Session[]>([])
const errors = ref<Record<string, string>>({})
const shown = computed(() => sessions.value.slice(0, MAX_ROWS))
const hasNodes = computed(() => sessions.value.some((s) => s.node) || Object.keys(errors.value).length > 0)

const readAuto = (): boolean => {
  try { return localStorage.getItem(AUTO_KEY) === 'true' } catch { return false }
}
const autoRefresh = ref(readAuto())
// The age column counts from this, so it moves between fetches too.
const now = ref(Date.now())

// Same request-sequence guard as ClientIpsModal: a slow response for a closed
// or reopened modal must not land on the current one.
let issued = 0
let awaited = 0
const loadData = async () => {
  if (!props.name) return
  const seq = ++issued
  awaited = seq
  loading.value = true
  const msg = await HttpUtils.get('api/sessions', { resource: 'user', tag: props.name })
  if (awaited !== seq) return
  loading.value = false
  loaded.value = true
  now.value = Date.now()
  sessions.value = msg.success ? (msg.obj?.sessions ?? []) : []
  errors.value = msg.success ? (msg.obj?.errors ?? {}) : {}
}

const disconnect = async () => {
  closing.value = true
  const msg = await HttpUtils.post('api/closeSessions', { u: props.name })
  closing.value = false
  if (!msg.success) return
  const r = msg.obj ?? {}
  push.success({ message: i18n.global.t('ui.sessDisconnected', { n: (r.connections ?? 0) + (r.sessions ?? 0) }) })
  if ((r.unclosable ?? 0) > 0) push.warning({ message: i18n.global.t('ui.sessQuicLeft') })
  for (const [node, err] of Object.entries(r.errors ?? {})) {
    push.error({ message: i18n.global.t('ui.sessNodeError', { node, err }) })
  }
  loadData()
}

let timer: ReturnType<typeof setInterval> | undefined
const stopTimer = () => {
  if (timer) clearInterval(timer)
  timer = undefined
}
const syncTimer = () => {
  stopTimer()
  if (props.visible && autoRefresh.value) timer = setInterval(loadData, REFRESH_MS)
}

watch(() => props.visible, (open) => {
  if (open) {
    loadData()
  } else {
    awaited = ++issued
    loading.value = false
    loaded.value = false
    sessions.value = []
    errors.value = {}
  }
  syncTimer()
})
watch(autoRefresh, (v) => {
  try { localStorage.setItem(AUTO_KEY, v ? 'true' : 'false') } catch { /* private mode */ }
  syncTimer()
})
onBeforeUnmount(stopTimer)

const size = (v: number) => HumanReadable.sizeFormat(v ?? 0)

// The destination's own host:port, to tell whether the second line would only
// repeat the first.
const hostPort = (s: Session) => s.domain && s.destination
  ? s.domain + s.destination.slice(s.destination.lastIndexOf(':'))
  : ''
const destTitle = (s: Session) =>
  [s.domain, s.destination, s.network].filter((v) => v).join('\n')

const usageTitle = (s: Session) =>
  `${i18n.global.t('stats.upload')}: ${size(s.up)}\n${i18n.global.t('stats.download')}: ${size(s.down)}`

const age = (createdAt: number): string => {
  const sec = Math.max(0, Math.floor(now.value / 1000) - createdAt)
  if (sec < 60) return `${sec}s`
  if (sec < 3600) return `${Math.floor(sec / 60)}m`
  if (sec < 86400) return `${Math.floor(sec / 3600)}h ${Math.floor((sec % 3600) / 60)}m`
  return `${Math.floor(sec / 86400)}d ${Math.floor((sec % 86400) / 3600)}h`
}
const since = (createdAt: number): string =>
  new Date(createdAt * 1000).toLocaleString(intlLocale())
</script>

<style scoped>
.sess-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}
.sess-warn {
  font-size: 12.5px;
  color: var(--amber);
  background: color-mix(in srgb, var(--amber) 10%, transparent);
  border: 1px solid color-mix(in srgb, var(--amber) 26%, transparent);
  border-radius: 8px;
  padding: 8px 10px;
  margin-bottom: 10px;
}
.sess-table {
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-x: auto;
}
.sess-table .cell {
  padding-top: 8px;
  padding-bottom: 8px;
  font-size: 12.5px;
  vertical-align: top;
}
.trunc {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
.trunc.addr {
  max-width: 190px;
}
.trunc.tag {
  max-width: 120px;
}
.sub {
  font-size: 11.5px;
  color: var(--text-3);
}
div.sub.trunc {
  display: block;
}
.sess-cards {
  display: none;
  flex-direction: column;
  gap: 8px;
}
.sess-card {
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--surface-3);
  min-width: 0;
}
.sess-kv {
  display: flex;
  gap: 10px;
  font-size: 12px;
  padding-top: 4px;
  min-width: 0;
}
.sess-kv > span:first-child {
  flex: none;
  color: var(--text-3);
}
.sess-kv > span:last-child {
  margin-inline-start: auto;
  min-width: 0;
  text-align: end;
}
@media (max-width: 820px) {
  .sess-table { display: none; }
  .sess-cards { display: flex; }
}
</style>
