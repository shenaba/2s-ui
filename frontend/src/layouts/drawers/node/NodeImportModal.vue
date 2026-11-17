<template>
  <Modal :open="visible" :title="$t('ui.nodeImportTitle')" :width="480" @close="$emit('close')">
    <div style="padding: 16px 18px; display: flex; flex-direction: column; gap: 4px;">
      <div v-if="loading" style="padding: 24px; text-align: center; color: var(--text-3); font-size: 13px;">
        {{ $t('loading') }}
      </div>
      <EmptyState
        v-else-if="inbounds.length === 0"
        icon="inbound"
        :title="$t('node.noRemoteInbounds')"
      />
      <label
        v-for="ib in inbounds"
        :key="ib.tag"
        class="imp-row"
        :class="{ disabled: ib.adopted }"
        @click.prevent="!ib.adopted && toggle(ib.tag)"
      >
        <Check :checked="selected.has(ib.tag)" />
        <div style="flex: 1; min-width: 0;">
          <div class="imp-tag">{{ ib.tag }}</div>
          <div class="imp-type">{{ ib.type }}</div>
        </div>
        <Chip v-if="ib.adopted" color="emerald">{{ $t('node.adopted') }}</Chip>
      </label>
      <div v-if="!loading && assignable" class="assign-box">
        <Users :clients="eligible" :data="initUsers" />
      </div>
    </div>
    <template #footer>
      <Btn @click="$emit('close')">{{ $t('actions.cancel') }}</Btn>
      <Btn variant="primary" :loading="importing" :disabled="selected.size === 0" @click="doImport">
        <Ico name="download" :size="15" /> {{ $t('node.import') }}
      </Btn>
    </template>
  </Modal>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import Modal from '@/components/ui/Modal.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import Check from '@/components/ui/Check.vue'
import EmptyState from '@/components/ui/EmptyState.vue'
import Users from '@/components/forms/in/Users.vue'
import { InitUsers, bindableClients, resolveInitUsers } from '@/types/clients'

const props = defineProps<{
  visible: boolean
  nodeId: number
  nodeName: string
}>()
const emit = defineEmits<{ close: [] }>()

// takesUsers: whether clients can be bound to it (the node's own rule, which
// sees the shadowtls version and the shadowsocks managed flag; the type alone
// cannot tell those apart).
interface RemoteInbound { id: number; type: string; tag: string; adopted: boolean; takesUsers?: boolean }

const inbounds = ref<RemoteInbound[]>([])
const selected = ref<Set<string>>(new Set())
const loading = ref(false)
const importing = ref(false)
// The same clients picker the inbound drawer shows for a new inbound, over the
// same bindable clients, resolved to ids the same way.
const initUsers = ref<InitUsers>({ model: 'none', values: [] })
const eligible = computed(() => bindableClients(Data().clients ?? []))
// Only offered when something selected can carry users. takesUsers is the
// backend's nodeInboundTakesUsers, the same test adoption binds by.
const assignable = computed(() =>
  inbounds.value.some((ib) => selected.value.has(ib.tag) && ib.takesUsers))

const toggle = (tag: string) => {
  const next = new Set(selected.value)
  next.has(tag) ? next.delete(tag) : next.add(tag)
  selected.value = next
}

const load = async () => {
  loading.value = true
  selected.value = new Set()
  initUsers.value = { model: 'none', values: [] }
  inbounds.value = []
  const msg = await HttpUtils.get('api/nodeInbounds', { id: props.nodeId })
  loading.value = false
  if (msg.success) inbounds.value = msg.obj ?? []
}

const doImport = async () => {
  importing.value = true
  const msg = await HttpUtils.post('api/adoptInbounds', {
    id: props.nodeId,
    tags: JSON.stringify([...selected.value]),
    initUsers: assignable.value ? resolveInitUsers(eligible.value, initUsers.value).join(',') : '',
  })
  importing.value = false
  if (msg.success) {
    Data().loadData(true)
    emit('close')
  }
}

watch(() => props.visible, (v) => { if (v) load() })
</script>

<style scoped>
.imp-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 6px;
  border-bottom: 1px solid var(--line);
  cursor: pointer;
}
.imp-row:last-child { border-bottom: none; }
.imp-row.disabled { cursor: default; opacity: 0.6; }
.imp-tag { font-weight: 600; font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.imp-type { font-size: 11.5px; color: var(--text-3); }
.assign-box { border-top: 1px solid var(--line); margin-top: 12px; padding-top: 14px; }
</style>
