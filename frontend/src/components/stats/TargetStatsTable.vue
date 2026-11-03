<template>
  <div class="target-stats-container">
    <!-- Toolbar: period selector, search input, refresh button -->
    <Teleport v-if="mounted && toolbarTeleport" :to="toolbarTeleport">
      <div class="ts-toolbar ts-header-toolbar">
        <span class="ts-node-hint">{{ $t('ui.localNodeOnly') }}</span>
        <Segmented v-model="period" :options="periods" @update:model-value="onPeriodChange" />
        <div class="search-box">
          <input
            v-model="searchInput"
            type="text"
            class="ts-input"
            :placeholder="$t('ui.siteSearch')"
            @input="onSearchInput"
            @keydown.enter="onSearchEnter"
          />
          <span v-if="searchInput" class="clear-btn" @click="clearSearch">✕</span>
        </div>
        <Btn sm variant="subtle" :loading="loading" @click="fetchData">
          <Ico name="refresh" :size="14" /> {{ $t('actions.update') }}
        </Btn>
      </div>
    </Teleport>
    <div v-else-if="!toolbarTeleport" class="ts-toolbar">
      <span class="ts-node-hint">{{ $t('ui.localNodeOnly') }}</span>
      <Segmented v-model="period" :options="periods" @update:model-value="onPeriodChange" />
      <div class="grow" />
      <div class="search-box">
        <input
          v-model="searchInput"
          type="text"
          class="ts-input"
          :placeholder="$t('ui.siteSearch')"
          @input="onSearchInput"
          @keydown.enter="onSearchEnter"
        />
        <span v-if="searchInput" class="clear-btn" @click="clearSearch">✕</span>
      </div>
      <Btn sm variant="subtle" :loading="loading" @click="fetchData">
        <Ico name="refresh" :size="14" /> {{ $t('actions.update') }}
      </Btn>
    </div>

    <!-- Summary KPI metrics cards -->
    <div class="ts-summary">
      <div class="ts-metric">
        <div class="ts-metric-k">{{ $t('ui.totalSitesCount') }}</div>
        <div class="ts-metric-v mono">{{ summary.totalSites }}</div>
      </div>
      <div class="ts-metric">
        <div class="ts-metric-k">{{ $t('stats.download') }}</div>
        <div class="ts-metric-v mono" style="color: var(--cyan);">↓ {{ HumanReadable.sizeFormat(summary.totalDown) }}</div>
      </div>
      <div class="ts-metric">
        <div class="ts-metric-k">{{ $t('stats.upload') }}</div>
        <div class="ts-metric-v mono" style="color: var(--violet);">↑ {{ HumanReadable.sizeFormat(summary.totalUp) }}</div>
      </div>
      <div class="ts-metric">
        <div class="ts-metric-k">{{ $t('ui.totalTraffic') }}</div>
        <div class="ts-metric-v mono" style="color: var(--brand);">{{ HumanReadable.sizeFormat(summary.total) }}</div>
      </div>
    </div>

    <!-- Loading state -->
    <div v-if="loading && items.length === 0" style="padding: 30px 0;">
      <EmptyState icon="chart" :title="$t('loading')" />
    </div>

    <!-- Empty state -->
    <div v-else-if="items.length === 0" style="padding: 30px 0;">
      <EmptyState icon="globe" :title="$t('noData')" />
    </div>

    <!-- Data table -->
    <div v-else class="ts-table-wrapper">
      <table class="dtable">
        <thead>
          <tr>
            <th style="width: 50px; text-align: center;">#</th>
            <th>{{ $t('ui.targetSite') }}</th>
            <th class="sortable" style="text-align: end; width: 140px;" @click="toggleSort('up')">
              {{ $t('stats.upload') }}
              <span class="sort-ico" :class="{ active: sortField === 'up' }">{{ sortOrderIco('up') }}</span>
            </th>
            <th class="sortable" style="text-align: end; width: 140px;" @click="toggleSort('down')">
              {{ $t('stats.download') }}
              <span class="sort-ico" :class="{ active: sortField === 'down' }">{{ sortOrderIco('down') }}</span>
            </th>
            <th class="sortable" style="text-align: end; width: 170px;" @click="toggleSort('total')">
              {{ $t('ui.totalTraffic') }}
              <span class="sort-ico" :class="{ active: sortField === 'total' }">{{ sortOrderIco('total') }}</span>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(item, idx) in items" :key="item.target">
            <td class="cell mono" style="text-align: center; color: var(--text-3); font-size: 11px;">
              {{ (page - 1) * limit + idx + 1 }}
            </td>
            <td class="cell">
              <div class="site-cell">
                <span class="site-icon">🌐</span>
                <span class="site-name mono" :title="item.target">{{ item.target }}</span>
                <button
                  type="button"
                  class="copy-btn"
                  :title="$t('actions.copy')"
                  @click="copySite(item.target)"
                >
                  <Ico name="copy" :size="12" />
                </button>
              </div>
            </td>
            <td class="cell mono" style="text-align: end; color: var(--violet); font-weight: 600;">
              ↑ {{ HumanReadable.sizeFormat(item.up) }}
            </td>
            <td class="cell mono" style="text-align: end; color: var(--cyan); font-weight: 600;">
              ↓ {{ HumanReadable.sizeFormat(item.down) }}
            </td>
            <td class="cell" style="text-align: end;">
              <div class="mono" style="font-weight: 700;">{{ HumanReadable.sizeFormat(item.total) }}</div>
              <div class="progress-bar">
                <div class="progress-fill" :style="{ width: percentOfMax(item.total) + '%' }" />
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- Pagination controls -->
    <div v-if="totalPages > 1" class="ts-pagination">
      <div class="page-info mono">
        {{ $t('ui.pageInfo', { current: page, total: totalPages, count: totalCount }) }}
      </div>
      <div class="page-btns">
        <Btn sm variant="subtle" :disabled="page <= 1" @click="changePage(page - 1)">
          {{ $t('actions.prev') }}
        </Btn>
        <span class="mono" style="font-size: 12px; padding: 0 4px;">{{ page }} / {{ totalPages }}</span>
        <Btn sm variant="subtle" :disabled="page >= totalPages" @click="changePage(page + 1)">
          {{ $t('actions.next') }}
        </Btn>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import HttpUtils from '@/plugins/httputil'
