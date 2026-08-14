<template>
  <Modal :open="visible" :title="$t('ui.ipListTitle')" :width="480" @close="$emit('close')">
    <div style="padding: 18px 20px;">
      <div style="display: flex; align-items: center; gap: 12px; margin-bottom: 14px;">
        <Chip color="brand"><span class="mono">{{ name }}</span></Chip>
        <div style="flex: 1;" />
        <Btn sm variant="subtle" :disabled="loading" @click="loadData">
          <Ico name="refresh" :size="14" /> {{ $t('ui.ipRefresh') }}
        </Btn>
      </div>

      <div v-if="loading" style="padding: 20px 0;">
        <EmptyState icon="globe" :title="$t('loading')" />
      </div>
      <div v-else-if="ips.length == 0" style="padding: 20px 0;">
        <EmptyState icon="globe" :title="$t('ui.ipNone')" />
      </div>
      <div v-else class="ip-list">
        <div v-for="ip in ips" :key="ip.ip" class="ip-row">
          <span class="mono" dir="ltr" style="font-size: 13px; font-weight: 600;">{{ ip.ip }}</span>
          <Chip v-if="ip.idle" color="amber">{{ $t('ui.ipIdle') }}</Chip>
          <div style="flex: 1;" />
          <span style="font-size: 11.5px; color: var(--text-3);">
            {{ $t('ui.ipSince') }} {{ formatSince(ip.since) }}
          </span>
        </div>
      </div>
    </div>
  </Modal>
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { intlLocale } from '@/locales'
import Modal from '@/components/ui/Modal.vue'
import Chip from '@/components/ui/Chip.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{
  visible: boolean
  name: string
}>()
defineEmits<{ close: [] }>()

// idle is null when the backend cannot tell: it only stamps per-connection
// activity while some client has an IP limit, so with none set the answer would
// be connection age, not inactivity. No badge is the honest rendering.
type OnlineIP = { ip: string; since: number; idle: boolean | null }

const loading = ref(false)
const ips = ref<OnlineIP[]>([])
// Identifies the request, not the client it is for. Keying on the name cannot
// disown a superseded request for that same name -- close and reopen on one
// client leaves two in flight, both matching -- so whichever landed last won,
// and an older response arriving second overwrote the newer list.
let issued = 0
let awaited = 0

// Fetched once per open rather than subscribed: the ws topic list is a fixed
// union, and a modal opened occasionally does not justify a fourth topic.
const loadData = async () => {
  if (!props.name) return
  const name = props.name
  const seq = ++issued
  awaited = seq
  loading.value = true
  const data = await HttpUtils.get('api/onlineIps', { name })
  // Superseded: either a newer request is still running, in which case the
  // spinner belongs to it, or the modal closed and the watcher cleared it.
  if (awaited !== seq) return
  loading.value = false
  ips.value = data.success ? (data.obj?.ips ?? []) : []
}

watch(() => props.visible, (open) => {
  if (open) {
    loadData()
    return
  }
  // Bumping the sequence disowns whatever is in flight, so its response cannot
  // land on the next open.
  awaited = ++issued
  loading.value = false
  ips.value = []
})

// Date included, not just the time: the point of the list is judging which IPs
// are stale, and a tunnel opened three days ago must not read as this afternoon.
const formatSince = (unix: number): string =>
  new Date(unix * 1000).toLocaleString(intlLocale())
</script>

<style scoped>
.ip-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.ip-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 10px;
  border-radius: 8px;
  background: var(--surface-3);
}
</style>
