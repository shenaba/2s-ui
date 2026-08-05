<template>
  <LogsModal :visible="logsOpen" @close="logsOpen = false" />
  <BackupModal :visible="backupOpen" @close="backupOpen = false" />
  <UsageStatsModal :visible="usageOpen" @close="usageOpen = false" />

  <div class="page-stack-lg fade-up">
    <!-- toolbar -->
    <div class="toolbar">
      <Btn sm @click="backupOpen = true"><Ico name="copy" :size="15" /> {{ $t('main.backup.title') }}</Btn>
      <Btn sm @click="logsOpen = true"><Ico name="list" :size="15" /> {{ $t('basic.log.title') }}</Btn>
      <Btn sm @click="usageOpen = true"><Ico name="chart" :size="15" /> {{ $t('main.stats.title') }}</Btn>
      <div class="grow" />
      <TilesMenu :show="tiles" @toggle="toggleTile" @reset="resetTiles" />
    </div>

    <!-- top overview: resources / server / key metrics -->
    <div v-if="topCount > 0" class="top-grid" :style="{ '--top-cols': topCount }">
      <DPanel v-if="tiles.resources" :title="$t('ui.systemResources')">
        <div class="res-grid">
          <Gauge :label="$t('ui.cpu')" :value="gCpu.pct" color="var(--brand)" :size="76" />
          <Gauge :label="$t('ui.memory')" :value="gMem.pct" color="var(--cyan)" :sub="gMem.sub" :size="76" />
          <Gauge :label="$t('ui.disk')" :value="gDsk.pct" color="var(--violet)" :sub="gDsk.sub" :size="76" />
          <Gauge :label="$t('ui.swap')" :value="gSwp.pct" color="var(--emerald)" :sub="gSwp.sub" :size="76" />
        </div>
      </DPanel>

      <DPanel v-if="tiles.server" :title="$t('ui.server')" :sub="sys.hostName ?? ''" :pad="0">
        <div class="srv-grid">
          <div><div class="srv-k">IPv4</div><div class="srv-v mono">{{ sys.ipv4?.[0] ?? '—' }}</div></div>
          <div><div class="srv-k">{{ $t('ui.cpu') }}</div><div class="srv-v" :title="sys.cpuType">{{ (sys.cpuCount ?? '—') + ' cores' }}</div></div>
          <div><div class="srv-k">{{ $t('ui.processMem') }}</div><div class="srv-v mono">{{ HumanReadable.sizeFormat(sbd.stats?.Alloc ?? 0) }}</div></div>
          <div><div class="srv-k">{{ $t('ui.uptime') }}</div><div class="srv-v mono">{{ uptime }}</div></div>
          <div><div class="srv-k">{{ $t('ui.panelLbl') }}</div><div class="srv-v mono" style="color: var(--brand);">{{ 'v' + (sys.appVersion ?? '—') }}</div></div>
          <div><div class="srv-k">{{ $t('ui.kernel') }}</div><div class="srv-v mono" :title="sbVersion">{{ sbVersion }}</div></div>
        </div>
        <div class="srv-status">
          <Chip v-if="sbd.running" color="emerald" dot>{{ $t('ui.singboxRunning') }}</Chip>
          <Chip v-else color="rose" dot>sing-box · {{ $t('main.info.running') }}: {{ $t('no') }}</Chip>
          <Btn variant="subtle" sm style="margin-inline-start: auto;" :loading="restarting" @click="restartSb">
            <Ico name="refresh" :size="14" /> {{ $t('ui.restart') }}
          </Btn>
        </div>
      </DPanel>

      <DPanel v-if="tiles.keymetrics" :title="$t('ui.keymetrics')">
        <template #right>
          <Chip color="emerald" dot>{{ $t('ui.live') }}</Chip>
        </template>
        <div class="km-grid">
          <MetricItem icon="clients" :label="$t('ui.onlineClients')" :value="String(onlineUsers)" accent="var(--emerald)" />
          <MetricItem icon="download" :label="$t('stats.download')" :value="HumanReadable.sizeFormat(totalDown)" accent="var(--cyan)" />
          <MetricItem icon="upload" :label="$t('stats.upload')" :value="HumanReadable.sizeFormat(totalUp)" accent="var(--violet)" />
          <MetricItem icon="inbound" :label="$t('ui.activeInbounds')" :value="`${onlineInbounds}/${data.inbounds.length}`" accent="var(--brand)" />
        </div>
      </DPanel>
    </div>

    <!-- traffic + protocol + network throughput (one row) -->
    <div v-if="mainCount > 0" class="main-grid" :style="{ '--main-cols': mainCount }">
      <DPanel v-if="tiles.traffic" :title="$t('ui.traffic')" :sub="$t('ui.trafficSub')">
        <template #right>
          <Chip color="emerald" dot>{{ $t('ui.live') }}</Chip>
        </template>
        <div style="display: flex; gap: 22px; margin-bottom: 12px; flex-wrap: wrap;">
          <Legend color="var(--brand)" :label="$t('stats.download')" :value="HumanReadable.sizeFormat(netInNow) + '/s'" />
          <Legend color="var(--emerald)" :label="$t('stats.upload')" :value="HumanReadable.sizeFormat(netOutNow) + '/s'" dashed />
        </div>
        <AreaChart :data="buf.netIn" :data2="buf.netOut" :height="180" :labels="trafficLabels" :value-formatter="netFmt" />
        <div class="mono" style="display: flex; justify-content: space-between; margin-top: 8px; font-size: 10.5px; color: var(--text-3);">
          <span v-for="l in trafficAxis" :key="l">{{ l }}</span>
        </div>
      </DPanel>

      <DPanel v-if="tiles.protocol" :title="$t('ui.protocolMix')" :sub="$t('ui.protocolSub')">
        <div v-if="protoMix.length === 0" style="font-size: 12.5px; color: var(--text-3);">{{ $t('noData') }}</div>
        <div v-else style="display: flex; align-items: center; gap: 18px; flex-wrap: wrap;">
          <div style="position: relative; flex: none;">
            <Donut :data="protoMix" :size="146" :thickness="17" />
            <div style="position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center;">
              <div class="mono" style="font-size: 22px; font-weight: 700; line-height: 1;">{{ onlineUsers }}</div>
              <div style="font-size: 10.5px; color: var(--text-3);">{{ $t('ui.sessions') }}</div>
            </div>
          </div>
          <div style="flex: 1; display: flex; flex-direction: column; gap: 9px; min-width: 120px;">
            <div v-for="p in protoMix" :key="p.name" style="display: flex; align-items: center; gap: 8px; font-size: 12.5px;">
              <span :style="{ width: '8px', height: '8px', borderRadius: '3px', background: p.color, flex: 'none' }" />
              <span style="flex: 1; color: var(--text-2); font-weight: 600;">{{ p.name }}</span>
              <span class="mono" style="color: var(--text-3);">{{ p.pct }}%</span>
            </div>
          </div>
        </div>
      </DPanel>

      <DPanel v-if="tiles.network" :title="$t('ui.networkThroughput')" :sub="`↓ ${HumanReadable.sizeFormat(netInNow)}/s · ↑ ${HumanReadable.sizeFormat(netOutNow)}/s`">
        <div class="net-grid">
          <div>
            <div style="display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-3); margin-bottom: 4px;">
              <span style="width: 8px; height: 8px; border-radius: 3px; background: var(--cyan);" />{{ $t('ui.inboundLbl') }}
            </div>
            <AreaChart :data="buf.netIn" color="var(--cyan)" :height="96" :labels="trafficLabels" :value-formatter="netFmt" />
          </div>
          <div>
            <div style="display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-3); margin-bottom: 4px;">
              <span style="width: 8px; height: 8px; border-radius: 3px; background: var(--violet);" />{{ $t('ui.outboundLbl') }}
            </div>
            <AreaChart :data="buf.netOut" color="var(--violet)" :height="96" :labels="trafficLabels" :value-formatter="netFmt" />
          </div>
        </div>
      </DPanel>
    </div>

    <!-- managed nodes (hidden entirely until at least one node exists) -->
    <DPanel v-if="tiles.nodes && data.nodes.length" :title="$t('ui.nodesTile')" :sub="$t('ui.nodesTileSub')" :pad="0">
      <template #right>
        <Btn variant="subtle" sm @click="$router.push('/nodes')">{{ $t('ui.viewAll') }} <Ico name="chevron" :size="14" /></Btn>
      </template>
      <div>
        <div
          v-for="(n, i) in data.nodes"
          :key="n.id"
          :style="{ display: 'flex', alignItems: 'center', gap: '11px', padding: '11px 20px', borderTop: i ? '1px solid var(--line)' : 'none' }"
        >
          <span :style="{ width: '8px', height: '8px', borderRadius: '50%', background: nodeDotColor(n), flex: 'none', boxShadow: `0 0 0 4px color-mix(in srgb, ${nodeDotColor(n)} 18%, transparent)` }" />
          <span style="font-weight: 700; font-size: 13px;">{{ n.name }}</span>
          <span dir="ltr" style="font-size: 11.5px; color: var(--text-3); min-width: 0; overflow: hidden; text-overflow: ellipsis;">{{ String(n.baseUrl).replace(/^https?:\/\//, '') }}</span>
          <span class="mono" dir="ltr" style="margin-inline-start: auto; font-size: 11.5px;" :style="{ color: nodeRightColor(n) }">{{ nodeRightLabel(n) }}</span>
        </div>
      </div>
    </DPanel>

    <!-- activity -->
    <DPanel v-if="tiles.activity" :title="$t('ui.activity')" :sub="$t('ui.activitySub')" :pad="0">
      <template #right>
        <Btn variant="subtle" sm @click="logsOpen = true">{{ $t('ui.viewLog') }} <Ico name="chevron" :size="14" /></Btn>
      </template>
      <div>
        <div v-if="changes.length === 0" style="padding: 16px 20px; font-size: 12.5px; color: var(--text-3);">{{ $t('noData') }}</div>
        <div
          v-for="(e, i) in changes"
          :key="e.id"
          :style="{ display: 'flex', alignItems: 'center', gap: '13px', padding: '12px 20px', borderTop: i ? '1px solid var(--line)' : 'none', flexWrap: 'wrap' }"
        >
          <span :style="{ width: '8px', height: '8px', borderRadius: '50%', background: actColor(e.action), flex: 'none', boxShadow: `0 0 0 4px color-mix(in srgb, ${actColor(e.action)} 18%, transparent)` }" />
          <span style="font-weight: 700; font-size: 13px;">{{ e.actor }}</span>
          <span style="font-size: 13px; color: var(--text-2); min-width: 0;">{{ actionLabel(e.action) }}</span>
          <Chip v-if="e.key" style="height: 22px;">{{ objectLabel(e.key) }}</Chip>
          <span class="mono" dir="ltr" style="margin-inline-start: auto; font-size: 11.5px; color: var(--text-3);">{{ e.time }}</span>
        </div>
      </div>
    </DPanel>

    <EmptyState
      v-if="allHidden"
      icon="settings"
      :title="$t('ui.tilesTitle')"
      :desc="$t('ui.customize')"
    />
  </div>
</template>

<script lang="ts" setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HttpUtils from '@/plugins/httputil'
import { liveSubscribe, type LiveSub } from '@/plugins/ws'
import { HumanReadable } from '@/plugins/utils'
import Data from '@/store/modules/data'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import DPanel from '@/components/ui/DPanel.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import AreaChart from '@/components/charts/AreaChart.vue'
import { requestChartRelayout } from '@/components/charts/useChart'
import Donut from '@/components/charts/Donut.vue'
import TilesMenu from './dashboard/TilesMenu.vue'
import Legend from './dashboard/Legend.vue'
import Gauge from '@/components/charts/Gauge.vue'
import MetricItem from './dashboard/MetricItem.vue'
import LogsModal from '@/layouts/drawers/LogsModal.vue'
import BackupModal from '@/layouts/drawers/BackupModal.vue'
import UsageStatsModal from '@/layouts/drawers/UsageStatsModal.vue'
import { loadTiles, saveTiles, defaultTiles } from './dashboard/tiles'

const { t, te, locale } = useI18n({ useScope: 'global' })
const data = Data()

/* ---------- tiles visibility ---------- */
const tiles = reactive(loadTiles())
const toggleTile = (id: string) => { tiles[id] = !tiles[id]; saveTiles(tiles) }
const resetTiles = () => { Object.assign(tiles, defaultTiles()); saveTiles(tiles) }
const topCount = computed(() => ['resources', 'server', 'keymetrics'].filter((i) => tiles[i]).length)
const mainCount = computed(() => ['traffic', 'protocol', 'network'].filter((i) => tiles[i]).length)
const allHidden = computed(() => Object.values(tiles).every((v) => !v))

// 卡片显隐会改变同一行图表所在 grid 列的宽度（如关掉「流量」后「网络吞吐」变宽、再开回又变窄），
// ECharts 不会自动跟随父级 grid 重排。布局变化后等 DOM 更新完，广播一次重排让所有图表按新列宽重测，
// 否则放大/缩小后 SVG 不跟随会溢出卡片边框（issue #15）。
watch([topCount, mainCount], () => { nextTick(requestChartRelayout) })

/* ---------- nodes tile ---------- */
const nodeStateOf = (n: any): string => {
  if (!n.enable) return 'disabled'
  return data.nodesStatus[n.id]?.state ?? 'pending'
}
const nodeDotColor = (n: any): string => {
  switch (nodeStateOf(n)) {
    case 'online': return 'var(--emerald)'
    case 'core-stopped': return 'var(--amber)'
    case 'offline': return 'var(--rose)'
    default: return 'var(--text-3)'
  }
}
const nodeRightLabel = (n: any): string => {
  switch (nodeStateOf(n)) {
    case 'online': return (data.nodesStatus[n.id]?.latency ?? 0) + ' ' + t('date.ms')
    case 'core-stopped': return t('node.status.coreStopped')
    case 'offline': return t('node.status.offline')
    case 'disabled': return t('disable')
    default: return t('node.status.pending')
  }
}
const nodeRightColor = (n: any): string =>
  nodeStateOf(n) === 'online' ? 'var(--text-2)' : nodeDotColor(n)

/* ---------- modals ---------- */
const logsOpen = ref(false)
const backupOpen = ref(false)
const usageOpen = ref(false)

/* ---------- live status via websocket push (server samples every 2s) ---------- */
const status = ref<any>({})
const sys = ref<any>({})
const sbd = computed(() => status.value.sbd ?? {})
const sbVersion = computed(() => sbd.value.version || '—')

// 服务端 status 主题的采样周期。只在首拍(还没有时间戳基准)时用来换算速率,
// 之后一律按推送自带的 t 算真实间隔,所以别当成"轮询周期"
const FALLBACK_SAMPLE_SEC = 2
const BUF = 40
const buf = reactive({
  netIn: [] as number[],
  netOut: [] as number[],
  users: [] as number[],
  inbounds: [] as number[],
  // 每个速率点对应的采样时刻(毫秒)。推送节奏可变(重连、断流),写死 2 秒会让
  // x 轴与 tooltip 标注跟真实时间对不上,所以按实际时间戳标注。
  netAt: [] as number[],
})
let lastNet: { recv: number; sent: number } | null = null
let lastT: number | null = null

const push = (arr: number[], v: number) => { arr.push(v); if (arr.length > BUF) arr.shift() }

const onSample = (obj: any) => {
  if (!obj) return
  status.value = { ...status.value, ...obj }
  // 推送带服务器时间戳 t(毫秒);按真实间隔换算速率,重连间隙不会造出尖峰
  const dtSec = Number.isFinite(obj.t) && lastT ? Math.max(0.5, (obj.t - lastT) / 1000) : FALLBACK_SAMPLE_SEC
  if (Number.isFinite(obj.t)) lastT = obj.t
  const net = obj.net
  // net.recv/sent 可能缺失(后端取不到 IO 计数器时返回空对象)
  if (net && Number.isFinite(net.recv) && Number.isFinite(net.sent)) {
    // 有基准才出速率;恢复后的首个有效拍只重建基准,避免把跨缺口的累计增量当作单拍速率
    if (lastNet) {
      push(buf.netIn, Math.max(0, (net.recv - lastNet.recv) / dtSec))
      push(buf.netOut, Math.max(0, (net.sent - lastNet.sent) / dtSec))
      push(buf.netAt, Number.isFinite(obj.t) ? obj.t : Date.now())
    }
    lastNet = { recv: net.recv, sent: net.sent }
  } else {
    // 计数器中断:作废基准,使恢复后首拍走上面"只重建基准"分支,既挡住 NaN 也避免速率尖峰
    lastNet = null
  }
  push(buf.users, data.onlines.user?.length ?? 0)
  push(buf.inbounds, data.onlines.inbound?.length ?? 0)
}

const statusResources = () => {
  const r = ['net', 'sbd']
  if (tiles.resources) r.push('cpu', 'mem', 'dsk', 'swp')
  return r
}

const loadSys = async () => {
  const msg = await HttpUtils.get('api/status', { r: 'sys' })
  if (msg.success && msg.obj?.sys) sys.value = msg.obj.sys
}

let live: LiveSub | null = null
onMounted(() => {
  loadSys()
  loadChanges()
  live = liveSubscribe({ topic: 'status', params: () => ({ r: statusResources() }), onData: onSample })
})
// 资源卡开关改变要采的指标集合;重订阅即以新参数生效
watch(() => tiles.resources, () => live?.resubscribe())
onBeforeUnmount(() => { live?.stop(); live = null })

/* ---------- derived live values ---------- */
const netInNow = computed(() => buf.netIn[buf.netIn.length - 1] ?? 0)
const netOutNow = computed(() => buf.netOut[buf.netOut.length - 1] ?? 0)
const onlineUsers = computed(() => data.onlines.user?.length ?? 0)
const onlineInbounds = computed(() => data.onlines.inbound?.length ?? 0)
const totalDown = computed(() => data.clients.reduce((a: number, c: any) => a + (c.down ?? 0), 0))
const totalUp = computed(() => data.clients.reduce((a: number, c: any) => a + (c.up ?? 0), 0))
const trafficAxis = computed(() => {
  // 按首尾采样时刻的真实跨度标注,而不是"点数 × 2 秒"
  const at = buf.netAt
  if (at.length < 2) return []
  const span = Math.round((at[at.length - 1] - at[0]) / 1000)
  if (span <= 0) return []
  return [`-${span}s`, `-${Math.round(span * 0.75)}s`, `-${Math.round(span * 0.5)}s`, `-${Math.round(span * 0.25)}s`, 'now']
})

// 实时图 tooltip：速率带单位，标签用相对时间（最后一点为 now）
const netFmt = (v: number) => HumanReadable.sizeFormat(v) + '/s'
const trafficLabels = computed(() =>
  buf.netIn.map((_, i, a) => {
    if (i === a.length - 1) return 'now'
    const at = buf.netAt
    // 同样按真实时间差,断流后重连的那一点不会被标成 2 秒前
    if (at.length === a.length && at[at.length - 1] != null && at[i] != null) {
      return `-${Math.round((at[at.length - 1] - at[i]) / 1000)}s`
    }
    return `-${(a.length - 1 - i) * FALLBACK_SAMPLE_SEC}s`
  }),
)

const pctOf = (d: any) => (d && d.total ? Math.min(100, Math.ceil((d.current * 100) / d.total)) : 0)
const subOf = (d: any) => (d && d.total ? `${HumanReadable.sizeFormat(d.current, 0)} / ${HumanReadable.sizeFormat(d.total, 0)}` : '')
const gCpu = computed(() => ({ pct: Math.ceil(status.value.cpu ?? 0) }))
const gMem = computed(() => ({ pct: pctOf(status.value.mem), sub: subOf(status.value.mem) }))
const gDsk = computed(() => ({ pct: pctOf(status.value.dsk), sub: subOf(status.value.dsk) }))
const gSwp = computed(() => ({ pct: pctOf(status.value.swp), sub: subOf(status.value.swp) }))

const uptime = computed(() =>
  sys.value.bootTime ? HumanReadable.formatSecond(Date.now() / 1000 - sys.value.bootTime) : '—'
)

/* ---------- protocol mix (clients per inbound protocol) ---------- */
const PALETTE = ['var(--brand)', 'var(--cyan)', 'var(--violet)', 'var(--emerald)', 'var(--amber)', 'var(--text-3)']
const protoMix = computed(() => {
  const typeById = new Map<number, string>(data.inbounds.map((i: any) => [i.id, i.type]))
  const byType: Record<string, number> = {}
  for (const c of data.clients) {
    if (!c.enable) continue
    const types = new Set((c.inbounds ?? []).map((id: number) => typeById.get(id)).filter(Boolean))
    for (const tp of types) byType[tp as string] = (byType[tp as string] ?? 0) + 1
  }
  const entries = Object.entries(byType).sort((a, b) => b[1] - a[1])
  const top = entries.slice(0, 5)
  const rest = entries.slice(5).reduce((a, e) => a + e[1], 0)
  if (rest > 0) top.push([t('ui.none'), rest] as any)
  const total = top.reduce((a, e) => a + e[1], 0) || 1
  return top.map(([name, value], i) => ({
    name,
    value,
    pct: Math.round((value / total) * 100),
    color: PALETTE[i % PALETTE.length],
  }))
})

/* ---------- activity (changes log) ---------- */
const changes = ref<any[]>([])
const loadChanges = async () => {
  const msg = await HttpUtils.get('api/changes', { a: '', k: '', c: 6 })
  if (msg.success && Array.isArray(msg.obj)) {
    const l = String(locale.value) == 'fa' ? 'fa-IR' : 'en-US'
    changes.value = msg.obj.map((c: any) => ({
      id: c.id,
      actor: c.Actor,
      action: c.action,
      key: c.key,
      time: new Date(Number(c.dateTime) * 1000).toLocaleTimeString(l, { hour: '2-digit', minute: '2-digit' }),
    }))
  }
}

const ACT_COLORS: Record<string, string> = {
  add: 'var(--emerald)', new: 'var(--emerald)',
  del: 'var(--rose)', delete: 'var(--rose)',
  edit: 'var(--brand)', set: 'var(--brand)', update: 'var(--brand)',
  restart: 'var(--amber)', reset: 'var(--amber)',
}
const actColor = (a: string) => ACT_COLORS[a] ?? 'var(--brand)'
const actionLabel = (a: string) => (te('actions.' + a) ? t('actions.' + a) : a)
const objectLabel = (k: string) => (te('objects.' + k) ? t('objects.' + k) : k)

/* ---------- restart sing-box ---------- */
const restarting = ref(false)
const restartSb = async () => {
  restarting.value = true
  await HttpUtils.post('api/restartSb', {})
  restarting.value = false
}
</script>

<style scoped>
.top-grid { display: grid; grid-template-columns: repeat(var(--top-cols, 3), minmax(0, 1fr)); gap: 14px; }
.res-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px 18px; height: 100%; align-content: center; }
.km-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px 14px; height: 100%; align-content: center; }
.srv-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 13px 14px; padding: 16px 20px; }
.srv-k { font-size: 10.5px; color: var(--text-3); margin-bottom: 3px; }
.srv-v { font-size: 12.5px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.srv-status { display: flex; align-items: center; gap: 10px; padding: 13px 20px; border-top: 1px solid var(--line); }
.main-grid { display: grid; grid-template-columns: repeat(var(--main-cols, 3), minmax(0, 1fr)); gap: 18px; }
/* minmax(0, 1fr)：裸 1fr 的 min 是 auto，ECharts SVG 的固定宽度会把列撑开导致图表缩不回（#15） */
.net-grid { display: grid; grid-template-columns: minmax(0, 1fr); gap: 14px; }
@media (max-width: 1180px) {
  .top-grid { grid-template-columns: 1fr 1fr !important; }
  .main-grid { grid-template-columns: repeat(2, minmax(0, 1fr)) !important; }
}
@media (max-width: 820px) {
  .top-grid { grid-template-columns: 1fr !important; }
  .main-grid { grid-template-columns: minmax(0, 1fr) !important; }
}
</style>
