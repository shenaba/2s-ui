<template>
  <InboundDrawer
    :visible="drawer.visible"
    :id="drawer.id"
    :in-tags="inTags"
    :tls-configs="tlsConfigs"
    @close="drawer.visible = false"
  />
  <StatsModal
    :visible="stats.visible"
    :resource="stats.resource"
    :tag="stats.tag"
    @close="stats.visible = false"
  />

  <!-- delete confirmation -->
  <DeleteConfirm :open="del.visible" :loading="deleting" @close="del.visible = false" @confirm="confirmDelete" />

  <div class="page-stack fade-up">
    <!-- ===================== toolbar ===================== -->
    <div class="toolbar">
      <Segmented v-model="filter" :options="filterOptions" />
      <div class="grow" />
      <Btn variant="primary" sm @click="openDrawer(0)">
        <Ico name="plus" :size="16" /> {{ $t('ui.addInbound') }}
      </Btn>
    </div>

    <!-- ===================== cards ===================== -->
    <div class="entity-grid">
      <div v-for="item in rows" :key="item.id" class="card inb-card" :class="{ 'inb-off': !isEnabled(item) }">
        <div class="inb-head">
          <div class="inb-ico" :style="iconStyle(item.type)"><Ico name="inbound" :size="19" /></div>
          <div class="inb-title">
            <div class="inb-tag">{{ item.tag }}</div>
            <div class="inb-type" :style="{ color: protoColor(item.type) }">{{ item.type }}</div>
          </div>
          <Chip v-if="nodeNameOf(item)" color="brand">{{ nodeNameOf(item) }}</Chip>
          <Chip v-if="!isEnabled(item)">{{ $t('ui.off') }}</Chip>
          <template v-else-if="!nodeNameOf(item)">
            <Chip v-if="isOnline(item.tag)" color="emerald" dot>{{ $t('ui.live') }}</Chip>
            <Chip v-else>{{ $t('ui.idle') }}</Chip>
          </template>
          <!-- A replica is switched on its node, where it runs. -->
          <Toggle
            v-if="!nodeNameOf(item)"
            :model-value="isEnabled(item)"
            :scale="0.8"
            :title="$t(isEnabled(item) ? 'disable' : 'enable')"
            :aria-label="$t(isEnabled(item) ? 'disable' : 'enable')"
            :style="{ flex: 'none', opacity: toggling[item.id] ? 0.5 : 1, pointerEvents: toggling[item.id] ? 'none' : 'auto' }"
            @update:model-value="toggleEnable(item)"
          />
        </div>
        <div class="inb-meta">
          <div>
            <div class="m-k">{{ $t('ui.listen') }}</div>
            <div class="m-v mono" dir="ltr">{{ listenOf(item) }}</div>
          </div>
          <div>
            <div class="m-k">{{ $t('ui.tlsLbl') }}</div>
            <Chip v-if="item.tls_id > 0" color="emerald">{{ $t('ui.enabled') }}</Chip>
            <div v-else class="m-v">{{ $t('ui.off') }}</div>
          </div>
          <div>
            <div class="m-k">{{ $t('ui.clientsCol') }}</div>
            <div class="m-v" :title="usersTitle(item)">{{ item.users ? item.users.length : '—' }}</div>
          </div>
          <div>
            <div class="m-k">{{ $t('ui.trafficCol') }}</div>
            <div class="m-v mono">{{ trafficOf(item) }}</div>
          </div>
        </div>
        <div class="inb-actions">
          <CardBtn
            v-if="nodeNameOf(item)"
            icon="eye"
            :label="$t('ui.view')"
            :title="$t('node.manageOnNode')"
            disabled
          />
          <CardBtn v-else icon="edit" :label="$t('ui.edit')" :title="$t('actions.edit')" @click="openDrawer(item.id)" />
          <CardBtn v-if="!nodeNameOf(item)" icon="clone" :label="$t('ui.clone')" border :title="$t('actions.clone')" @click="clone(item.id)" />
          <CardBtn
            v-if="dataStore.enableTraffic"
            icon="chart"
            :label="$t('ui.stats')"
            border
            :title="$t('stats.graphTitle')"
            @click="showStats(item.tag)"
          />
          <CardBtn icon="trash" border danger :title="nodeNameOf(item) ? $t('node.deAdopt') : $t('actions.del')" @click="askDelete(item.tag)" />
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import { createInbound, followListenPort } from '@/types/inbounds'
import RandomUtil from '@/plugins/randomUtil'
import { protoColor } from '@/plugins/colors'
import { HumanReadable } from '@/plugins/utils'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import DeleteConfirm from '@/components/ui/DeleteConfirm.vue'
import Segmented from '@/components/ui/Segmented.vue'
import CardBtn from '@/components/ui/CardBtn.vue'
import Toggle from '@/components/ui/Toggle.vue'
import InboundDrawer from '@/layouts/drawers/inbound/InboundDrawer.vue'
import StatsModal from '@/layouts/drawers/StatsModal.vue'

