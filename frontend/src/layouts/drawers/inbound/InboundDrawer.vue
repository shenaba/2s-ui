<template>
  <MDrawer
    :open="visible"
    :width="500"
    icon="inbound"
    :color="protoColor(inbound.type)"
    :title="isNew ? $t('ui.newInbound') : originalTag"
    :sub="subTitle"
    :save-label="isNew ? $t('ui.createReload') : $t('ui.saveReload')"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <div v-if="loading" style="padding: 40px 0; text-align: center; color: var(--text-3); font-size: 13px;">
      {{ $t('loading') }}
    </div>
    <template v-else>
      <Field :label="$t('ui.protocolType')">
        <Select
          class="mono"
          :style="{ color: protoColor(inbound.type), fontWeight: 700 }"
          v-model="inbound.type"
          @change="changeType"
        >
          <optgroup :label="$t('ui.protoGroupProxy')">
            <option v-for="p in proxyTypes" :key="p" :value="p">{{ p }}</option>
          </optgroup>
          <optgroup :label="$t('ui.protoGroupLocal')">
            <option v-for="p in localTypes" :key="p" :value="p">{{ p }}</option>
          </optgroup>
        </Select>
      </Field>
      <Field :label="$t('objects.tag')">
        <input class="input mono" required v-model="inbound.tag" />
      </Field>

      <Segmented
        v-if="hasInData"
        v-model="side"
        :options="sideOptions"
        block
        style="margin: 4px 0 18px;"
      />

      <!-- ===================== Server side ===================== -->
      <template v-if="side === 's'">
        <!-- tun brings its own interface options; cloudflared dials out to the
             Cloudflare edge and sing-box rejects listen fields on it. -->
        <template v-if="!NoListen.includes(inbound.type)">
          <Listen :data="inbound" :in-tags="inTags" />
          <hr class="form-divider" />
        </template>
        <Direct v-if="inbound.type == inTypes.Direct" :data="inbound" />
        <Shadowsocks v-if="inbound.type == inTypes.Shadowsocks" :data="inbound" />
        <Hysteria v-if="inbound.type == inTypes.Hysteria" :data="inbound" />
        <Hysteria2 v-if="inbound.type == inTypes.Hysteria2" :data="inbound" />
        <Naive v-if="inbound.type == inTypes.Naive" direction="in" :data="inbound" />
        <ShadowTls v-if="inbound.type == inTypes.ShadowTLS" :data="inbound" />
        <Tuic v-if="inbound.type == inTypes.TUIC" direction="in" :data="inbound" />
        <Tun v-if="inbound.type == inTypes.Tun" :data="inbound" />
        <AnyTls v-if="inbound.type == inTypes.AnyTls" direction="in" :data="inbound" />
        <Snell v-if="inbound.type == inTypes.Snell" direction="in" :data="inbound" />
        <Cloudflared v-if="inbound.type == inTypes.Cloudflared" :data="inbound" />
        <TProxy v-if="inbound.type == inTypes.TProxy" :inbound="inbound" />
        <Transport v-if="Object.hasOwn(inbound, 'transport')" :data="inbound" />
        <Users v-if="hasUser" :clients="clients" :data="initUsers" />
        <InTLS v-if="hasTls" :inbound="inbound" :tls-configs="tlsConfigs" />
        <Multiplex v-if="muxAvailable" direction="in" :data="inbound" />
      </template>

      <!-- ===================== Client side ===================== -->
      <template v-else>
        <OutJson :in-data="inbound" :type="inbound.type" />
        <Multiplex v-if="Object.hasOwn(inbound, 'multiplex')" direction="out" :data="inbound.out_json" />
        <Dial v-if="inbound.out_json" :dial="inbound.out_json" mode="client" />
        <hr class="form-divider" />
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
          <SectionLabel style="flex: 1;">{{ $t('in.multiDomain') }}</SectionLabel>
          <Btn sm icon :title="$t('actions.add')" @click="add_addr"><Ico name="plus" :size="14" /></Btn>
        </div>
        <div
          v-for="(addr, index) in addrsList"
          :key="index"
          class="card"
          style="padding: 14px; margin-bottom: 12px; background: var(--surface-2);"
        >
          <div style="display: flex; align-items: center; margin-bottom: 10px;">
            <Chip><span class="mono">{{ $t('in.addr') }} #{{ index + 1 }}</span></Chip>
            <IconBtn
              name="trash"
              danger
              style="margin-inline-start: auto;"
              :title="$t('actions.del')"
              @click="inbound.addrs?.splice(index, 1)"
            />
          </div>
          <AddrVue :addr="addr" :has-tls="hasTls" />
        </div>
      </template>
    </template>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import { InTypes, createInbound, Addr } from '@/types/inbounds'
