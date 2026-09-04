<template>
  <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
    <SectionLabel>Wireguard</SectionLabel>
    <div style="flex: 1;" />
    <Pop :min-width="190" direction="down">
      <template #trigger="{ toggle }">
        <Btn variant="subtle" sm @click="toggle">{{ $t('types.wg.options') }}</Btn>
      </template>
      <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
        <div class="pop-item"><SwitchLabel v-model="optionUdp" label="UDP Timeout" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionWorker" :label="$t('types.wg.worker')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionMtu" label="MTU" /></div>
      </div>
    </Pop>
  </div>

  <Field :label="$t('types.wg.privKey')">
    <KeyInput v-model="data.private_key" :title="$t('actions.generate')" @regenerate="$emit('newWgKey')" />
  </Field>
  <Field :label="$t('tls.pubKey')">
    <div style="display: flex; gap: 8px;">
      <input class="input mono" style="font-size: 12.5px;" readonly v-model="publicKey" />
      <button type="button" class="btn btn-ghost btn-icon" :title="$t('actions.generate')" @click="getWgPubKey">
        <Ico name="refresh" :size="15" />
      </button>
    </div>
  </Field>
  <Field :label="$t('types.wg.localIp') + ' ' + $t('commaSeparated')">
    <input class="input mono" v-model="address" />
  </Field>
  <div class="grid2">
    <Field :label="$t('ui.listenPort')">
      <input class="input mono" type="number" min="1" v-model.number="data.listen_port" />
    </Field>
    <Field v-if="data.udp_timeout != undefined" :label="'UDP Timeout (' + $t('date.m') + ')'">
      <input class="input mono" type="number" min="0" v-model.number="udpTimeout" />
    </Field>
    <Field v-if="data.workers != undefined" :label="$t('types.wg.worker')">
      <input class="input mono" type="number" min="1" v-model.number="data.workers" />
    </Field>
    <Field v-if="data.mtu != undefined" :label="$t('ui.mtu')">
      <input class="input mono" type="number" min="0" v-model.number="data.mtu" />
    </Field>
  </div>
  <Field :label="$t('dns.title') + ' ' + $t('commaSeparated')">
    <input class="input mono" v-model="extDns" />
  </Field>
  <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 15px;">
    <SwitchLabel v-model="system" :label="$t('types.wg.sysIf')" />
  </div>
  <Field v-if="data.system" :label="$t('types.wg.ifName')">
    <input class="input mono" v-model="ifName" />
  </Field>
  <UdpNat :data="data" />

  <template v-if="data.peers != undefined">
    <hr class="form-divider" />
    <div style="display: flex; align-items: center; margin-bottom: 12px;">
      <SectionLabel>{{ $t('types.wg.peers') }}</SectionLabel>
      <div style="flex: 1;" />
      <Btn variant="subtle" sm @click="$emit('addPeer')"><Ico name="plus" :size="14" /> {{ $t('ui.addPeer') }}</Btn>
    </div>
    <div
      v-for="(p, index) in data.peers"
      :key="index"
      class="card"
      style="padding: 14px; margin-bottom: 12px; background: var(--surface-2);"
    >
      <div style="display: flex; align-items: center; margin-bottom: 10px;">
        <Chip>{{ $t('types.wg.peer') + ' ' + (Number(index) + 1) }}</Chip>
        <div style="flex: 1;" />
        <Btn variant="subtle" icon sm :title="$t('actions.del')" @click="$emit('delPeer', Number(index))">
          <Ico name="trash" :size="14" />
        </Btn>
      </div>
      <Peer :data="p" :ext="data.ext" @refreshPeerKey="$emit('refreshPeerKey', Number(index))" />
    </div>
  </template>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import UdpNat from '../UdpNat.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import KeyInput from '@/components/ui/KeyInput.vue'
import Peer from '../WgPeer.vue'

const props = defineProps<{ data: any }>()
const emit = defineEmits<{
  newWgKey: []
  getWgPubKey: [privateKey: string]
  addPeer: []
  delPeer: [index: number]
  refreshPeerKey: [index: number]
}>()

const getWgPubKey = () => {
  const privKey = props.data.private_key
  if (privKey.length == 0) return
  emit('getWgPubKey', privKey)
}

const system = computed({
  get: () => props.data.system ?? false,
  set: (v: boolean) => { props.data.system = v },
})
const optionUdp = computed({
  get: (): boolean => props.data.udp_timeout != undefined,
  set: (v: boolean) => { props.data.udp_timeout = v ? '5m' : undefined },
})
const optionWorker = computed({
  get: (): boolean => props.data.workers != undefined,
  set: (v: boolean) => { props.data.workers = v ? 2 : undefined },
})
const optionMtu = computed({
  get: (): boolean => props.data.mtu != undefined,
  set: (v: boolean) => { props.data.mtu = v ? 1408 : undefined },
})
const ifName = computed({
  get: () => props.data.name ?? '',
  set: (v: string) => { props.data.name = v.length > 0 ? v : undefined },
})
const address = computed({
  get: () => props.data.address?.join(','),
  set: (v: string) => { props.data.address = v.length > 0 ? v.split(',') : undefined },
})
const udpTimeout = computed({
  get: () => props.data.udp_timeout ? parseInt(props.data.udp_timeout.replace('m', '')) : 5,
  set: (v: number) => { props.data.udp_timeout = v > 0 ? v + 'm' : '5m' },
})
const publicKey = computed({
  get: () => props.data.ext?.public_key ?? '',
  set: (v: string) => { (props.data.ext ??= {}).public_key = v },
})
const extDns = computed({
  get: () => props.data.ext?.dns ?? '',
  set: (v: string) => { (props.data.ext ??= {}).dns = v },
})
</script>
