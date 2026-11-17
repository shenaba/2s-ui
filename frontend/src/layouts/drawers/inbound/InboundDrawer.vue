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
          <Listen :data="inbound" :in-tags="inTags" :proxy-protocol="proxyProtocolOffered" />
          <MHint v-if="proxyWithoutAddrs" style="margin-bottom: 14px;">{{ $t('in.proxyProtocolNoAddrs', { port: inbound.listen_port }) }}</MHint>
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
        <Mieru v-if="inbound.type == inTypes.Mieru" direction="in" :data="inbound" />
        <Snell v-if="inbound.type == inTypes.Snell" direction="in" :data="inbound" />
        <Cloudflared v-if="inbound.type == inTypes.Cloudflared" :data="inbound" />
        <TProxy v-if="inbound.type == inTypes.TProxy" :inbound="inbound" />
        <!-- mieru's transport is its own TCP/UDP string, not the V2Ray transport
             object this form edits; switching that on would overwrite it. -->
        <Transport v-if="Object.hasOwn(inbound, 'transport') && inbound.type != inTypes.Mieru" :data="inbound" />
        <Users v-if="hasUser" :clients="clients" :data="initUsers" />
        <InTLS v-if="hasTls" :inbound="inbound" :tls-configs="tlsConfigs" />
        <Multiplex v-if="muxAvailable" direction="in" :data="inbound" />

        <!-- 入站自身的开关和流量上限 (#289)。抽屉只编辑本机入站,节点副本不进来 -->
        <hr class="form-divider" />
        <SectionLabel style="margin-bottom: 12px;">{{ $t('ui.limits') }}</SectionLabel>
        <div style="margin-bottom: 15px;">
          <SwitchLabel v-model="enabled" :label="$t('enable')" />
        </div>
        <div v-if="!isNew" class="card usage-card">
          <div style="display: flex; align-items: center; margin-bottom: 10px;">
            <div style="font-size: 12px; color: var(--text-3); font-weight: 600;">{{ $t('ui.currentUsage') }}</div>
            <Chip v-if="depleted" color="rose" style="margin-inline-start: 8px;">{{ $t('in.depleted') }}</Chip>
            <div v-if="percent > 0" class="mono" dir="ltr" style="margin-inline-start: auto; font-size: 12px; color: var(--text-3);">{{ percent }}%</div>
          </div>
          <div style="display: flex; gap: 22px; align-items: center;">
            <div>
              <div style="font-size: 11px; color: var(--violet); font-weight: 700;">↑ {{ $t('ui.upload') }}</div>
              <div class="mono" style="font-size: 17px; font-weight: 700;">{{ upText }}</div>
            </div>
            <div>
              <div style="font-size: 11px; color: var(--cyan); font-weight: 700;">↓ {{ $t('ui.download') }}</div>
              <div class="mono" style="font-size: 17px; font-weight: 700;">{{ downText }}</div>
            </div>
            <Btn sm variant="subtle" style="margin-inline-start: auto;" :disabled="resetting" @click="resetUsage">
              <Ico name="refresh" :size="14" /> {{ $t('reset') }}
            </Btn>
          </div>
          <div v-if="inbound.volume > 0" style="margin-top: 12px;">
            <BarMini :value="percent" :color="percentColor" :height="5" />
          </div>
        </div>
        <Field :label="$t('ui.dataLimit')" :hint="$t('in.trafficLimitHint')">
          <div style="display: flex; gap: 8px;">
            <input class="input mono" type="number" min="0" v-model.number="volumeGiB" />
            <div class="input suffix-box">{{ $t('stats.GB') }}</div>
          </div>
        </Field>
      </template>

      <!-- ===================== Client side ===================== -->
      <template v-else>
        <OutJson :in-data="inbound" :type="inbound.type" />
        <Multiplex v-if="Object.hasOwn(inbound, 'multiplex')" direction="out" :data="inbound.out_json" />
        <Dial v-if="inbound.out_json" :dial="inbound.out_json" mode="client" :detour-tags="detourTags" />
        <hr class="form-divider" />
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
          <SectionLabel style="flex: 1;">{{ $t('in.multiDomain') }}</SectionLabel>
          <Btn sm icon :title="$t('actions.add')" @click="add_addr"><Ico name="plus" :size="14" /></Btn>
        </div>
        <MHint v-if="proxyWithoutAddrs" style="margin-bottom: 12px;">{{ $t('in.proxyProtocolNoAddrs', { port: inbound.listen_port }) }}</MHint>
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
import { InTypes, ProxyProtocolTypes, InboundTypesWithUsers, createInbound, Addr, followListenPort } from '@/types/inbounds'
import { InitUsers, bindableClients, resolveInitUsers } from '@/types/clients'
import { checkHopInterval } from '@/types/hysteria2'
import { push } from 'notivue'
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
import MHint from '@/components/ui/MHint.vue'
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
import Mieru from '@/components/forms/in/Mieru.vue'
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
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import BarMini from '@/components/charts/BarMini.vue'
import { HumanReadable } from '@/plugins/utils'

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
  InTypes.Mieru,
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
  InTypes.Mieru,
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
// The listen port as loaded, so a save can carry the Multi Domain rows that
// sat on it to wherever the port was moved (#216).
const originalPort = ref<number | undefined>(undefined)
const initUsers = ref<InitUsers>({ model: 'none', values: [] })

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

const clients = computed((): any[] => bindableClients(Data().clients ?? []))

const addrsList = computed((): any[] => inbound.value.addrs ?? [])

// The QUIC transport serves only UDP, where there is no header to read; the
// save refuses it there (checkProxyProtocol).
const proxyProtocolOffered = computed((): boolean =>
  ProxyProtocolTypes.includes(inbound.value.type) && inbound.value.transport?.type != 'quic')

// Behind a front, the inbound's own port is not the one clients dial, and
// links are generated from it unless a Multi Domain row says otherwise (#260).
const proxyWithoutAddrs = computed((): boolean =>
  inbound.value.proxy_protocol == true && hasInData.value && addrsList.value.length == 0)

// What this client config can dial through: the ShadowTLS listeners, whose
// client half carries no proxy of its own and is only ever ridden (#255).
// Only v3 ones -- before v3 there is no client config to ride, and the save
// refuses it. Replicas are left out: they reach a subscription as links, never
// as an outbound a detour could name.
const detourTags = computed((): string[] => {
  if (inbound.value.type == InTypes.ShadowTLS) return []
  return (Data().inbounds ?? [])
    .filter((i: any) => i.type == InTypes.ShadowTLS && (i.version ?? 0) >= 3 &&
      i.node_id == null && i.tag != inbound.value.tag)
    .map((i: any) => i.tag)
})

const hasUser = computed((): boolean => {
  if (props.id > 0) return false
  if (!InboundTypesWithUsers.includes(inbound.value.type)) return false
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
  originalPort.value = inbound.value.listen_port
  stored.value = { enable: inbound.value.enable ?? true, volume: inbound.value.volume ?? 0 }
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
    originalPort.value = undefined
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
  // The switch and the cap are the inbound's, not the protocol's.
  const keep = {
    id: inbound.value.id,
    tag: tag,
    enable: inbound.value.enable,
    volume: inbound.value.volume,
    up: inbound.value.up,
    down: inbound.value.down,
  }
  const prevConfig = noListen
    ? keep
    : {
        ...keep,
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

// ---------- limits (#289) ----------
// A row from before the columns, or a fresh form, has neither field: enabled,
// no cap. The counters come with the loaded row and are the server's -- an
// edit never writes them back.
const enabled = computed({
  get: () => inbound.value.enable ?? true,
  set: (v: boolean) => { inbound.value.enable = v },
})
const volumeGiB = computed({
  // Stored in whole bytes, so 0.0001 GB reads back as 0.0000999998...; six
  // decimals give back what was typed. A cap too small to show in six stays
  // raw rather than reading as 0, which would say "unlimited".
  get: () => {
    if (!(inbound.value.volume > 0)) return 0
    const gb = inbound.value.volume / (1024 ** 3)
    const shown = Math.round(gb * 1e6) / 1e6
    return shown > 0 ? shown : gb
  },
  set: (v: number | string) => {
    const n = Number(v)
    inbound.value.volume = n > 0 ? Math.round(n * (1024 ** 3)) : 0
  },
})
const used = computed((): number => (inbound.value.up ?? 0) + (inbound.value.down ?? 0))
const upText = computed(() => HumanReadable.sizeFormat(inbound.value.up ?? 0))
const downText = computed(() => HumanReadable.sizeFormat(inbound.value.down ?? 0))
// Same comparison as the server's (up+down > volume).
const depleted = computed((): boolean => inbound.value.volume > 0 && used.value > inbound.value.volume)
const percent = computed((): number =>
  inbound.value.volume > 0 ? Math.min(100, Math.round((used.value * 100) / inbound.value.volume)) : 0)
const percentColor = computed((): string => {
  if (depleted.value) return 'var(--rose)'
  return percent.value > 90 ? 'var(--amber)' : 'var(--brand)'
})
// Applied at once, not with the form: the counters are the server's and a save
// never carries them. Mirrored here rather than reloaded, which would throw away
// whatever else is being edited -- the reset switches back on only an inbound
// the cap had switched off, and so does this. Judged on the row as loaded, as
// the server judges it, not on a switch or cap edited in the form and unsaved.
const stored = ref({ enable: true, volume: 0 })
const resetting = ref(false)
const resetUsage = async () => {
  if (resetting.value || isNew.value) return
  resetting.value = true
  try {
    const wasSpent = !stored.value.enable && stored.value.volume > 0 && used.value > stored.value.volume
    const success = await Data().save('inbounds', 'resetTraffic', originalTag.value)
    if (success) {
      inbound.value.up = 0
      inbound.value.down = 0
      if (wasSpent) {
        stored.value.enable = true
        inbound.value.enable = true
      }
    }
  } finally {
    resetting.value = false
  }
}

const add_addr = () => {
  inbound.value.addrs?.push(<Addr>{ server: location.hostname, server_port: inbound.value.listen_port })
}

const saveChanges = async () => {
  if (!props.visible || loading.value) return
  // check duplicate tag
  const isDuplicatedTag = Data().checkTag('inbound', inbound.value.id, inbound.value.tag)
  if (isDuplicatedTag) return
  // the port hopping fields live in the generated client config
  const hopError = checkHopInterval(inbound.value.out_json)
  if (hopError) {
    push.error({ message: t(hopError) })
    return
  }

  // save data
  loading.value = true
  const clientIds = hasUser.value ? resolveInitUsers(clients.value, initUsers.value) : []
  // Remapped on the payload, not on the form: a save that fails leaves the
  // rows as shown, still keyed to originalPort for the next attempt.
  const data = { ...inbound.value, addrs: followListenPort(inbound.value.addrs, originalPort.value, inbound.value.listen_port) }
  const success = await Data().save('inbounds', props.id == 0 ? 'new' : 'edit', data, clientIds)
  if (success) emit('close')
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>

<style scoped>
.usage-card {
  padding: 16px;
  margin-bottom: 18px;
  background: var(--surface-3);
  border: none;
  box-shadow: none;
}
.suffix-box {
  width: 80px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-2);
}
</style>
