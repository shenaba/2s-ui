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
      <div v-if="!loading && inbounds.length > 0" class="assign-box">
        <b>{{ $t('node.assignExisting') }}</b>
        <Select v-model="assignMode">
          <option value="all">{{ $t('node.assignAll') }}</option>
          <option value="groups">{{ $t('node.assignGroups') }}</option>
          <option value="selected">{{ $t('node.assignSelected') }}</option>
          <option value="none">{{ $t('node.assignNone') }}</option>
        </Select>
        <div v-if="assignMode==='groups'" class="assign-list">
          <label v-for="g in groups" :key="g" class="assign-row" @click.prevent="toggleGroup(g)">
            <Check :checked="chosenGroups.has(g)" />
            <span class="assign-name">{{ g || $t('node.assignNoGroup') }}</span>
          </label>
        </div>
        <div v-if="assignMode==='selected'">
          <input v-model="search" class="input" :placeholder="$t('node.searchClients')" />
          <div class="assign-list">
            <label v-for="c in clients" :key="c.id" class="assign-row" @click.prevent="toggleClient(c.id)">
              <Check :checked="chosenClients.has(c.id)" />
              <span class="assign-name">{{ c.name }}</span>
              <small>{{ c.group }}</small>
            </label>
          </div>
        </div>
      </div>
    </div>
    <template #footer>
      <Btn @click="$emit('close')">{{ $t('actions.cancel') }}</Btn>
      <Btn variant="primary" :loading="importing" :disabled="!canImport" @click="doImport">
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
import Select from '@/components/ui/Select.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import Check from '@/components/ui/Check.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{
  visible: boolean
  nodeId: number
  nodeName: string
}>()
const emit = defineEmits<{ close: [] }>()

interface RemoteInbound { id: number; type: string; tag: string; adopted: boolean }

const inbounds = ref<RemoteInbound[]>([])
const selected = ref<Set<string>>(new Set())
const loading = ref(false)
const importing = ref(false)
const assignMode = ref<'none'|'all'|'groups'|'selected'>('all')
const chosenGroups = ref<Set<string>>(new Set())
const chosenClients = ref<Set<number>>(new Set())
const search = ref('')
const eligible = computed(() => (Data().clients ?? []).filter((c:any) => c.group!=='@cluster'))
const groups = computed(() => [...new Set<string>(eligible.value.map((c: any) => String(c.group ?? '')))].sort())
const clients = computed(() => eligible.value.filter((c:any)=>`${c.name ?? ''} ${c.group ?? ''}`.toLowerCase().includes(search.value.toLowerCase())))
const canImport = computed(() => !importing.value && selected.value.size>0 &&
  (assignMode.value!=='groups'||chosenGroups.value.size>0) &&
  (assignMode.value!=='selected'||chosenClients.value.size>0))
const toggleGroup=(g:string)=>{ const next=new Set(chosenGroups.value); next.has(g)?next.delete(g):next.add(g);chosenGroups.value=next }
const toggleClient=(id:number)=>{ const next=new Set(chosenClients.value); next.has(id)?next.delete(id):next.add(id);chosenClients.value=next }

const toggle = (tag: string) => {
  const next = new Set(selected.value)
  next.has(tag) ? next.delete(tag) : next.add(tag)
  selected.value = next
}

const load = async () => {
  loading.value = true
  selected.value = new Set()
  chosenGroups.value = new Set()
  chosenClients.value = new Set()
  search.value = ''
  assignMode.value = 'all'
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
    assignMode: assignMode.value,
    assignGroups: JSON.stringify([...chosenGroups.value]),
    clientIds: JSON.stringify([...chosenClients.value]),
  })
  importing.value = false
  if (msg.success) {
    Data().loadData()
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
.assign-box { border-top:1px solid var(--line); margin-top:12px; padding-top:14px; display:grid; gap:10px; }
.assign-list { max-height:180px; overflow:auto; border:1px solid var(--line); border-radius:8px; }
.assign-row { display:flex; gap:9px; align-items:center; padding:8px 10px; border-bottom:1px solid var(--line); font-size:12px; cursor:pointer; user-select:none; }
.assign-row:last-child { border-bottom: none; }
.assign-row:hover { background: var(--surface-3); }
.assign-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.assign-row small { margin-left:auto; color:var(--text-3); flex-shrink: 0; }
</style>
