<template>
  <div ref="el" :style="{ width: '100%', minWidth: 0, height: height + 'px' }" />
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue'
import echarts from '@/plugins/echarts'
import { useChart, resolveColor, withAlpha, cssVar } from './useChart'

const props = withDefaults(defineProps<{
  data: number[]
  data2?: number[]
  color?: string
  color2?: string
  height?: number
  width?: number
  pad?: number
  /** Tooltip value formatter (e.g. HumanReadable.sizeFormat for traffic); defaults to comma-separated thousands */
  valueFormatter?: (v: number) => string
  /** Labels for each x-axis point (date/time), used for tooltip header row */
  labels?: string[]
  /** Whether animation is enabled. Disabled by default for real-time charts to render latest data instantly without jitter */
  animated?: boolean
}>(), {
  color: 'var(--brand)',
  color2: 'var(--emerald)',
  height: 160,
  width: 600,
  pad: 8,
  animated: false,
})

const el = ref<HTMLElement>()

const build = (chart: ReturnType<typeof echarts.init>, host: HTMLElement) => {
  const c1 = resolveColor(host, props.color)
  const c2 = resolveColor(host, props.color2)
  const lineCol = cssVar(host, '--line')
  const muted = cssVar(host, '--text-3')
  const surface = cssVar(host, '--surface')
  const elevated = cssVar(host, '--elevated') || surface
  const text = cssVar(host, '--text')
  const fmt = props.valueFormatter ?? ((v: number) => Math.round(v).toLocaleString())

  const all = props.data2 ? [...props.data, ...props.data2] : props.data
  const max = (Math.max(...all, 0) || 1) * 1.12
  const last = props.data.length - 1

  const series: any[] = [
    {
      type: 'line',
      smooth: 0.4,
      showSymbol: false,
      clip: false, // Prevent terminal dot from being clipped at the edge
      lineStyle: { color: c1, width: 2.4, cap: 'round' },
      areaStyle: {
        color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
          { offset: 0, color: withAlpha(c1, 0.32) },
          { offset: 1, color: withAlpha(c1, 0) },
        ]),
      },
      data: props.data,
      z: 3,
      markPoint: {
        symbol: 'circle',
        symbolSize: 7,
        silent: true,
        label: { show: false },
        itemStyle: { color: c1, borderColor: surface, borderWidth: 2 },
        data: last >= 0 ? [{ coord: [last, props.data[last]] }] : [],
      },
    },
  ]

  if (props.data2) {
    series.push({
      type: 'line',
      smooth: 0.4,
      showSymbol: false,
      clip: false,
      lineStyle: { color: c2, width: 2, type: 'dashed', opacity: 0.85 },
      data: props.data2,
      z: 2,
    })
  }

  const option: any = {
    animation: props.animated,
    animationDuration: props.animated ? 500 : 0,
    animationDurationUpdate: props.animated ? 300 : 0,
    grid: { left: props.pad, right: props.pad + 4, top: 14, bottom: 10, containLabel: false },
    tooltip: {
      trigger: 'axis',
      backgroundColor: elevated,
      borderColor: lineCol,
      borderWidth: 1,
      padding: [6, 10],
      textStyle: { color: text, fontSize: 12 },
      axisPointer: { type: 'line', lineStyle: { color: muted, width: 1, type: 'dashed' } },
      formatter: (ps: any[]) => {
        const idx = ps[0]?.dataIndex ?? 0
        const lbl = props.labels?.[idx]
        const head = lbl ? `<div style="font-size:11px;color:${muted};margin-bottom:5px;">${lbl}</div>` : ''
        return head + ps
          .map(
            (p) =>
              `<div style="display:flex;align-items:center;gap:7px;line-height:1.65;"><span style="width:8px;height:8px;border-radius:2px;background:${p.color};flex:none;"></span><span class="mono">${fmt(Number(p.value))}</span></div>`,
          )
          .join('')
      },
    },
    xAxis: {
      type: 'category',
      boundaryGap: false,
      show: false,
      data: props.labels ?? props.data.map((_, i) => i),
    },
    yAxis: {
      // show: true with axis line/tick/label hidden — otherwise show: false hides splitLine grid lines as well
      type: 'value',
      min: 0,
      max,
      show: true,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { show: false },
      splitNumber: 3,
      splitLine: { show: true, lineStyle: { color: lineCol, width: 1 } },
    },
    series,
  }

  chart.setOption(option) // merge mode: smooth transition on data changes
}

const { render } = useChart(el, build)

// Re-render when real-time data changes
watch(() => [props.data, props.data2], render, { deep: true })
</script>
