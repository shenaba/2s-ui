<template>
  <Field :label="$t('ui.uuid')">
    <KeyInput v-model="data.uuid" @regenerate="data.uuid = RandomUtil.randomUUID()" />
  </Field>
  <div class="grid2" style="margin-bottom: 15px;">
    <Field :label="$t('types.pw')" :mb="0">
      <input class="input mono" type="password" v-model="data.password" />
    </Field>
    <Network :data="data" />
    <Field label="UDP Relay Mode" :mb="0">
      <Select v-model="udpRelayMode">
        <option value="">{{ $t('none') }}</option>
        <option value="native">native</option>
        <option value="quic">quic</option>
      </Select>
    </Field>
    <Field :label="$t('types.tuic.congControl')" :mb="0">
      <Select v-model="data.congestion_control">
        <option v-for="c in congestionControls" :key="c" :value="c">{{ c }}</option>
      </Select>
    </Field>
    <Field :label="$t('types.tuic.hb') + ' (' + $t('date.s') + ')'" :mb="0">
      <input class="input mono" type="number" min="1" v-model.number="heartbeat" />
    </Field>
  </div>
  <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 15px;">
    <SwitchLabel v-model="udpOverStream" label="UDP Over Stream" />
    <SwitchLabel v-model="zeroRtt" label="Zero-RTT Handshake" />
  </div>
  <QuicFields :data="data" quic />
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import QuicFields from '../QuicFields.vue'
import RandomUtil from '@/plugins/randomUtil'
import Field from '@/components/ui/Field.vue'
import KeyInput from '@/components/ui/KeyInput.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import Network from '../Network.vue'

const props = defineProps<{ data: any }>()

const congestionControls = ['cubic', 'new_reno', 'bbr']

const udpRelayMode = computed({
  get: () => props.data.udp_relay_mode ?? '',
  set: (v: string) => {
    if (v == '') delete props.data.udp_relay_mode
    else props.data.udp_relay_mode = v
  },
})
const udpOverStream = computed({
  get: () => props.data.udp_over_stream ?? false,
  set: (v: boolean) => { props.data.udp_over_stream = v },
})
const zeroRtt = computed({
  get: () => props.data.zero_rtt_handshake ?? false,
  set: (v: boolean) => { props.data.zero_rtt_handshake = v },
})
const heartbeat = computed({
  get: () => props.data.heartbeat ? parseInt(props.data.heartbeat.replace('s', '')) : '',
  set: (v: number) => { props.data.heartbeat = v ? v + 's' : '' },
})
</script>
