<template>
  <div>
    <SwitchLabel v-model="show" :label="quic ? 'QUIC' : 'HTTP/2'" />
    <template v-if="show">
      <div class="grid2" style="margin-top: 12px;">
        <Field :label="$t('quic.idleTimeout')">
          <input class="input mono" v-model="idleTimeout" placeholder="30s" />
        </Field>
        <Field :label="$t('quic.keepAlive')">
          <input class="input mono" v-model="keepAlivePeriod" placeholder="0s" />
        </Field>
      </div>
      <!-- Byte sizes take a plain number or a string with a unit, e.g. 8mb. -->
      <div class="grid2">
        <Field :label="$t('quic.streamWindow')">
          <input class="input mono" v-model="streamReceiveWindow" placeholder="8mb" />
        </Field>
        <Field :label="$t('quic.connectionWindow')">
          <input class="input mono" v-model="connectionReceiveWindow" placeholder="16mb" />
        </Field>
      </div>
      <div class="grid2">
        <Field :label="$t('quic.maxStreams')">
          <input class="input mono" type="number" min="0" v-model.number="maxConcurrentStreams" />
        </Field>
        <Field v-if="quic" :label="$t('quic.initialPacketSize')">
          <input class="input mono" type="number" min="0" v-model.number="initialPacketSize" />
        </Field>
      </div>
      <div v-if="quic" style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
        <SwitchLabel v-model="disableMtu" :label="$t('quic.disableMtuDiscovery')" />
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import Field from '@/components/ui/Field.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

// The transport tuning sing-box flattens into whatever carries it, replacing
// hysteria's own recv_window_* options. Every field is optional, and an empty
// one is deleted rather than written out: sing-box rejects "" for a duration or
// a byte size, so a revealed but unfilled group has to leave no trace.
const http2Keys = [
  'idle_timeout',
  'keep_alive_period',
  'stream_receive_window',
  'connection_receive_window',
  'max_concurrent_streams',
]
const quicKeys = ['initial_packet_size', 'disable_path_mtu_discovery']

const props = defineProps<{ data: any; quic?: boolean }>()

const fields = computed((): string[] => (props.quic ? http2Keys.concat(quicKeys) : http2Keys))

// Revealing the group writes nothing, so clearing the last field does not make
// the whole section collapse under the operator mid-edit.
const show = ref(false)
watch(
  () => props.data,
  () => { show.value = fields.value.some((k) => props.data?.[k] != undefined) },
  { immediate: true },
)
// Turning it off clears what it owns; individual fields are already removed as
// they are emptied.
watch(show, (v) => { if (!v) fields.value.forEach((k) => delete props.data[k]) })

const text = (key: string) => props.data?.[key] ?? ''
const setText = (key: string, value: string) => {
  const trimmed = (value ?? '').trim()
  if (trimmed) props.data[key] = trimmed
  else delete props.data[key]
}
const num = (key: string) => props.data?.[key] ?? 0
const setNum = (key: string, value: number) => {
  if (value > 0) props.data[key] = value
  else delete props.data[key]
}

const idleTimeout = computed({
  get: () => text('idle_timeout'),
  set: (v: string) => setText('idle_timeout', v),
})
const keepAlivePeriod = computed({
  get: () => text('keep_alive_period'),
  set: (v: string) => setText('keep_alive_period', v),
})
const streamReceiveWindow = computed({
  get: () => String(props.data?.stream_receive_window ?? ''),
  set: (v: string) => setText('stream_receive_window', v),
})
const connectionReceiveWindow = computed({
  get: () => String(props.data?.connection_receive_window ?? ''),
  set: (v: string) => setText('connection_receive_window', v),
})
const maxConcurrentStreams = computed({
  get: () => num('max_concurrent_streams'),
  set: (v: number) => setNum('max_concurrent_streams', v),
})
const initialPacketSize = computed({
  get: () => num('initial_packet_size'),
  set: (v: number) => setNum('initial_packet_size', v),
})
const disableMtu = computed({
  get: (): boolean => props.data?.disable_path_mtu_discovery ?? false,
  set: (v: boolean) => {
    if (v) props.data.disable_path_mtu_discovery = true
    else delete props.data.disable_path_mtu_discovery
  },
})
</script>
