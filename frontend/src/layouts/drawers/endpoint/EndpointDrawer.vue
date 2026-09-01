<template>
  <MDrawer
    :open="visible"
    icon="endpoint"
    :color="endpoint.type === 'tailscale' ? 'var(--violet)' : 'var(--cyan)'"
    :title="isNew ? $t('ui.epNew') : endpoint.tag"
    :sub="isNew ? $t('ui.epSub') : endpoint.type"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <!-- type: switchable only while adding (legacy select was disabled on edit) -->
    <div v-if="isNew" style="margin-bottom: 15px;">
      <Segmented v-model="endpointType" block :options="typeOptions" />
    </div>
    <Field v-else :label="$t('type')">
      <Select v-model="endpoint.type" disabled>
        <option v-for="(value, key) in epTypes" :key="value" :value="value">{{ key }}</option>
      </Select>
    </Field>

    <Field :label="$t('objects.tag')">
      <input class="input mono" v-model="endpoint.tag" />
    </Field>

    <Wireguard
      v-if="endpoint.type == epTypes.Wireguard"
      :data="endpoint"
      @getWgPubKey="getWgPubKey"
      @newWgKey="newWgKey"
      @addPeer="addWgPeer"
      @delPeer="delWgPeer"
      @refreshPeerKey="refreshWgPeerKey"
    />
    <Warp v-if="endpoint.type == epTypes.Warp" :data="endpoint" />
    <TailscaleVue v-if="endpoint.type == epTypes.Tailscale" :data="endpoint" />
    <EndpointTls v-if="HasTls.includes(endpoint.type)" :endpoint="endpoint" :tls-configs="tlsConfigs" />
    <OpenConnect v-if="endpoint.type == epTypes.OpenConnect" :data="endpoint" />
    <OpenVpn v-if="isOpenVpn" :data="endpoint" />
    <Dial v-if="!NoDial.includes(endpoint.type)" :dial="endpoint" />
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import RandomUtil from '@/plugins/randomUtil'
import { EpTypes, Endpoint, createEndpoint } from '@/types/endpoints'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Segmented from '@/components/ui/Segmented.vue'
import Dial from '@/components/forms/out/Dial.vue'
import Wireguard from '@/components/forms/out/protocols/Wireguard.vue'
import Warp from '@/components/forms/out/protocols/Warp.vue'
import TailscaleVue from '@/components/forms/out/protocols/Tailscale.vue'
import OpenConnect from '@/components/forms/out/protocols/OpenConnect.vue'
import OpenVpn from '@/components/forms/out/protocols/OpenVpn.vue'
import EndpointTls from '@/components/forms/out/tls/EndpointTls.vue'