const { t } = useI18n({ useScope: 'global' })
const dataStore = Data()

// ---------------- store data ----------------
const inbounds = computed((): any[] => dataStore.inbounds ?? [])
const tlsConfigs = computed((): any[] => dataStore.tlsConfigs ?? [])
const inTags = computed((): string[] => dataStore.inboundTags)
const onlines = computed((): string[] => dataStore.onlines.inbound ?? [])
const isOnline = (tag: string): boolean => onlines.value.includes(tag)

// ---------------- node attribution ----------------
const nodeById = computed((): Record<number, string> =>
  Object.fromEntries((dataStore.nodes ?? []).map((n: any) => [n.id, n.name])),
)
const nodeNameOf = (item: any): string => (item.node_id ? nodeById.value[item.node_id] ?? '' : '')

// ---------------- filter ----------------
const filter = ref<string | number>('all')
const filterOptions = computed((): [string | number, string][] => {
  const base: [string | number, string][] = [
    ['all', t('ui.any')],
    ['local', t('node.local')],
    ['tls', t('ui.tlsOnly')],
    ['online', t('ui.online')],
  ]
  // one entry per node that actually owns a replica inbound
  const nodeIds = [...new Set(inbounds.value.filter((i: any) => i.node_id).map((i: any) => i.node_id))]
  for (const id of nodeIds) base.push([`node:${id}`, nodeById.value[id] ?? `#${id}`])
  return base
})
const rows = computed((): any[] =>
  inbounds.value.filter((i: any) => {
    if (filter.value === 'local') return !i.node_id
    if (filter.value === 'tls') return i.tls_id > 0
    if (filter.value === 'online') return isOnline(i.tag)
    if (typeof filter.value === 'string' && filter.value.startsWith('node:')) {
      return String(i.node_id) === filter.value.slice(5)
    }
    return true
  }),
)

// ---------------- cells ----------------
const iconStyle = (type: string) => {
  const col = protoColor(type)
  return { color: col, background: `color-mix(in srgb, ${col} 14%, transparent)` }
}
const listenOf = (item: any): string =>
  item.listen_port ? `${item.listen ?? ''}:${item.listen_port}` : '—'
const usersTitle = (item: any): string | undefined =>
  item.users && item.users.length > 0 ? item.users.join('\n') : undefined
// The core counts each inbound separately, and that is the figure to show.
// Summing the inbound's clients instead gave every inbound that shares a client
// the same number, because a client's up/down is its total across all of the
// inbounds it belongs to. A node replica is measured on its node, which reports
// per client only, so there is nothing to show for one here.
const trafficOf = (item: any): string =>
  item.node_id ? '—' : HumanReadable.sizeFormat(dataStore.inboundTraffic[item.tag] ?? 0)

// ---------------- enable switch ----------------
// Absent is on: rows from a panel that predates the switch carry no flag.
const isEnabled = (item: any): boolean => item.enable !== false

// The list rows are a projection, so the full inbound is fetched and saved
// back with only the switch changed -- as the clients list does. Off, the
// panel takes it out of the core and its links out of the subscriptions; a
// refusal (a client config still riding it) is shown by the save itself.
const toggling = ref<Record<number, boolean>>({})
const toggleEnable = async (item: any) => {
  if (toggling.value[item.id] || nodeNameOf(item)) return
  toggling.value = { ...toggling.value, [item.id]: true }
  try {
    const full = (await Data().loadInbounds([item.id]))[0]
    if (full?.id) {
      await Data().save('inbounds', 'edit', { ...full, enable: !isEnabled(item) })
    }
  } finally {
    toggling.value = { ...toggling.value, [item.id]: false }
  }
}