import { HumanReadable } from '@/plugins/utils'
import { push } from 'notivue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Segmented from '@/components/ui/Segmented.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = withDefaults(defineProps<{
  user?: string
  defaultPeriod?: string
  limit?: number
  toolbarTeleport?: string
}>(), {
  user: '',
  defaultPeriod: 'day',
  limit: 15,
})

const { t } = useI18n({ useScope: 'global' })

const mounted = ref(false)

const period = ref<string | number>(props.defaultPeriod)
const periods = computed<[string, string][]>(() => [
  ['hour', '1' + t('date.h')],
  ['day', '1' + t('date.d')],
  ['month', '30' + t('date.d')],
  ['60day', '60' + t('date.d')],
  ['90day', '90' + t('date.d')],
])

interface TargetItem {
  target: string
  up: number
  down: number
  total: number
}

const items = ref<TargetItem[]>([])
const summary = ref({
  totalUp: 0,
  totalDown: 0,
  total: 0,
  totalSites: 0,
})

const loading = ref(false)
const searchInput = ref('')
const sortField = ref<'total' | 'down' | 'up'>('total')
const sortOrder = ref<'desc' | 'asc'>('desc')
const page = ref(1)
const totalCount = ref(0)

const totalPages = computed(() => {
  return Math.ceil(totalCount.value / props.limit) || 1
})

const maxTotal = computed(() => {
  if (items.value.length === 0) return 1
  return Math.max(...items.value.map((i) => i.total)) || 1
})

const percentOfMax = (val: number) => {
  return Math.min(100, Math.max(4, Math.round((val / maxTotal.value) * 100)))
}

const sortOrderIco = (field: string) => {
  if (sortField.value !== field) return '⇅'
  return sortOrder.value === 'desc' ? '↓' : '↑'
}

const toggleSort = (field: 'total' | 'down' | 'up') => {
  if (sortField.value === field) {
    sortOrder.value = sortOrder.value === 'desc' ? 'asc' : 'desc'
  } else {
    sortField.value = field
    sortOrder.value = 'desc'
  }
  page.value = 1
  fetchData()
}

let currentReqId = 0
let debounceTimer: any = null
const onSearchInput = () => {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    page.value = 1
    fetchData()
  }, 350)
}

const onSearchEnter = () => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  page.value = 1
  fetchData()
}

const clearSearch = () => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  searchInput.value = ''
  page.value = 1
  fetchData()
}

const onPeriodChange = () => {
  page.value = 1
  fetchData()
}

const changePage = (p: number) => {
  if (p < 1 || p > totalPages.value) return
  page.value = p
  fetchData()
}

