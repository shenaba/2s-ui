<template>
  <div style="margin-bottom: 15px;">
    <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
      <SectionLabel>{{ $t('objects.listen') }}</SectionLabel>
      <div style="flex: 1;" />
      <Pop :min-width="210" direction="down">
        <template #trigger="{ toggle }">
          <Btn variant="subtle" sm @click="toggle">{{ $t('listen.options') }}</Btn>
        </template>
        <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
          <div class="pop-item"><SwitchLabel v-model="optionDetour" :label="$t('listen.detour')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionTCP" :label="$t('listen.tcpOptions')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionUDP" :label="$t('listen.udpOptions')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionTcpKeepAlive" :label="$t('listen.tcpKeepAlive')" /></div>
          <div v-if="proxyProtocol || optionProxyProtocol" class="pop-item"><SwitchLabel v-model="optionProxyProtocol" :label="$t('listen.proxyProtocol')" /></div>
        </div>
      </Pop>
    </div>

    <div class="grid2">
      <Field :label="$t('in.addr')">
        <input class="input mono" required v-model="data.listen" />
      </Field>
      <Field :label="$t('in.port')">
        <input class="input mono" type="number" min="1" max="65535" required v-model.number="data.listen_port" />
      </Field>
      <Field v-if="optionDetour" :label="$t('listen.detourText')">
        <Select v-model="data.detour">
          <option v-for="tag in inTags" :key="tag" :value="tag">{{ tag }}</option>
        </Select>
      </Field>
      <Field v-if="optionUDP" :label="'UDP NAT expiration (' + $t('date.m') + ')'">
        <input class="input mono" type="number" min="1" v-model.number="udpTimeout" />
      </Field>
    </div>

    <MHint v-if="proxyProtocol && optionProxyProtocol" style="margin-bottom: 14px;">{{ $t('listen.proxyProtocolHint') }}</MHint>

    <div v-if="optionTCP || optionUDP" style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-if="optionTCP" v-model="tcpFastOpen" label="TCP Fast Open" />
      <SwitchLabel v-if="optionTCP" v-model="tcpMultiPath" label="TCP Multi Path" />
      <SwitchLabel v-if="optionUDP" v-model="udpFragment" label="UDP Fragment" />
    </div>

    <template v-if="optionTcpKeepAlive">
      <div style="margin-bottom: 12px;">
        <SwitchLabel v-model="disableTcpKeepAlive" :label="$t('listen.disableTcpKeepAlive')" />
      </div>
      <div class="grid2">
        <Field :label="$t('listen.tcpKeepAlive')">
          <input class="input mono" v-model="data.tcp_keep_alive" />
        </Field>
        <Field :label="$t('listen.tcpKeepAliveInterval')">
          <input class="input mono" v-model="data.tcp_keep_alive_interval" />
        </Field>
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MHint from '@/components/ui/MHint.vue'
import { valueField } from '@/plugins/formField'

// proxyProtocol: whether this inbound can read a PROXY header at all
// (ProxyProtocolTypes, and not over the QUIC transport). The switch is not
// offered otherwise -- except to turn it off where it is already on, which
// the save would refuse.
const props = defineProps<{ data: any; inTags?: string[]; proxyProtocol?: boolean }>()

const d = () => props.data

const udpTimeout = computed({
  get: () => props.data.udp_timeout ? parseInt(props.data.udp_timeout.replace('m', '')) : 5,
  set: (v: number) => { props.data.udp_timeout = v > 0 ? v + 'm' : '5m' },
})
const tcpFastOpen = valueField(d, 'tcp_fast_open', false)
const tcpMultiPath = valueField(d, 'tcp_multi_path', false)
const udpFragment = valueField(d, 'udp_fragment', false)
const disableTcpKeepAlive = valueField(d, 'disable_tcp_keep_alive', false)

const optionTCP = computed({
  get: (): boolean =>
    props.data.tcp_fast_open != undefined &&
    props.data.tcp_multi_path != undefined,
  set: (v: boolean) => {
    props.data.tcp_fast_open = v ? false : undefined
    props.data.tcp_multi_path = v ? false : undefined
  },
})
const optionUDP = computed({
  get: (): boolean =>
    props.data.udp_fragment != undefined &&
    props.data.udp_timeout != undefined,
  set: (v: boolean) => {
    props.data.udp_fragment = v ? false : undefined
    props.data.udp_timeout = v ? '5m' : undefined
  },
})
const optionDetour = computed({
  get: (): boolean => props.data.detour != undefined,
  set: (v: boolean) => { props.data.detour = v ? (props.inTags ?? [])[0] ?? '' : undefined },
})
// Present only while on: sing-box's own listener refuses the field outright,
// and only the panel's copies of these protocols take it back out (#260).
const optionProxyProtocol = computed({
  get: (): boolean => props.data.proxy_protocol == true,
  set: (v: boolean) => {
    if (v) props.data.proxy_protocol = true
    else delete props.data.proxy_protocol
  },
})
const optionTcpKeepAlive = computed({
  get: (): boolean =>
    props.data.disable_tcp_keep_alive != undefined ||
    props.data.tcp_keep_alive != undefined ||
    props.data.tcp_keep_alive_interval != undefined,
  set: (v: boolean) => {
    if (v) {
      props.data.tcp_keep_alive = '5m'
      props.data.tcp_keep_alive_interval = '75s'
    } else {
      delete props.data.disable_tcp_keep_alive
      delete props.data.tcp_keep_alive
      delete props.data.tcp_keep_alive_interval
    }
  },
})
</script>
