<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">{{ $t('pages.basics') }}</SectionLabel>
    <div class="grid2">
      <Field v-if="type == inTypes.SOCKS" :label="$t('version')">
        <Select v-model="inData.out_json.version">
          <option v-for="v in ['4', '4a', '5']" :key="v" :value="v">{{ v }}</option>
        </Select>
      </Field>
      <Network v-if="needNetwork" :data="inData.out_json" :mb="15" />
      <UoT v-if="needUot" :data="inData.out_json" :mb="15" />
      <Field v-if="type == inTypes.HTTP" :label="$t('transport.path')">
        <input class="input mono" v-model="inData.out_json.path" />
      </Field>
      <Field v-if="type == inTypes.VMess || type == inTypes.VLESS" :label="$t('types.vless.udpEnc')">
        <Select v-model="packet_encoding">
          <option v-for="e in ['none', 'packetaddr', 'xudp']" :key="e" :value="e">{{ e }}</option>
        </Select>
      </Field>
      <Field v-if="type == inTypes.VMess" :label="$t('types.vmess.security')">
        <Select v-model="inData.out_json.security">
          <option v-for="s in vmessSecurities" :key="s" :value="s">{{ s }}</option>
        </Select>
      </Field>
      <!-- recv_window was hysteria's own name for the QUIC stream window; the
           shared QUIC fields on the outbound form carry it now. -->
      <Field v-if="type == inTypes.TUIC" label="UDP Relay Mode">
        <Select v-model="udpRelayMode">
          <option value="">{{ $t('none') }}</option>
          <option v-for="m in ['native', 'quic']" :key="m" :value="m">{{ m }}</option>
        </Select>
      </Field>
    </div>
    <div v-if="type == inTypes.VMess" style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="globalPadding" :label="$t('types.vmess.globalPadding')" />
      <SwitchLabel v-model="authenticatedLength" :label="$t('types.vmess.authLen')" />
    </div>
    <div v-if="type == inTypes.TUIC" style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="udpOverStream" label="UDP Over Stream" />
    </div>
    <template v-if="type == inTypes.Hysteria || type == inTypes.Hysteria2">
      <Field :label="$t('rule.portRange') + ' ' + $t('commaSeparated')">
        <input class="input mono" v-model="server_ports" />
      </Field>
      <Field :label="$t('ruleset.interval') + ' (' + $t('date.s') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="hop_interval" />
      </Field>
    </template>
    <template v-if="type == inTypes.Shadowsocks">
      <Field label="Plugin">
        <input class="input mono" v-model="plugin" />
      </Field>
      <Field v-if="inData.out_json.plugin" label="Plugin Options">
        <input class="input mono" v-model="pluginOpts" />
      </Field>
    </template>
    <Headers v-if="type == inTypes.HTTP" :data="inData.out_json" />
    <AnyTls v-if="type == inTypes.AnyTls" :data="inData.out_json" direction="out_json" />
    <Naive v-if="type == inTypes.Naive" :data="inData.out_json" direction="out_json" />
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import { InTypes } from '@/types/inbounds'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import Network from '../out/Network.vue'
import UoT from '../out/UoT.vue'
import Headers from '../out/Headers.vue'
import AnyTls from './AnyTls.vue'
import Naive from './Naive.vue'

const props = defineProps<{ inData: any; type: string }>()

const inTypes = InTypes

const vmessSecurities = [
  'auto',
  'none',
  'zero',
  'aes-128-gcm',
  'aes-128-ctr',
  'chacha20-poly1305',
]

const haveNetwork = [
  InTypes.SOCKS,
  InTypes.Shadowsocks,
  InTypes.VMess,
  InTypes.Trojan,
  InTypes.Hysteria,
  InTypes.VLESS,
  InTypes.TUIC,
  InTypes.Hysteria2,
]

const havUoT = [InTypes.SOCKS, InTypes.Shadowsocks]

const needNetwork = computed((): boolean => haveNetwork.includes(props.type))
const needUot = computed((): boolean => havUoT.includes(props.type))

const packet_encoding = computed({
  get: (): string =>
    props.inData.out_json.packet_encoding != undefined ? props.inData.out_json.packet_encoding : 'none',
  set: (v: string) => { props.inData.out_json.packet_encoding = v != 'none' ? v : undefined },
})

const globalPadding = computed({
  get: (): boolean => props.inData.out_json.global_padding ?? false,
  set: (v: boolean) => { props.inData.out_json.global_padding = v },
})

const authenticatedLength = computed({
  get: (): boolean => props.inData.out_json.authenticated_length ?? false,
  set: (v: boolean) => { props.inData.out_json.authenticated_length = v },
})

const udpOverStream = computed({
  get: (): boolean => props.inData.out_json.udp_over_stream ?? false,
  set: (v: boolean) => { props.inData.out_json.udp_over_stream = v },
})

const udpRelayMode = computed({
  get: (): string => props.inData.out_json.udp_relay_mode ?? '',
  set: (v: string) => {
    if (v === '') delete props.inData.out_json.udp_relay_mode
    else props.inData.out_json.udp_relay_mode = v
  },
})

const server_ports = computed({
  get: (): string => props.inData.out_json.server_ports?.join(',') ?? '',
  set: (v: string) => { props.inData.out_json.server_ports = v.length > 0 ? v.split(',') : undefined },
})

const hop_interval = computed({
  get: (): number =>
    props.inData.out_json.hop_interval ? parseInt(props.inData.out_json.hop_interval.replace('s', '')) : 0,
  set: (v: number) => { props.inData.out_json.hop_interval = v > 0 ? v + 's' : undefined },
})

// These two ride in out_json rather than the inbound itself: sing-box has no
// server-side plugin option, but the client needs `?plugin=` in its ss:// link.
const plugin = computed({
  get: (): string => props.inData.out_json.plugin ?? '',
  set: (v: string) => {
    props.inData.out_json.plugin = v.length > 0 ? v : undefined
    if (!props.inData.out_json.plugin) props.inData.out_json.plugin_opts = undefined
  },
})

const pluginOpts = computed({
  get: (): string => props.inData.out_json.plugin_opts ?? '',
  set: (v: string) => { props.inData.out_json.plugin_opts = v.length > 0 ? v : undefined },
})
</script>
