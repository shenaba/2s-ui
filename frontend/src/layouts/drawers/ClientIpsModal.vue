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

type OnlineIP = { ip: string; since: number; idle: boolean }

const loading = ref(false)
const ips = ref<OnlineIP[]>([])
// Which client the in-flight request is for, so a response that arrives after
// the modal was reopened on someone else is dropped instead of rendered.
const pending = ref('')

// Fetched once per open rather than subscribed: the ws topic list is a fixed
// union, and a modal opened occasionally does not justify a fourth topic.
const loadData = async () => {
  if (!props.name) return
  const name = props.name
  pending.value = name
  loading.value = true
  const data = await HttpUtils.get('api/onlineIps', { name })
  if (pending.value !== name) return
  ips.value = data.success ? (data.obj?.ips ?? []) : []
  loading.value = false
}

watch(() => props.visible, (open) => {
  if (open) {
    loadData()
    return
  }
  // Clearing pending also disowns any in-flight request.
  pending.value = ''
  ips.value = []
})

const formatSince = (unix: number): string =>
  new Date(unix * 1000).toLocaleTimeString(intlLocale())
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
