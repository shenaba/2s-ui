<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">OpenConnect</SectionLabel>
    <div class="grid2">
      <Field :label="$t('out.server')">
        <input class="input mono" v-model="data.server" />
      </Field>
      <Field :label="$t('types.ovpn.flavor')">
        <Select v-model="flavor">
          <option v-for="f in flavors" :key="f.value" :value="f.value">{{ f.title }}</option>
        </Select>
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('login.username')">
        <input class="input mono" v-model="username" />
      </Field>
      <Field :label="$t('login.password')">
        <input class="input mono" type="password" v-model="password" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.ovpn.authGroup')">
        <input class="input mono" v-model="authGroup" />
      </Field>
      <Field :label="$t('types.ovpn.cookie')">
        <input class="input mono" type="password" v-model="cookie" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.ovpn.ifName')">
        <input class="input mono" v-model="name" />
      </Field>
      <Field label="MTU">
        <input class="input mono" type="number" min="0" v-model.number="mtu" />
      </Field>
    </div>
    <Field :label="$t('types.ovpn.udpTimeout')">
      <input class="input mono" v-model="udpTimeout" placeholder="5m" />
    </Field>
    <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="system" :label="$t('types.ovpn.system')" />
      <SwitchLabel v-model="noUdp" :label="$t('types.ovpn.noUdp')" />
      <SwitchLabel v-model="ipv6Disabled" :label="$t('types.ovpn.ipv6Disabled')" />
      <SwitchLabel v-model="allowInsecureCrypto" :label="$t('types.ovpn.insecureCrypto')" />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

const props = defineProps<{ data: any }>()

const flavors = [
  { title: 'Cisco AnyConnect', value: 'anyconnect' },
  { title: 'Palo Alto GlobalProtect', value: 'gp' },
  { title: 'Fortinet', value: 'fortinet' },
  { title: 'F5 BIG-IP', value: 'f5' },
  { title: 'Juniper Pulse', value: 'pulse' },
  { title: 'Junos Network Connect', value: 'nc' },
]

// Optional strings are removed rather than stored empty, so an untouched field
// leaves sing-box on its own default instead of an empty value it rejects.
const str = (key: string) => computed({
  get: () => props.data?.[key] ?? '',
  set: (v: string) => {
    const trimmed = (v ?? '').trim()
    if (trimmed) props.data[key] = trimmed
    else delete props.data[key]
  },
})
const flag = (key: string) => computed({
  get: (): boolean => props.data?.[key] ?? false,
  set: (v: boolean) => { if (v) props.data[key] = true; else delete props.data[key] },
})

const flavor = computed({
  get: () => props.data.flavor ?? 'anyconnect',
  set: (v: string) => { props.data.flavor = v },
})
const username = str('username')
const password = str('password')
const authGroup = str('auth_group')
const cookie = str('cookie')
const name = str('name')
const udpTimeout = str('udp_timeout')
const mtu = computed({
  get: (): number => props.data?.mtu ?? 0,
  set: (v: number) => { props.data.mtu = v > 0 ? v : undefined },
})
const system = flag('system')
const noUdp = flag('no_udp')
const ipv6Disabled = flag('ipv6_disabled')
const allowInsecureCrypto = flag('allow_insecure_crypto')
</script>
