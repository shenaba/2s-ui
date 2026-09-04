<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">Tun</SectionLabel>
    <Field :label="$t('types.tun.addr') + ' ' + $t('commaSeparated')">
      <input class="input mono" placeholder="172.18.0.1/30" v-model="addrs" />
    </Field>
    <div class="grid2">
      <Field :label="$t('types.tun.ifName')">
        <input class="input mono" placeholder="tun0" v-model="ifName" />
      </Field>
      <Field label="MTU">
        <input class="input mono" type="number" v-model.number="data.mtu" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="'UDP timeout' + ' (' + $t('date.m') + ')'">
        <input class="input mono" type="number" min="1" v-model.number="udpTimeout" />
      </Field>
      <Field label="Stack">
        <Select v-model="data.stack">
          <option v-for="s in ['system', 'gvisor', 'mixed']" :key="s" :value="s">{{ s }}</option>
        </Select>
      </Field>
    </div>
    <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="autoRoute" label="Auto Route" />
    </div>
    <div v-if="autoRoute" class="fade-up" style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="autoRedirect" label="Auto Redirect" />
      <SwitchLabel v-model="strictRoute" label="Strict Route" />
      <SwitchLabel v-if="data.auto_redirect" v-model="excludeMptcp" :label="$t('types.tun.excludeMptcp')" />
    </div>
    <Field v-if="autoRoute && data.auto_redirect" :label="$t('types.tun.fallbackRuleIndex')">
      <input class="input mono" type="number" min="0" v-model.number="fallbackRuleIndex" />
    </Field>
    <!-- hijack, the default, now also sets the platform's interface DNS and
         installs platform-level hijacking; native leaves both to the system.
         dns_address is what hijack answers on. -->
    <div class="grid2">
      <Field :label="$t('types.tun.dnsMode')">
        <Select v-model="dnsMode">
          <option value="">{{ $t('ui.none') }}</option>
          <option v-for="m in dnsModes" :key="m" :value="m">{{ m }}</option>
        </Select>
      </Field>
      <Field v-if="dnsMode !== 'disabled'" :label="$t('types.tun.dnsAddress') + ' ' + $t('commaSeparated')">
        <input class="input mono" placeholder="172.18.0.2" v-model="dnsAddress" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.tun.includeMac') + ' ' + $t('commaSeparated')">
        <input class="input mono" placeholder="00:11:22:33:44:55" v-model="includeMac" />
      </Field>
      <Field :label="$t('types.tun.excludeMac') + ' ' + $t('commaSeparated')">
        <input class="input mono" placeholder="00:11:22:33:44:55" v-model="excludeMac" />
      </Field>
    </div>
    <UdpNat :data="data" />
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import UdpNat from '../out/UdpNat.vue'

const props = defineProps<{ data: any }>()

const dnsModes = ['disabled', 'native', 'hijack']

const addrs = computed({
  get: (): string => props.data.address?.join(',') ?? '',
  set: (v: string) => { props.data.address = v.length > 0 ? v.split(',') : undefined },
})

const ifName = computed({
  get: (): string => props.data.interface_name ?? '',
  set: (v: string) => { props.data.interface_name = v.length > 0 ? v : undefined },
})

const udpTimeout = computed({
  get: (): number => (props.data.udp_timeout ? parseInt(props.data.udp_timeout.replace('m', '')) : 5),
  set: (v: number) => { props.data.udp_timeout = v > 0 ? v + 'm' : '5m' },
})

// endpoint_independent_nat is gone: since sing-box 1.12 the tun stack decides
// this for itself and the option is ignored, so offering it only invited an
// operator to set something that does nothing.

const autoRoute = computed({
  get: (): boolean => props.data.auto_route ?? false,
  set: (v: boolean) => {
    props.data.auto_route = v
    props.data.auto_redirect = v ? false : undefined
    props.data.strict_route = v ? false : undefined
  },
})

const autoRedirect = computed({
  get: (): boolean => props.data.auto_redirect ?? false,
  set: (v: boolean) => { props.data.auto_redirect = v },
})

const strictRoute = computed({
  get: (): boolean => props.data.strict_route ?? false,
  set: (v: boolean) => { props.data.strict_route = v },
})

const excludeMptcp = computed({
  get: (): boolean => props.data.exclude_mptcp ?? false,
  set: (v: boolean) => { props.data.exclude_mptcp = v },
})

const dnsMode = computed({
  get: (): string => props.data.dns_mode ?? '',
  set: (v: string) => {
    if (v.length > 0) props.data.dns_mode = v
    else delete props.data.dns_mode
    // Nothing answers on it when DNS is off, so the address goes with the mode.
    if (v === 'disabled') delete props.data.dns_address
  },
})

const list = (key: string) =>
  computed({
    get: (): string => props.data[key]?.join(',') ?? '',
    set: (v: string) => {
      const parts = v.split(',').map((s) => s.trim()).filter((s) => s.length > 0)
      if (parts.length > 0) props.data[key] = parts
      else delete props.data[key]
    },
  })
const dnsAddress = list('dns_address')
const includeMac = list('include_mac_address')
const excludeMac = list('exclude_mac_address')

const fallbackRuleIndex = computed({
  get: (): number => props.data.auto_redirect_iproute2_fallback_rule_index ?? 32768,
  set: (v: number) => {
    const val = typeof v === 'number' && !isNaN(v) && v >= 0 ? v : undefined
    props.data.auto_redirect_iproute2_fallback_rule_index = val
  },
})
</script>
