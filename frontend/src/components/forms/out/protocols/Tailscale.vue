<template>
  <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
    <SectionLabel>Tailscale</SectionLabel>
    <div style="flex: 1;" />
    <Pop :min-width="220" direction="down">
      <template #trigger="{ toggle }">
        <Btn variant="subtle" sm @click="toggle">{{ $t('types.ts.options') }}</Btn>
      </template>
      <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
        <div class="pop-item"><SwitchLabel v-model="optionStateDir" :label="$t('types.ts.stateDir')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionAuth" :label="$t('types.ts.authKey')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionCtrlUrl" :label="$t('types.ts.controlUrl')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionHostname" :label="$t('types.ts.hostname')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionExitNode" :label="$t('types.ts.exitNode')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionRelay" :label="$t('types.ts.relayServer')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionSysIf" :label="$t('types.ts.systemInterface')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionAdvRoutes" :label="$t('types.ts.advRoutes')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionUdpTimeout" :label="$t('types.ts.udpTimeout')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionSsh" :label="$t('types.ts.sshServer')" /></div>
      </div>
    </Pop>
  </div>

  <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 15px;">
    <SwitchLabel v-model="ephemeral" :label="$t('types.ts.ephemeral')" />
    <SwitchLabel v-model="acceptRoutes" :label="$t('types.ts.acceptRoutes')" />
  </div>

  <Field v-if="optionAuth" :label="$t('types.ts.authKey')">
    <input class="input mono" type="password" v-model="data.auth_key" placeholder="tskey-auth-…" />
  </Field>
  <Field v-if="optionCtrlUrl" :label="$t('types.ts.controlUrl')">
    <input class="input mono" v-model="data.control_url" />
  </Field>
  <div class="grid2">
    <Field v-if="optionHostname" :label="$t('types.ts.hostname')">
      <input class="input mono" v-model="data.hostname" />
    </Field>
    <Field v-if="optionStateDir" :label="$t('types.ts.stateDir')">
      <input class="input mono" v-model="data.state_directory" />
    </Field>
    <Field v-if="optionUdpTimeout" :label="$t('types.ts.udpTimeout') + ' (' + $t('date.s') + ')'">
      <input class="input mono" type="number" min="1" v-model.number="udpTimeout" />
    </Field>
  </div>

  <template v-if="optionExitNode">
    <Field :label="$t('types.ts.exitNode')">
      <input class="input mono" v-model="data.exit_node" />
    </Field>
    <div style="margin-bottom: 15px;">
      <SwitchLabel v-model="allowLanAccess" :label="$t('types.ts.allowLanAccess')" />
    </div>
  </template>

  <template v-if="optionRelay">
    <div class="grid2">
      <Field :label="$t('types.ts.relayServerPort')">
        <input class="input mono" type="number" min="0" v-model.number="data.relay_server_port" />
      </Field>
    </div>
    <Field :label="$t('types.ts.relayEndpoints') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="relayEndpoints" />
    </Field>
  </template>

  <template v-if="optionSysIf">
    <div style="margin-bottom: 15px;">
      <SwitchLabel v-model="systemInterface" :label="$t('types.ts.systemInterface')" />
    </div>
    <div class="grid2" v-if="data.system_interface">
      <Field :label="$t('types.ts.sysIfName')">
        <input class="input mono" v-model="data.system_interface_name" />
      </Field>
      <Field :label="$t('types.ts.sysIfMtu')">
        <input class="input mono" type="number" min="0" v-model.number="data.system_interface_mtu" />
      </Field>
    </div>
  </template>

  <template v-if="optionAdvRoutes">
    <Field :label="$t('types.ts.advRoutes') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="advertiseRoutes" />
    </Field>
    <Field :label="$t('types.ts.advTags') + ' ' + $t('commaSeparated')">
      <input class="input mono" placeholder="tag:server" v-model="advertiseTags" />
    </Field>
    <div style="margin-bottom: 15px;">
      <SwitchLabel v-model="advertiseExitNode" :label="$t('types.ts.advExitNode')" />
    </div>
  </template>

  <div class="grid2">
    <Field :label="$t('ui.listenPort')">
      <input class="input mono" type="number" min="0" max="65535" :value="data.listen_port ?? 0" @input="setListenPort" />
    </Field>
    <Field :label="$t('types.ts.taildrop')">
      <input class="input mono" placeholder="/var/lib/taildrop" v-model="taildropDirectory" />
    </Field>
  </div>

  <!-- Serving SSH over the tailnet. The three switches say what it will not
       offer, so they only mean anything once it is on. -->
  <template v-if="optionSsh">
    <div style="margin-bottom: 15px;">
      <SwitchLabel v-model="sshEnabled" :label="$t('types.ts.sshServer')" />
    </div>
    <div v-if="sshEnabled" style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="sshDisablePty" :label="$t('types.ts.sshDisablePty')" />
      <SwitchLabel v-model="sshDisableSftp" :label="$t('types.ts.sshDisableSftp')" />
      <SwitchLabel v-model="sshDisableForwarding" :label="$t('types.ts.sshDisableForwarding')" />
    </div>
  </template>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'

const props = defineProps<{ data: any }>()

