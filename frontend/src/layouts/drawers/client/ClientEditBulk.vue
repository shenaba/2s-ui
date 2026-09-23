<template>
  <MDrawer
    :open="visible"
    icon="clients"
    color="var(--brand)"
    :title="$t('actions.editbulk')"
    :sub="$t('pages.clients')"
    :save-label="$t('actions.save')"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <Field :label="$t('actions.action')">
      <Select v-model="actionMode" @change="onActionChange">
        <option v-for="m in actionModes" :key="m.value" :value="m.value">{{ m.title }}</option>
      </Select>
    </Field>

    <template v-if="actionMode === 'change_limits'">
      <div class="grid2">
        <Field :label="$t('bulk.addDays')">
          <div style="display: flex; gap: 8px;">
            <input class="input mono" type="number" v-model.number="editData.addDays" />
            <div class="input suffix-box">{{ $t('date.d') }}</div>
          </div>
        </Field>
        <Field :label="$t('bulk.addVolume')">
          <div style="display: flex; gap: 8px;">
            <input class="input mono" type="number" v-model.number="editData.addVolume" />
            <div class="input suffix-box">{{ $t('stats.GB') }}</div>
          </div>
        </Field>
      </div>
      <div style="margin-bottom: 15px;">
        <SwitchLabel v-model="editData.enable" :label="$t('enable')" />
      </div>
    </template>

    <template v-else-if="actionMode === 'reset_policy'">
      <div style="margin-bottom: 15px;">
        <SwitchLabel v-model="editData.autoReset" :label="$t('client.autoReset')" />
      </div>
      <ResetCycleFields v-if="editData.autoReset" :data="editData" style="--suffix-box-width: 64px;" />
      <MHint v-else>{{ $t('bulk.resetPolicyOffHint') }}</MHint>
    </template>

    <Field
      v-else-if="actionMode === 'add_inbounds' || actionMode === 'remove_inbounds'"
      :label="$t('client.inboundTags')"
    >
      <div style="display: flex; flex-direction: column; gap: 8px;">
        <label
          v-for="it in inboundItems"
          :key="it.id"
          class="inb-item"
          :class="{ on: editData.inboundTags.includes(it.id) }"
          @click.prevent="toggleInbound(it.id)"
        >
          <span class="inb-dot" :class="{ online: it.online }" />
          <div style="flex: 1; min-width: 0;">
            <div style="font-weight: 700; font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{{ it.tag }}</div>
            <div class="mono" style="font-size: 11.5px; color: var(--text-3);">{{ it.type }} · :{{ it.port }}</div>
          </div>
          <Check :checked="editData.inboundTags.includes(it.id)" />
        </label>
      </div>
    </Field>

    <div v-else-if="actionMode === 'delete_bulk'" style="margin-bottom: 15px;">
      <MHint>{{ $t('confirm') }}</MHint>
    </div>

    <hr class="form-divider" />

    <!-- target clients (legacy components/Users.vue selection model) -->
    <div style="margin-bottom: 12px;">
      <SectionLabel>{{ $t('pages.clients') }}</SectionLabel>
    </div>
    <Field>
      <Select v-model="selectedClients.model" @change="selectedClients.values = []">
        <option v-for="m in selectModels" :key="m.value" :value="m.value">{{ m.title }}</option>
      </Select>
    </Field>

    <Field v-if="selectedClients.model === 'group'" :label="$t('client.group')">
      <MultiPick v-model="groupValues" :options="groupNames" />
    </Field>

    <div v-if="selectedClients.model === 'client'" class="cl-list">
      <label
        v-for="c in clients ?? []"
        :key="c.id"
        class="cl-item"
        @click.prevent="toggleClient(c.id)"
      >
        <Check :checked="selectedClients.values.includes(c.id)" />
        <span style="flex: 1; min-width: 0; font-size: 13px; font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{{ c.name }}</span>
        <span style="font-size: 11.5px; color: var(--text-3); flex: none;">{{ c.group }}</span>
      </label>
    </div>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import ResetCycleFields from './ResetCycleFields.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import { Client } from '@/types/clients'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Check from '@/components/ui/Check.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MultiPick from '@/components/ui/MultiPick.vue'
import MHint from '@/components/ui/MHint.vue'