import RandomUtil from '@/plugins/randomUtil'
import { protoColor } from '@/plugins/colors'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Segmented from '@/components/ui/Segmented.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import Chip from '@/components/ui/Chip.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import IconBtn from '@/components/ui/IconBtn.vue'
import Listen from '@/components/forms/out/Listen.vue'
import Direct from '@/components/forms/in/Direct.vue'
import Shadowsocks from '@/components/forms/in/Shadowsocks.vue'
import Hysteria from '@/components/forms/in/Hysteria.vue'
import Hysteria2 from '@/components/forms/in/Hysteria2.vue'
import Naive from '@/components/forms/in/Naive.vue'
import ShadowTls from '@/components/forms/in/ShadowTls.vue'
import Tuic from '@/components/forms/in/Tuic.vue'
import Tun from '@/components/forms/in/Tun.vue'
import AnyTls from '@/components/forms/in/AnyTls.vue'
import Cloudflared from '@/components/forms/in/Cloudflared.vue'
import Snell from '@/components/forms/out/protocols/Snell.vue'
import TProxy from '@/components/forms/in/TProxy.vue'
import Transport from '@/components/forms/out/Transport.vue'
import Users from '@/components/forms/in/Users.vue'
import InTLS from '@/components/forms/in/InTLS.vue'
import Multiplex from '@/components/forms/out/Multiplex.vue'
import OutJson from '@/components/forms/in/OutJson.vue'
import Dial from '@/components/forms/out/Dial.vue'
import AddrVue from '@/components/forms/in/Addr.vue'

