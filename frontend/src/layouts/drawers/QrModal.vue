<template>
  <Modal :open="visible" title="QR" :width="400" @close="$emit('close')">
    <div id="qrcode-modal" style="padding: 0 20px 18px;">
      <Tabs v-model="tab" :tabs="[['sub', $t('setting.sub')], ['link', $t('client.links')]]" />
      <div v-if="loading" style="padding: 40px 0; text-align: center; color: var(--text-3); font-size: 13px;">
        {{ $t('loading') }}
      </div>
      <template v-else>
        <div v-if="tab === 'sub'" class="qr-list">
          <div class="qr-item" v-for="q in subQrs" :key="q.label">
            <Chip>{{ q.label }}</Chip>
            <QrcodeVue
              :value="q.value"
              :size="size"
              :margin="1"
              :style="{ borderRadius: '1rem', cursor: q.copy ? 'copy' : 'not-allowed' }"
              @click="q.copy && copy(q.value)"
            />
          </div>
        </div>
        <div v-else class="qr-list">
          <div class="qr-item" v-for="l in clientLinks" :key="l.uri">
            <Chip>{{ l.remark ?? $t('client.' + l.type) }}</Chip>
            <QrcodeVue :value="l.uri" :size="size" :margin="1" style="border-radius: .5rem; cursor: copy;" @click="copy(l.uri)" />
          </div>
        </div>
      </template>
    </div>
  </Modal>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import QrcodeVue from 'qrcode.vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import { copyToClipboard } from '@/plugins/clipboard'
import Modal from '@/components/ui/Modal.vue'
import Tabs from '@/components/ui/Tabs.vue'
import Chip from '@/components/ui/Chip.vue'

const props = defineProps<{
  visible: boolean
  id: number
}>()
defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const tab = ref('sub')
const client = ref<any>({})
const loading = ref(false)

const load = async () => {
  loading.value = true
  client.value = await Data().loadClients(props.id)
  loading.value = false
}

const clientSub = computed(() => Data().subURI + (client.value.name ?? ''))
const singbox = computed(() => {
  const url = clientSub.value + '?format=json'
  return 'sing-box://import-remote-profile?url=' + encodeURIComponent(url) + '#' + (client.value.name ?? '')
})
const subQrs = computed(() => [
  { label: t('setting.sub'), value: clientSub.value, copy: true },
  { label: t('setting.pageSub'), value: clientSub.value + '?format=page', copy: true },
  { label: t('setting.jsonSub'), value: clientSub.value + '?format=json', copy: true },
  { label: t('setting.clashSub'), value: clientSub.value + '?format=clash', copy: true },
  { label: 'SING-BOX', value: singbox.value, copy: false },
])
const clientLinks = computed(() => client.value.links ?? [])

const size = computed(() => {
  if (window.innerWidth > 380) return 280
  if (window.innerWidth > 330) return 260
  return 230
})

const copy = (txt: string) => copyToClipboard(txt, 'qrcode-modal')

watch(() => props.visible, (v) => {
  if (v) {
    tab.value = 'sub'
    load()
  }
})
</script>

<style scoped>
.qr-list { display: flex; flex-direction: column; gap: 18px; }
.qr-item { display: flex; flex-direction: column; align-items: center; gap: 9px; }
</style>