const copySite = (site: string) => {
  navigator.clipboard.writeText(site).then(() => {
    push.success({ message: t('actions.copied') })
  }).catch(() => {})
}

const fetchData = async () => {
  const reqId = ++currentReqId
  loading.value = true
  try {
    const params: Record<string, any> = {
      period: period.value,
      sort: sortField.value,
      order: sortOrder.value,
      page: page.value,
      limit: props.limit,
    }
    if (props.user) {
      params.user = props.user
    }
    if (searchInput.value.trim()) {
      params.search = searchInput.value.trim()
    }

    const res = await HttpUtils.get('api/targetStats', params)
    if (reqId !== currentReqId) {
      return
    }
    if (res.success && res.obj) {
      items.value = res.obj.items || []
      summary.value = res.obj.summary || { totalUp: 0, totalDown: 0, total: 0, totalSites: 0 }
      totalCount.value = res.obj.total || 0
    }
  } finally {
    if (reqId === currentReqId) {
      loading.value = false
    }
  }
}

watch(() => props.user, () => {
  page.value = 1
  fetchData()
})

onMounted(() => {
  mounted.value = true
  fetchData()
})
</script>

<style scoped>
.target-stats-container {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ts-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.ts-node-hint {
  font-size: 11px;
  color: var(--text-3);
  padding: 3px 8px;
  border-radius: 4px;
  background: var(--bg-hover);
  border: 1px solid var(--border-subtle);
  white-space: nowrap;
}

.ts-header-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.grow {
  flex: 1;
}

.search-box {
  position: relative;
  display: flex;
  align-items: center;
}

.ts-input {
  background: var(--surface-3);
  border: 1px solid var(--line);
  color: var(--text);
  border-radius: var(--radius-sm);
  padding: 0 26px 0 10px;
  height: 32px;
  font-size: 12.5px;
  width: 170px;
  transition: all 0.15s ease;
}

.ts-input:focus {
  outline: none;
  border-color: var(--brand);
  background: var(--surface);
  box-shadow: 0 0 0 3px var(--brand-soft);
  width: 210px;
}

.clear-btn {
  position: absolute;
  right: 8px;
  cursor: pointer;
  color: var(--text-3);
  font-size: 11px;
}

.clear-btn:hover {
  color: var(--text);
}

.ts-summary {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
  gap: 10px;
}

.ts-metric {
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--radius-sm);
  padding: 8px 12px;
}

.ts-metric-k {
  font-size: 11px;
  color: var(--text-3);
  margin-bottom: 2px;
}

.ts-metric-v {
  font-size: 14px;
  font-weight: 700;
}

.ts-table-wrapper {
  border: 1px solid var(--line);
  border-radius: 10px;
  overflow-x: auto;
}

.dtable {
  width: 100%;
  border-collapse: collapse;
}

.dtable th {
  padding: 9px 12px;
  font-size: 11.5px;
  color: var(--text-3);
  font-weight: 600;
  border-bottom: 1px solid var(--line);
  white-space: nowrap;
}

.dtable th.sortable {
  cursor: pointer;
  user-select: none;
}

.dtable th.sortable:hover {
  color: var(--text-1);
}

.sort-ico {
  display: inline-block;
  margin-inline-start: 4px;
  font-size: 10px;
  opacity: 0.4;
}

.sort-ico.active {
  opacity: 1;
  color: var(--brand);
}

.dtable .cell {
  padding: 9px 12px;
  font-size: 12.5px;
  border-bottom: 1px solid var(--line);
}

.site-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  max-width: 320px;
}

.site-icon {
  font-size: 13px;
  flex: none;
}

.site-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.copy-btn {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-3);
  padding: 2px 4px;
  border-radius: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
  display: flex;
  align-items: center;
}

.site-cell:hover .copy-btn {
  opacity: 1;
}

.copy-btn:hover {
  color: var(--brand);
  background: color-mix(in srgb, var(--brand) 12%, transparent);
}

.progress-bar {
  width: 100%;
  height: 3px;
  background: color-mix(in srgb, var(--line) 60%, transparent);
  border-radius: 2px;
  margin-top: 4px;
  overflow: hidden;
}

.progress-fill {
  height: 100%;
  background: var(--brand);
  border-radius: 2px;
  transition: width 0.3s ease;
}

.ts-pagination {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 2px;
  flex-wrap: wrap;
  gap: 10px;
}

.page-info {
  font-size: 12px;
  color: var(--text-3);
}

.page-btns {
  display: flex;
  align-items: center;
  gap: 6px;
}
</style>