const props = defineProps<{
  visible: boolean
  clients: any[]
  inboundTags: { title: string; value: number }[]
  // optional preselection coming from the table bulk-select toolbar
  selected?: number[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const loading = ref(false)
const actionMode = ref('change_limits')
const editData = ref({
  enable: true,
  addDays: 0,
  addVolume: 0,
  inboundTags: [] as number[],
  autoReset: true,
  resetDays: 0,
  resetDayOfMonth: 1,
})
const selectedClients = ref({
  model: 'none',
  values: [] as any[],
})

const actionModes = computed(() => [
  { title: t('bulk.changeLimits'), value: 'change_limits' },
  { title: t('bulk.resetPolicy'), value: 'reset_policy' },
  { title: t('bulk.addInbounds'), value: 'add_inbounds' },
  { title: t('bulk.removeInbounds'), value: 'remove_inbounds' },
  { title: t('actions.delbulk'), value: 'delete_bulk' },
])
const selectModels = computed(() => [
  { title: t('none'), value: 'none' },
  { title: t('all'), value: 'all' },
  { title: t('client.group'), value: 'group' },
  { title: t('pages.clients'), value: 'client' },
])

const groupNames = computed((): string[] =>
  Array.from(new Set((props.clients ?? []).map((c: any) => String(c.group ?? '')))).filter((g) => g.length > 0),
)
const groupValues = computed<string[]>({
  get: () => selectedClients.value.values as string[],
  set: (v) => { selectedClients.value.values = v },
})

const inboundItems = computed(() => props.inboundTags.map((it) => {
  const inb = Data().inbounds?.find((i: any) => i.id == it.value)
  return {
    id: it.value,
    tag: it.title,
    type: inb?.type ?? '',
    port: inb?.listen_port ?? '',
    online: Data().onlines?.inbound ? Data().onlines.inbound.includes(it.title) : false,
  }
}))

const onActionChange = () => {
  editData.value.inboundTags = []
}
const toggleInbound = (id: number) => {
  const list = editData.value.inboundTags
  editData.value.inboundTags = list.includes(id) ? list.filter((i) => i !== id) : [...list, id]
}
const toggleClient = (id: number) => {
  const list = selectedClients.value.values
  selectedClients.value.values = list.includes(id) ? list.filter((i: any) => i !== id) : [...list, id]
}

// identical to legacy modals/ClientEditBulk.vue
const getTargetClients = (): Client[] => {
  const clients = props.clients ?? []
  switch (selectedClients.value.model) {
    case 'all':
      return clients
    case 'group':
      return clients.filter((c: any) => selectedClients.value.values.includes(c.group))
    case 'client':
      return clients.filter((c: any) => selectedClients.value.values.includes(c.id))
    default:
      return []
  }
}

const saveChanges = async () => {
  if (!props.visible) return
  const targetClients = getTargetClients()
  // legacy modal disabled the save button with no targets; keep it a no-op
  if (targetClients.length === 0) return
  loading.value = true
  switch (actionMode.value) {
    case 'change_limits':
      targetClients.forEach((c: Client) => {
        if (editData.value.addVolume != 0 && c.volume > 0)
          c.volume += editData.value.addVolume * (1024 ** 3)
        if (editData.value.addDays != 0 && c.expiry > 0)
          c.expiry += editData.value.addDays * (24 * 60 * 60)
        if (editData.value.enable)
          c.enable = (c.volume == 0 || c.up + c.down < c.volume) && (c.expiry == 0 || c.expiry > Date.now() / 1000)
      })
      break
    case 'add_inbounds':
      targetClients.forEach((c: Client) => {
        editData.value.inboundTags.forEach((tg: number) => {
          if (!c.inbounds.includes(tg)) {
            c.inbounds.push(tg)
          }
        })
        c.inbounds = c.inbounds.sort()
      })
      break
    case 'remove_inbounds':
      targetClients.forEach((c: Client) => {
        c.inbounds = c.inbounds.filter((i: number) => !editData.value.inboundTags.includes(i))
      })
      break
    case 'reset_policy': {
      // 独立的 act,不走 editbulk:后端那条路会把每个 reset 列从旧行恢复回来,
      // 正好是这里要改的字段
      const success = await Data().save('clients', 'resetpolicy', {
        ids: targetClients.map((c: Client) => c.id),
        autoReset: editData.value.autoReset,
        resetDays: editData.value.autoReset ? editData.value.resetDays : 0,
        resetDayOfMonth: editData.value.autoReset ? editData.value.resetDayOfMonth : 0,
      })
      if (success) emit('close')
      loading.value = false
      return
    }
    case 'delete_bulk': {
      const success = await Data().save('clients', 'delbulk', targetClients.map((c: Client) => c.id))
      if (success) emit('close')
      loading.value = false
      return
    }
  }
  const success = await Data().save('clients', 'editbulk', targetClients)
  if (success) emit('close')
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) {
    actionMode.value = 'change_limits'
    editData.value = {
      enable: true, addDays: 0, addVolume: 0, inboundTags: [],
      autoReset: true, resetDays: 0, resetDayOfMonth: 1,
    }
    selectedClients.value = (props.selected && props.selected.length > 0)
      ? { model: 'client', values: [...props.selected] }
      : { model: 'none', values: [] }
    loading.value = false
  }
})
</script>

<style scoped>
.inb-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 14px;
  border-radius: 11px;
  border: 1px solid var(--line);
  background: var(--surface);
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.inb-item.on { background: var(--brand-soft); }
.inb-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-3); flex: none; }
.inb-dot.online { background: var(--emerald); }
.cl-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  max-height: 260px;
  overflow-y: auto;
  border: 1px solid var(--line);
  border-radius: 11px;
  padding: 6px;
}
.cl-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 9px;
  border-radius: 8px;
  cursor: pointer;
}
.cl-item:hover { background: var(--surface-3); }
.suffix-box {
  width: 64px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-2);
}
</style>