// ---------------- drawer ----------------
const drawer = ref({ visible: false, id: 0 })
const openDrawer = (id: number) => {
  drawer.value.id = id
  drawer.value.visible = true
}

// ---------------- clone ----------------
const cloning = ref(false)
// Both draws retry against what is already configured: a taken port fails the
// bind on every core start until someone edits it, and a taken tag is refused
// on save. A replica's port lives on its node, so it cannot collide here.
// Bounded, so a crowded table degrades to a collision rather than a hang.
const freePort = (): number => {
  const store = Data()
  const used = new Set(
    [...store.inbounds.filter((i: any) => !i.node_id), ...store.services, ...store.endpoints]
      .map((o: any) => o?.listen_port).filter(Boolean))
  let port = RandomUtil.randomIntRange(10000, 60000)
  for (let i = 0; i < 20 && used.has(port); i++) port = RandomUtil.randomIntRange(10000, 60000)
  return port
}
const freeTag = (type: string): string => {
  const used = new Set(Data().inbounds.map((i: any) => i.tag))
  let tag = type + '-' + RandomUtil.randomSeq(3)
  for (let i = 0; i < 20 && used.has(tag); i++) tag = type + '-' + RandomUtil.randomSeq(3)
  return tag
}
const clone = async (id: number) => {
  if (cloning.value) return
  cloning.value = true
  // A failed load returns no row, and a throw anywhere below must not leave
  // `cloning` set: every later click would return at the guard above.
  try {
    const inbound = (await Data().loadInbounds([id]))[0]
    if (!inbound) return
    const newPort = freePort()
    // A hysteria port-hopping range is forwarded (iptables, outside the panel)
    // to the original's listen port, so the copy's clients would hop straight
    // onto the original. There is no range to move it to that the panel could
    // know, so the copy starts without hopping and dials its own port until
    // the operator forwards a range for it.
    const out_json = inbound.out_json ? { ...inbound.out_json } : inbound.out_json
    if (out_json) {
      delete out_json.server_ports
      delete out_json.hop_interval
      delete out_json.hop_interval_max
    }
    const newInbound = createInbound(inbound.type, {
      ...inbound,
      // A copy is a fresh inbound to start from, on even if the original is
      // parked: the original's state is not what the operator is copying.
      enable: true,
      id: 0,
      tag: freeTag(inbound.type),
      listen_port: newPort,
      addrs: followListenPort(inbound.addrs, inbound.listen_port, newPort),
      out_json,
    })
    await Data().save('inbounds', 'new', newInbound)
  } finally {
    cloning.value = false
  }
}

// ---------------- delete (with confirm) ----------------
const del = ref({ visible: false, tag: '' })
const deleting = ref(false)
const askDelete = (tag: string) => {
  del.value = { visible: true, tag }
}
const confirmDelete = async () => {
  if (del.value.tag === '') return
  deleting.value = true
  const success = await Data().save('inbounds', 'del', del.value.tag)
  if (success) del.value.visible = false
  deleting.value = false
}

// ---------------- stats ----------------
const stats = ref({ visible: false, resource: 'inbound', tag: '' })
const showStats = (tag: string) => {
  stats.value.tag = tag
  stats.value.visible = true
}
</script>

<style scoped>
.inb-card {
  padding: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.inb-head {
  padding: 15px 16px 13px;
  display: flex;
  align-items: center;
  gap: 11px;
}
.inb-ico {
  width: 38px;
  height: 38px;
  border-radius: 10px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
}
.inb-title {
  flex: 1;
  min-width: 0;
}
.inb-tag {
  font-weight: 700;
  font-size: 14px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.inb-type {
  font-size: 11.5px;
  font-weight: 600;
}
.inb-meta {
  padding: 0 16px 14px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px 14px;
}
.m-k {
  font-size: 11px;
  color: var(--text-3);
  font-weight: 600;
  margin-bottom: 3px;
}
.m-v {
  font-size: 13px;
  font-weight: 600;
}
/* Dimmed rather than hidden: it is still configured, and its clients keep it.
   The switch stays at full strength, so it reads as the way back. */
.inb-off .inb-ico,
.inb-off .inb-title,
.inb-off .inb-meta {
  opacity: 0.55;
}
.inb-actions {
  display: flex;
  border-top: 1px solid var(--line);
  margin-top: auto;
}
</style>
