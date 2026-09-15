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

    <div v-if="mode === 'static_key'">
      <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
        <Segmented
          v-model="useKeyPath"
          :options="[[0, $t('tls.usePath')], [1, $t('tls.useText')]]"
          @update:model-value="switchKeyMode"
        />
        <div style="flex: 1;" />
        <Btn variant="subtle" sm :loading="generating" :title="$t('actions.generate')" @click="genStaticKey">
          <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
        </Btn>
      </div>

      <div class="grid2">
        <Field v-if="useKeyPath == 0" :label="$t('types.ovpn.staticKeyPath')">
          <input class="input mono" v-model="staticKeyPath" />
        </Field>
        <Field v-else :label="$t('types.ovpn.staticKey')">
          <textarea class="input mono" rows="4" spellcheck="false" v-model="staticKeyText"></textarea>
        </Field>
        <Field :label="$t('types.ovpn.keyDirection')">
          <Select v-model="keyDirection">
            <option value="">{{ $t('none') }}</option>
            <option v-for="k in keyDirections" :key="k.value" :value="k.value">{{ k.title }}</option>
          </Select>
        </Field>
      </div>
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
import { computed, ref } from 'vue'
import { push } from 'notivue'
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import Btn from '@/components/ui/Btn.vue'
import Field from '@/components/ui/Field.vue'
import Ico from '@/components/ui/Ico.vue'
import Segmented from '@/components/ui/Segmented.vue'
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

// static_key and static_key_path are alternatives, so the two are switched
// between rather than shown together -- the same shape the ECH key uses.
const useKeyPath = ref<string | number>(props.data?.static_key ? 1 : 0)
const generating = ref(false)

const switchKeyMode = (v: string | number) => {
  if (v == 0) delete props.data.static_key
  else delete props.data.static_key_path
}

// sing-box takes the key as the file's lines; the textarea is those joined.
const staticKeyText = computed({
  get: (): string => (props.data?.static_key ? props.data.static_key.join('\n') : ''),
  set: (v: string) => {
    if (v.length > 0) props.data.static_key = v.split('\n')
    else delete props.data.static_key
  },
})

// What `openvpn --genkey secret` writes. Both ends of the tunnel have to
// carry the same one, so this fills in the text and leaves the path alone.
const genStaticKey = async () => {
  generating.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'openvpn' })
  generating.value = false
  if (!msg.success || !Array.isArray(msg.obj) || msg.obj.length === 0) return
  props.data.static_key = msg.obj
  delete props.data.static_key_path
  useKeyPath.value = 1
  push.success({ message: i18n.global.t('types.ovpn.staticKeyGenerated') })
}
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
