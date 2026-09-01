<template>
  <MDrawer
    :open="open"
    icon="rules"
    color="var(--cyan)"
    :title="isNew ? $t('ui.httpClientNew') : client.tag"
    :sub="$t('httpClient.sub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="520"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <div class="grid2">
      <Field :label="$t('objects.tag')" :hint="tagError">
        <input class="input mono" v-model="client.tag" />
      </Field>
      <Field :label="$t('httpClient.version')">
        <Select v-model="version">
          <option v-for="v in versions" :key="v.value" :value="v.value">{{ v.title }}</option>
        </Select>
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('httpClient.engine')">
        <Select v-model="engine">
          <option value="">{{ $t('none') }}</option>
          <option v-for="e in engines" :key="e.value" :value="e.value">{{ e.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('httpClient.versionFallback')">
        <SwitchLabel v-model="disableVersionFallback" :label="$t('disable')" />
      </Field>
    </div>

    <Headers :data="client" />
    <Dial :dial="client" mode="client" />
    <!-- Which tuning group applies is decided by the version: HTTP/1.1 has
         neither, HTTP/3 is QUIC, anything else is HTTP/2. -->
    <QuicFields v-if="fieldGroup != 'none'" :data="client" :quic="fieldGroup == 'quic'" />
  </MDrawer>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import Headers from '@/components/forms/out/Headers.vue'
import Dial from '@/components/forms/out/Dial.vue'
import QuicFields from '@/components/forms/out/QuicFields.vue'
import RandomUtil from '@/plugins/randomUtil'
import { i18n } from '@/locales'
import { HttpClient, HttpVersion, createHttpClient, httpFieldGroup } from '@/types/httpClient'

// The tuning fields belong to one transport or the other, so switching version
// drops the ones that no longer apply rather than leaving them for sing-box to
// reject.
const versionKeys = [
  'idle_timeout',
  'keep_alive_period',
  'stream_receive_window',
  'connection_receive_window',
  'max_concurrent_streams',
  'initial_packet_size',
  'disable_path_mtu_discovery',
]

const props = defineProps<{
  open: boolean
  index: number
  data: string
  tags: string[]
}>()
const emit = defineEmits<{ close: []; save: [data: HttpClient] }>()

const isNew = computed(() => props.index === -1)

const client = ref<HttpClient>(createHttpClient(''))

function init() {
  if (props.index !== -1) {
    client.value = <HttpClient>JSON.parse(props.data)
  } else {
    client.value = createHttpClient('hc-' + RandomUtil.randomSeq(3))
  }
}
watch(() => props.open, (v) => { if (v) init() })

const engines = [
  { title: 'Go', value: 'go' },
  { title: 'Apple', value: 'apple' },
]
const versions = [
  { title: 'Auto', value: 0 },
  { title: 'HTTP/1.1', value: 1 },
  { title: 'HTTP/2', value: 2 },
  { title: 'HTTP/3', value: 3 },
]

const fieldGroup = computed((): string => httpFieldGroup(client.value.version))

const version = computed({
  get: (): number => client.value.version ?? 0,
  set: (v: number) => {
    client.value.version = <HttpVersion>v
    const group = httpFieldGroup(<HttpVersion>v)
    if (group === 'none') {
      versionKeys.forEach((key) => delete (<any>client.value)[key])
      return
    }
    if (group === 'http2') {
      delete client.value.initial_packet_size
      delete client.value.disable_path_mtu_discovery
    }
  },
})

const engine = computed({
  get: () => client.value.engine ?? '',
  set: (v: string) => { client.value.engine = v.length > 0 ? <'go' | 'apple'>v : undefined },
})
const disableVersionFallback = computed({
  get: (): boolean => client.value.disable_version_fallback ?? false,
  set: (v: boolean) => {
    if (v) client.value.disable_version_fallback = true
    else delete client.value.disable_version_fallback
  },
})

const tagError = computed((): string => {
  const tag = client.value.tag?.trim() ?? ''
  if (tag.length === 0) return i18n.global.t('error.invalidData') + ': ' + i18n.global.t('objects.tag')
  const taken = props.index === -1
    ? props.tags
    : props.tags.filter((_, i) => i !== props.index)
  if (taken.includes(tag)) return i18n.global.t('error.dplData') + ': ' + i18n.global.t('objects.tag')
  return ''
})

function saveChanges() {
  if (tagError.value.length > 0) return
  client.value.tag = client.value.tag.trim()
  emit('save', client.value)
}
</script>
