<template>
  <Modal :open="visible" :title="$t('stats.graphTitle')" :width="800" @close="$emit('close')">
    <div style="padding: 18px 20px;">
      <div style="display: flex; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 16px;">
        <Chip :color="'brand'"><span class="mono">{{ $t('objects.' + resource) }} · {{ tag }}</span></Chip>
        <div style="flex: 1;" />
        <Segmented v-model="period" :options="periods" @update:model-value="onPeriodChange" />
      </div>

      <div v-if="noData" style="padding: 24px 0;">
        <EmptyState icon="chart" :title="$t('noData')" />
      </div>
      <template v-else>
        <div style="display: flex; gap: 22px; margin-bottom: 12px; flex-wrap: wrap;">
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="width: 16px; height: 3px; border-radius: 2px; background: var(--brand);" />
            <div>
              <div style="font-size: 11.5px; color: var(--text-3);">{{ $t('stats.download') }}</div>
              <div class="mono" style="font-size: 14px; font-weight: 700;">{{ HumanReadable.sizeFormat(totalDown) }}</div>
            </div>
          </div>
          <div style="display: flex; align-items: center; gap: 8px;">
            <span style="width: 16px; height: 3px; border-radius: 2px; border-top: 2px dashed var(--emerald);" />
            <div>
              <div style="font-size: 11.5px; color: var(--text-3);">{{ $t('stats.upload') }}</div>
              <div class="mono" style="font-size: 14px; font-weight: 700;">{{ HumanReadable.sizeFormat(totalUp) }}</div>
            </div>
          </div>
        </div>
        <AreaChart :data="down" :data2="up" :height="220" :labels="tooltipLabels" :value-formatter="HumanReadable.sizeFormat" />
        <div class="mono" style="display: flex; justify-content: space-between; margin-top: 8px; font-size: 10.5px; color: var(--text-3);">
          <span v-for="l in axisLabels" :key="l">{{ l }}</span>
        </div>
      </template>
    </div>
  </Modal>
</template>

<script lang="ts" setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { liveSubscribe, type LiveSub } from '@/plugins/ws'
import { HumanReadable } from '@/plugins/utils'
import Modal from '@/components/ui/Modal.vue'
import Chip from '@/components/ui/Chip.vue'
import Segmented from '@/components/ui/Segmented.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import AreaChart from '@/components/charts/AreaChart.vue'

const props = defineProps<{
  visible: boolean
  resource: string
  tag: string
}>()
defineEmits<{ close: [] }>()

const { t, locale } = useI18n({ useScope: 'global' })

const period = ref<string | number>('hour')
const periods = computed<[string, string][]>(() => [
  ['hour', '1' + t('date.h')],
  ['day', '1' + t('date.d')],
  ['month', '30' + t('date.d')],
  ['60day', '60' + t('date.d')],
  ['90day', '90' + t('date.d')],
])

const up = ref<number[]>([])
const down = ref<number[]>([])
const times = ref<number[]>([])
const noData = ref(false)

const totalUp = computed(() => up.value.reduce((a, b) => a + b, 0))
const totalDown = computed(() => down.value.reduce((a, b) => a + b, 0))

const genLabel = (ms: number) => {
  const l = String(locale.value) == 'fa' ? 'fa-IR' : 'en-US'
  const d = new Date(ms)
  // 小时/单日维度显示「时:分」；30/60/90 天等多日维度显示「月-日」
  if (period.value === 'hour' || period.value === 'day') return d.toLocaleTimeString(l, { hour: '2-digit', minute: '2-digit', hour12: false })
  return d.toLocaleDateString(l, { month: '2-digit', day: '2-digit' })
}
const axisLabels = computed(() => {
  if (times.value.length < 2) return []
  const pick = [0, .25, .5, .75, 1].map((f) => times.value[Math.round(f * (times.value.length - 1))])
  return [...new Set(pick)].map((tms) => genLabel(tms * 1000))
})

// tooltip 标题：每个数据点的完整日期/时间（小时/单日到分钟；多日到「月-日 时:分」）
const tooltipLabels = computed(() => {
  const l = String(locale.value) == 'fa' ? 'fa-IR' : 'en-US'
  return times.value.map((tms) => {
    const d = new Date(tms * 1000)
    if (period.value === 'hour' || period.value === 'day') return d.toLocaleString(l, { hour: '2-digit', minute: '2-digit', hour12: false })
    return d.toLocaleString(l, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
  })
})

const applyRows = (rows: any) => {
  if (Array.isArray(rows) && rows.length > 0) {
    // one up row + one down row per time bucket, sorted by time
    const upBy: Record<number, number> = {}
    const downBy: Record<number, number> = {}
    const set = new Set<number>()
    for (const o of rows) {
      const tms = Number(o.dateTime)
      if (!tms) continue
      set.add(tms)
      if (o.direction) upBy[tms] = Number(o.traffic) || 0
      else downBy[tms] = Number(o.traffic) || 0
    }
    const sorted = Array.from(set).sort((a, b) => a - b)
    if (sorted.length === 0) { noData.value = true; return }
    times.value = sorted
    up.value = sorted.map((tms) => upBy[tms] ?? 0)
    down.value = sorted.map((tms) => downBy[tms] ?? 0)
    noData.value = false
  } else {
    noData.value = true
  }
}

// The websocket 'stats' subscription answers a subscribe with the rows right
// away and refreshes them after every stats flush (the only moment the data
// can change). The echoed key drops pushes that raced a period/tag switch.
let live: LiveSub | null = null
watch(() => props.visible, (v) => {
  if (v) {
    period.value = 'hour'
    noData.value = false
    up.value = []
    down.value = []
    live = liveSubscribe({
      topic: 'stats',
      params: () => ({ resource: props.resource, tag: props.tag, period: String(period.value) }),
      onData: (d) => {
        if (d && d.resource === props.resource && d.tag === props.tag && d.period === String(period.value)) {
          applyRows(d.stats)
        }
      },
    })
    // Opened while the socket is down: the subscribe never left the browser,
    // so show the empty state rather than an blank chart that looks like data.
    if (!live.connected()) noData.value = true
  } else {
    live?.stop()
    live = null
  }
})
// Re-subscribing with the new period makes the server answer immediately. If it
// could not be sent, the axis and tooltips have already switched format, so the
// old buckets would render under the new period's labels — show nothing instead.
const onPeriodChange = () => {
  if (live && !live.resubscribe()) {
    up.value = []
    down.value = []
    times.value = []
    noData.value = true
  }
}
onBeforeUnmount(() => { live?.stop(); live = null })
</script>