const props = defineProps<{
  visible: boolean
  id: number
  data: string
  tags?: string[]
  tlsConfigs?: any[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const epTypes = EpTypes
// Endpoints that terminate or present TLS can reuse a panel TLS config.
const HasTls = [EpTypes.OpenConnect, EpTypes.OpenVPNClient, EpTypes.OpenVPNServer]
// openvpn-server listens rather than dials, so it takes no dialer options.
const NoDial = [EpTypes.OpenVPNServer]

const isOpenVpn = computed((): boolean =>
  [EpTypes.OpenVPNClient, EpTypes.OpenVPNServer].includes(endpoint.value.type))
const endpoint = ref<Endpoint>(createEndpoint('wireguard', { tag: '' }))
const loading = ref(false)

const isNew = computed(() => props.id === 0)
const typeOptions = computed((): [string | number, string][] =>
  Object.entries(EpTypes).map(([key, value]): [string | number, string] => [value, key]))

const updateData = async (id: number) => {
  if (id > 0) {
    const newData = JSON.parse(props.data)
    endpoint.value = createEndpoint(newData.type, newData)
  } else {
    endpoint.value.type = 'wireguard'
    endpoint.value.listen_port = RandomUtil.randomIntRange(10000, 60000)
    await changeType()
  }
}

const changeType = async () => {
  // Tag change only in add endpoint
  const tag = endpoint.value.type + '-' + RandomUtil.randomSeq(3)

  // Use previous data. The TLS reference is kept only when the type being
  // switched to can still use one, so it does not linger as a dead column.
  const keepTls = HasTls.includes(endpoint.value.type) ? (<any>endpoint.value).tls_id : undefined
  let prevConfig = {}
  switch (endpoint.value.type) {
    case EpTypes.Wireguard: {
      const wgKeys = await genWgKey()
      const randomIPoctet = RandomUtil.randomIntRange(1, 255)
      prevConfig = {
        tag: tag,
        listen_port: endpoint.value.listen_port ?? RandomUtil.randomIntRange(10000, 60000),
        address: ['10.0.0.' + randomIPoctet.toString() + '/32', 'fe80::' + randomIPoctet.toString(16) + '/128'],
        private_key: wgKeys.private_key,
        peers: [],
        ext: {
          public_key: wgKeys.public_key,
          keys: [],
        },
      }
      break
    }
    case EpTypes.Warp:
      prevConfig = { tag: tag }
      break
    case EpTypes.Tailscale:
      prevConfig = { tag: tag }
      break
    case EpTypes.OpenConnect:
    case EpTypes.OpenVPNClient:
      prevConfig = { tag: tag }
      break
    case EpTypes.OpenVPNServer:
      prevConfig = {
        tag: tag,
        listen: '::',
        listen_port: (<any>endpoint.value).listen_port ?? RandomUtil.randomIntRange(10000, 60000),
      }
      break
  }
  endpoint.value = createEndpoint(endpoint.value.type, prevConfig)
  // Carry the selected TLS config across only while the new type can use one.
  if (keepTls != undefined) (<any>endpoint.value).tls_id = keepTls
}

const endpointType = computed({
  get: () => endpoint.value.type,
  set: (v: string | number) => {
    endpoint.value.type = String(v)
    changeType()
  },
})

const saveChanges = async () => {
  if (!props.visible) return

  // check duplicate tag
  const isDuplicatedTag = Data().checkTag('endpoint', endpoint.value.id, endpoint.value.tag)
  if (isDuplicatedTag) return

  // save data
  loading.value = true
  const success = await Data().save('endpoints', props.id == 0 ? 'new' : 'edit', endpoint.value)
  if (success) emit('close')
  loading.value = false
}

const genWgKey = async (): Promise<{ private_key: string; public_key: string }> => {
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'wireguard' })
  loading.value = false
  const result = { private_key: '', public_key: '' }
  if (msg.success) {
    msg.obj.forEach((line: string) => {
      if (line.startsWith('PrivateKey')) {
        result.private_key = line.substring(12)
      }
      if (line.startsWith('PublicKey')) {
        result.public_key = line.substring(11)
      }
    })
  } else {
    push.error({
      message: t('failed') + ': ' + msg.obj,
    })
  }
  return result
}

const newWgKey = async () => {
  loading.value = true
  const newKeys = await genWgKey()
  endpoint.value.private_key = newKeys.private_key
  if (!endpoint.value.ext) endpoint.value.ext = { keys: [] }
  endpoint.value.ext.public_key = newKeys.public_key
  loading.value = false
}

const getWgPubKey = async (private_key: string) => {
  if (!endpoint.value.ext) endpoint.value.ext = { keys: [] }
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'wireguard', o: private_key })
  if (msg.success) {
    endpoint.value.ext.public_key = msg.obj[0]
  }
  loading.value = false
}

const addWgPeer = async () => {
  if (endpoint.value.type != EpTypes.Wireguard) return
  loading.value = true
  const newKeys = await genWgKey()
  if (!endpoint.value.ext) endpoint.value.ext = { keys: [] }
  endpoint.value.ext.keys.push(newKeys)
  endpoint.value.peers.push({
    public_key: newKeys.public_key,
    allowed_ips: [findFreeIP()],
  })
  loading.value = false
}

const findFreeIP = (): string => {
  const peerAllowedIPs = endpoint.value.peers.map((peer: any) => peer.allowed_ips).flat()
  for (let i = 2; i < 255; i++) {
    const newIP = '10.0.1.' + i.toString() + '/32'
    if (!peerAllowedIPs.includes(newIP)) return newIP
  }
  return '0.0.0.0/0'
}

const delWgPeer = (index: number) => {
  if (endpoint.value.type != EpTypes.Wireguard) return
  endpoint.value.ext.keys = endpoint.value.ext.keys.filter(
    (key: any) => key.public_key != endpoint.value.peers[index].public_key)
  endpoint.value.peers.splice(index, 1)
}

const refreshWgPeerKey = async (index: number) => {
  loading.value = true
  const newKeys = await genWgKey()
  if (!endpoint.value.ext) endpoint.value.ext = { keys: [] }
  const indexKeys = endpoint.value.ext.keys.findIndex(
    (key: any) => key.public_key == endpoint.value.peers[index].public_key)
  endpoint.value.ext.keys[indexKeys == -1 ? endpoint.value.ext.keys.length : indexKeys] = newKeys
  endpoint.value.peers[index].public_key = newKeys.public_key
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>
