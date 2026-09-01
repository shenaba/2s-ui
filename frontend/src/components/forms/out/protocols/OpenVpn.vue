<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">{{ isServer ? 'OpenVPN Server' : 'OpenVPN Client' }}</SectionLabel>

    <div class="grid2">
      <Field :label="$t('types.ovpn.mode')">
        <Select v-model="mode">
          <option v-for="m in modes" :key="m.value" :value="m.value">{{ m.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('network')">
        <Select v-model="network">
          <option v-for="n in networks" :key="n.value" :value="n.value">{{ n.title }}</option>
        </Select>
      </Field>
    </div>

    <div v-if="!isServer" class="grid2">
      <Field :label="$t('out.server')">
        <input class="input mono" v-model="data.server" />
      </Field>
      <Field :label="$t('out.port')">
        <input class="input mono" type="number" min="1" max="65535" v-model.number="data.server_port" />
      </Field>
    </div>

    <!-- In tls mode the server pushes the addresses; in static_key mode there is
         nobody to push them, so they have to be configured. -->
    <Field
      v-if="isServer || mode === 'static_key'"
      :label="$t('types.ovpn.address') + ' ' + $t('commaSeparated')"
    >
      <input class="input mono" v-model="addresses" />
    </Field>

    <div v-if="!isServer" class="grid2">
      <Field :label="$t('login.username')">
        <input class="input mono" autocomplete="off" v-model="username" />
      </Field>
      <Field :label="$t('login.password')">
        <input class="input mono" type="password" autocomplete="new-password" v-model="password" />
      </Field>
    </div>

    <div v-if="isServer" class="grid2">
      <Field :label="$t('types.ovpn.maxClients')">
        <input class="input mono" type="number" min="0" v-model.number="maxClients" />
      </Field>
      <Field :label="$t('types.ovpn.duplicateCn')">
        <SwitchLabel v-model="duplicateCn" :label="$t('enable')" />
      </Field>
    </div>

    <div class="grid2">
      <Field :label="$t('types.ovpn.cipher')">
        <input class="input mono" v-model="cipher" placeholder="AES-256-GCM" />
      </Field>
      <Field :label="$t('types.ovpn.auth')">
        <input class="input mono" v-model="auth" placeholder="SHA256" />
      </Field>
    </div>

    <div v-if="mode === 'static_key'" class="grid2">
      <Field :label="$t('types.ovpn.staticKeyPath')">
        <input class="input mono" v-model="staticKeyPath" />
      </Field>
      <Field :label="$t('types.ovpn.keyDirection')">
        <Select v-model="keyDirection">
          <option value="">{{ $t('none') }}</option>
          <option v-for="k in keyDirections" :key="k.value" :value="k.value">{{ k.title }}</option>
        </Select>
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
    <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="system" :label="$t('types.ovpn.system')" />
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

const modes = [
  { title: 'TLS', value: 'tls' },
  { title: 'Static Key', value: 'static_key' },
]
const keyDirections = [
  { title: 'Server', value: 'server' },
  { title: 'Client', value: 'client' },
]

const isServer = computed((): boolean => props.data.type === 'openvpn-server')

// The server accepts tcp/udp only; the client also takes the v4/v6 forms.
const networks = computed(() => {
  const base = [
    { title: 'UDP', value: 'udp' },
    { title: 'TCP', value: 'tcp' },
  ]
  if (isServer.value) return base
  return base.concat([
    { title: 'UDPv4', value: 'udp4' },
    { title: 'UDPv6', value: 'udp6' },
    { title: 'TCPv4', value: 'tcp4' },
    { title: 'TCPv6', value: 'tcp6' },
  ])
})

const mode = computed({
  get: (): string => props.data.mode,
  // static_key mode requires a cipher, and it must be a CBC one: GCM relies on
  // the TLS key exchange for IV uniqueness, so sing-box rejects it here.
  set: (v: string) => {
    props.data.mode = v
    if (v === 'static_key') {
      if (!props.data.cipher || props.data.cipher.includes('GCM')) {
        props.data.cipher = 'AES-256-CBC'
      }
    }
  },
})

const str = (key: string) => computed({
  get: () => props.data?.[key] ?? '',
  set: (v: string) => {
    const trimmed = (v ?? '').trim()
    if (trimmed) props.data[key] = trimmed
    else delete props.data[key]
  },
})

const network = computed({
  get: () => props.data.network ?? 'udp',
  set: (v: string) => { props.data.network = v },
})
const addresses = computed({
  get: () => props.data.address?.join(',') ?? '',
  set: (v: string) => {
    if (v.endsWith(',')) return
    props.data.address = v.length > 0 ? v.split(',') : undefined
  },
})
const username = str('username')
const password = str('password')
const cipher = str('cipher')
const auth = str('auth')
const staticKeyPath = str('static_key_path')
const keyDirection = str('key_direction')
const name = str('name')
const mtu = computed({
  get: (): number => props.data?.mtu ?? 0,
  set: (v: number) => { props.data.mtu = v > 0 ? v : undefined },
})
const maxClients = computed({
  get: (): number => props.data?.max_clients ?? 0,
  set: (v: number) => { props.data.max_clients = v > 0 ? v : undefined },
})
const duplicateCn = computed({
  get: (): boolean => props.data?.duplicate_cn ?? false,
  set: (v: boolean) => { if (v) props.data.duplicate_cn = true; else delete props.data.duplicate_cn },
})
const system = computed({
  get: (): boolean => props.data?.system ?? false,
  set: (v: boolean) => { if (v) props.data.system = true; else delete props.data.system },
})
</script>