const props = defineProps<{
  visible: boolean
  id: number
  inTags: string[]
  tlsConfigs: any[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const inTypes = InTypes

// protocol select groups (design extra.jsx) — together they cover the full InTypes set
const proxyTypes = [
  InTypes.VLESS,
  InTypes.VMess,
  InTypes.Trojan,
  InTypes.Shadowsocks,
  InTypes.ShadowTLS,
  InTypes.Hysteria2,
  InTypes.Hysteria,
  InTypes.TUIC,
  InTypes.Naive,
  InTypes.AnyTls,
  InTypes.Snell,
]
const localTypes = [
  InTypes.Mixed,
  InTypes.SOCKS,
  InTypes.HTTP,
  InTypes.Direct,
  InTypes.Tun,
  InTypes.Redirect,
  InTypes.TProxy,
  InTypes.Cloudflared,
]

// same capability sets as the legacy modal (layouts/modals/Inbound.vue)
const inboundWithUsers = [
  'mixed', 'socks', 'http', 'shadowsocks', 'snell', 'vmess', 'trojan', 'naive',
  'hysteria', 'shadowtls', 'tuic', 'hysteria2', 'vless', 'anytls',
]
const HasInData = [
  InTypes.SOCKS,
  InTypes.HTTP,
  InTypes.Mixed,
  InTypes.Shadowsocks,
  InTypes.Snell,
  InTypes.VMess,
  InTypes.ShadowTLS,
  InTypes.Trojan,
  InTypes.Hysteria,
  InTypes.VLESS,
  InTypes.AnyTls,
  InTypes.TUIC,
  InTypes.Hysteria2,
  InTypes.Naive,
]
const HasTls = [
  InTypes.HTTP,
  InTypes.VMess,
  InTypes.Trojan,
  InTypes.Naive,
  InTypes.Hysteria,
  InTypes.TUIC,
  InTypes.Hysteria2,
  InTypes.VLESS,
  InTypes.AnyTls,
]
const MuxAvailable = [
  InTypes.VLESS,
  InTypes.VMess,
  InTypes.Trojan,
  InTypes.Shadowsocks,
]
const NoListen = [InTypes.Tun, InTypes.Cloudflared]

const inbound = ref<any>(createInbound('direct', { id: 0, tag: '' }))
const loading = ref(false)
const side = ref<string | number>('s')
const originalTag = ref('')
const initUsers = ref<{ model: string; values: any[] }>({ model: 'none', values: [] })

const isNew = computed(() => props.id == 0)
const hasInData = computed(() => HasInData.includes(inbound.value.type))
const hasTls = computed(() => HasTls.includes(inbound.value.type))
const muxAvailable = computed(() => MuxAvailable.includes(inbound.value.type))

const sideOptions = computed((): [string | number, string][] => [
  ['s', t('ui.serverSide')],
  ['c', t('ui.clientSide')],
])

const subTitle = computed(() => {
  if (isNew.value) return t('ui.configureListener')
  const port = inbound.value.listen_port
  return inbound.value.type + (port ? ' · :' + port : '')
})

const clients = computed((): any[] => Data().clients ?? [])

const addrsList = computed((): any[] => inbound.value.addrs ?? [])

const hasUser = computed((): boolean => {
  if (props.id > 0) return false
  if (!inboundWithUsers.includes(inbound.value.type)) return false
  if (inbound.value.type == InTypes.ShadowTLS && inbound.value.version < 3) return false
  if (inbound.value.managed) return false
  return true
})

const loadData = async (id: number) => {
  loading.value = true
  const inboundArray = await Data().loadInbounds([id])
  inbound.value = inboundArray[0]
  if (HasInData.includes(inbound.value.type) && inbound.value.out_json == null) {
    inbound.value.out_json = {}
  }
  originalTag.value = inbound.value.tag
  loading.value = false
}

const updateData = (id: number) => {
  if (id > 0) {
    loadData(id)
  } else {
    const port = RandomUtil.randomIntRange(10000, 60000)
    inbound.value = createInbound('direct', { id: 0, tag: 'direct-' + port, listen: '::', listen_port: port })
    if (HasInData.includes(inbound.value.type)) {
      inbound.value.addrs = []
      inbound.value.out_json = {}
    } else {
      delete inbound.value.addrs
      delete inbound.value.out_json
    }
    originalTag.value = ''
    loading.value = false
  }
  side.value = 's'
  initUsers.value = { model: 'none', values: [] }
}

const changeType = () => {
  if (!inbound.value.listen_port) inbound.value.listen_port = RandomUtil.randomIntRange(10000, 60000)
  // Same list the Listen section is gated on: a type that does not listen must
  // not carry the listen settings over either, or sing-box rejects them as
  // unknown fields on a form that no longer shows them.
  const noListen = NoListen.includes(inbound.value.type)
  // Tag change only in add inbound. Without a port there is nothing to make the
  // tag unique, so fall back to a random suffix.
  const tag = props.id > 0
    ? inbound.value.tag
    : inbound.value.type + '-' + (noListen ? RandomUtil.randomSeq(3) : inbound.value.listen_port)
  // Use previous data. `id` is kept in both branches: dropping it turns the
  // save into a create and leaves the original row behind.
  const prevConfig = noListen
    ? { id: inbound.value.id, tag: tag }
    : {
        id: inbound.value.id,
        tag: tag,
        listen: inbound.value.listen ?? '::',
        listen_port: inbound.value.listen_port,
      }
  inbound.value = createInbound(inbound.value.type, prevConfig)
  if (HasInData.includes(inbound.value.type)) {
    inbound.value.addrs = []
    inbound.value.out_json = {}
  } else {
    delete inbound.value.addrs
    delete inbound.value.out_json
  }
  side.value = 's'
}

const add_addr = () => {
  inbound.value.addrs?.push(<Addr>{ server: location.hostname, server_port: inbound.value.listen_port })
}

const saveChanges = async () => {
  if (!props.visible || loading.value) return
  // check duplicate tag
  const isDuplicatedTag = Data().checkTag('inbound', inbound.value.id, inbound.value.tag)
  if (isDuplicatedTag) return

  // save data
  loading.value = true
  let clientIds: number[] = []
  if (hasUser.value) {
    switch (initUsers.value.model) {
      case 'all':
        clientIds = clients.value.map((c: any) => c.id)
        break
      case 'group':
        clientIds = clients.value
          .filter((c: any) => initUsers.value.values.includes(c.group))
          .map((c: any) => c.id)
        break
      case 'client':
        clientIds = initUsers.value.values
    }
  }
  const success = await Data().save('inbounds', props.id == 0 ? 'new' : 'edit', inbound.value, clientIds)
  if (success) emit('close')
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>