const ephemeral = computed({
  get: () => props.data.ephemeral ?? false,
  set: (v: boolean) => { props.data.ephemeral = v },
})
const acceptRoutes = computed({
  get: () => props.data.accept_routes ?? false,
  set: (v: boolean) => { props.data.accept_routes = v },
})
const allowLanAccess = computed({
  get: () => props.data.exit_node_allow_lan_access ?? false,
  set: (v: boolean) => { props.data.exit_node_allow_lan_access = v },
})
const advertiseExitNode = computed({
  get: () => props.data.advertise_exit_node ?? false,
  set: (v: boolean) => { props.data.advertise_exit_node = v },
})
const systemInterface = computed({
  get: () => props.data.system_interface ?? false,
  set: (v: boolean) => { props.data.system_interface = v },
})

const optionStateDir = computed({
  get: () => props.data?.state_directory !== undefined,
  set: (v: boolean) => { props.data.state_directory = v ? '$HOME/.tailscale' : undefined },
})
const optionAuth = computed({
  get: () => props.data?.auth_key !== undefined,
  set: (v: boolean) => { props.data.auth_key = v ? '' : undefined },
})
const optionCtrlUrl = computed({
  get: () => props.data?.control_url !== undefined,
  set: (v: boolean) => { props.data.control_url = v ? 'https://controlplane.tailscale.com' : undefined },
})
const optionHostname = computed({
  get: () => props.data?.hostname !== undefined,
  set: (v: boolean) => { props.data.hostname = v ? 'localhost' : undefined },
})
const optionExitNode = computed({
  get: () => props.data?.exit_node !== undefined,
  set: (v: boolean) => {
    if (v) {
      props.data.exit_node = ''
    } else {
      delete props.data.exit_node
      delete props.data.exit_node_allow_lan_access
    }
  },
})
const optionAdvRoutes = computed({
  get: () => props.data?.advertise_routes !== undefined,
  set: (v: boolean) => {
    if (v) {
      props.data.advertise_routes = []
    } else {
      delete props.data.advertise_routes
      delete props.data.advertise_exit_node
    }
  },
})
const optionRelay = computed({
  get: () => props.data?.relay_server_port !== undefined || (props.data?.relay_server_static_endpoints?.length ?? 0) > 0,
  set: (v: boolean) => {
    if (v) {
      props.data.relay_server_port = 0
      props.data.relay_server_static_endpoints = []
    } else {
      delete props.data.relay_server_port
      delete props.data.relay_server_static_endpoints
    }
  },
})
const optionSysIf = computed({
  get: () => props.data?.system_interface !== undefined,
  set: (v: boolean) => {
    if (v) {
      props.data.system_interface = false
    } else {
      delete props.data.system_interface
      delete props.data.system_interface_name
      delete props.data.system_interface_mtu
    }
  },
})
const advertiseTags = computed({
  get: (): string => props.data.advertise_tags?.join(',') ?? '',
  set: (v: string) => {
    const parts = v.split(',').map((s) => s.trim()).filter((s) => s.length > 0)
    if (parts.length > 0) props.data.advertise_tags = parts
    else delete props.data.advertise_tags
  },
})

const taildropDirectory = computed({
  get: (): string => props.data.taildrop_directory ?? '',
  set: (v: string) => {
    const trimmed = (v ?? '').trim()
    if (trimmed) props.data.taildrop_directory = trimmed
    else delete props.data.taildrop_directory
  },
})

// uint16 in sing-box, and 0 is what "let it pick" is spelled as, so neither a
// fraction nor a zero leaves a key behind.
const setListenPort = (event: Event) => {
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isInteger(value) && value > 0 && value <= 65535) props.data.listen_port = value
  else delete props.data.listen_port
}

const optionSsh = computed({
  get: (): boolean => props.data.ssh_server != undefined,
  set: (v: boolean) => {
    if (v) props.data.ssh_server = { enabled: true }
    else delete props.data.ssh_server
  },
})
const sshFlag = (key: string) =>
  computed({
    get: (): boolean => props.data.ssh_server?.[key] === true,
    set: (v: boolean) => {
      if (v) props.data.ssh_server[key] = true
      else delete props.data.ssh_server[key]
    },
  })
const sshEnabled = sshFlag('enabled')
const sshDisablePty = sshFlag('disable_pty')
const sshDisableSftp = sshFlag('disable_sftp')
const sshDisableForwarding = sshFlag('disable_forwarding')

const optionUdpTimeout = computed({
  get: () => props.data?.udp_timeout !== undefined,
  set: (v: boolean) => { props.data.udp_timeout = v ? '30s' : undefined },
})
const udpTimeout = computed({
  get: () => props.data?.udp_timeout ? props.data.udp_timeout.replace('s', '') : '',
  set: (v: number) => { props.data.udp_timeout = v > 1 ? v + 's' : '30s' },
})
const advertiseRoutes = computed({
  get: () => props.data?.advertise_routes?.join(',') ?? '',
  set: (v: string) => { props.data.advertise_routes = v.length > 0 ? v.split(',') : [] },
})
const relayEndpoints = computed({
  get: () => props.data?.relay_server_static_endpoints?.join(',') ?? '',
  set: (v: string) => { props.data.relay_server_static_endpoints = v.length > 0 ? v.split(',') : [] },
})
</script>
