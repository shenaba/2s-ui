<template>
  <MDrawer
    :open="open"
    icon="dns"
    :color="dnsColor(server.type)"
    :title="isNew ? $t('ui.dnssNew') : server.tag"
    :sub="$t('ui.dnssSub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    @close="$emit('close')"
    @save="$emit('save', server)"
  >
    <div class="grid2">
      <Field :label="$t('type')">
        <Select v-model="serverType">
          <option v-for="dt in dnsTypes" :key="dt.value" :value="dt.value">{{ dt.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('objects.tag')">
        <input class="input mono" v-model="server.tag" />
      </Field>
    </div>

    <!-- server address / port -->
    <div class="grid2" v-if="HasServer.includes(server.type)">
      <Field :label="$t('in.addr')">
        <input class="input mono" v-model="server.server" placeholder="1.1.1.1" />
      </Field>
      <Field :label="$t('in.port')">
        <input class="input mono" type="number" min="0" v-model.number="server.server_port" />
      </Field>
    </div>

    <!-- https / h3 path -->
    <Field v-if="HasHeaders.includes(server.type)" :label="$t('transport.path')">
      <input class="input mono" v-model="server.path" />
    </Field>

    <!-- local -->
    <div v-if="server.type === 'local'" style="margin-bottom: 15px;">
      <SwitchLabel :label="$t('dns.local.preferGo')" :model-value="!!server.prefer_go" @update:model-value="server.prefer_go = $event" />
      <!-- Single-label hosts answered from neighbour resolution. Setting it is
           also what turns that resolution on, the way a source_mac_address rule
           does. -->
      <Field :label="$t('dns.local.neighborDomain') + ' ' + $t('commaSeparated')" style="margin-top: 12px;">
        <input class="input mono" v-model="neighborDomain" placeholder="lan" />
      </Field>
    </div>

    <!-- dhcp -->
    <Field v-if="server.type === 'dhcp'" :label="$t('types.tun.ifName')">
      <input class="input mono" v-model="server.interface" placeholder="auto" />
    </Field>

    <!-- fakeip -->
    <div class="grid2" v-if="server.type === 'fakeip'">
      <Field :label="$t('dns.rule.inet4Range')">
        <input class="input mono" v-model="server.inet4_range" />
      </Field>
      <Field :label="$t('dns.rule.inet6Range')">
        <input class="input mono" v-model="server.inet6_range" />
      </Field>
    </div>

    <!-- hosts -->
    <template v-if="server.type === 'hosts'">
      <Field :label="$t('transport.path') + ' ' + $t('commaSeparated')">
        <input class="input mono" :value="hostsPath" @change="hostsPath = ($event.target as HTMLInputElement).value" />
      </Field>
      <div style="display: flex; align-items: center; margin-bottom: 10px;">
        <SectionLabel>Predefined</SectionLabel>
        <div style="flex: 1;" />
        <IconBtn name="plus" :title="$t('actions.add')" @click="addHostsPredefined" />
      </div>
      <div
        v-for="(pd, index) in hostsPredefined"
        :key="index"
        style="display: grid; grid-template-columns: 1fr 1fr 34px; gap: 8px; align-items: center; margin-bottom: 8px;"
      >
        <input class="input mono" style="height: 38px; font-size: 12.5px;" :value="pd.name" :placeholder="$t('setting.domain')" @change="updatePdsKey(index, ($event.target as HTMLInputElement).value)" />
        <input class="input mono" style="height: 38px; font-size: 12.5px;" :value="pd.value" :placeholder="$t('types.tun.addr') + ' ' + $t('commaSeparated')" @change="updatePdsValue(index, ($event.target as HTMLInputElement).value)" />
        <button type="button" class="btn btn-subtle btn-icon" style="height: 38px; width: 34px;" @click="delHostsPredefined(index)">
          <Ico name="close" :size="14" />
        </button>
      </div>
    </template>

    <!-- tailscale / resolved -->
    <template v-if="server.type === 'tailscale' || server.type === 'resolved'">
      <Field v-if="server.type === 'tailscale'" :label="$t('objects.endpoint')">
        <Select v-model="server.endpoint">
          <option v-for="e in tsTags" :key="e" :value="e">{{ e }}</option>
        </Select>
      </Field>
      <Field v-if="server.type === 'resolved'" :label="$t('objects.service')">
        <Select v-model="server.service">
          <option v-for="s in rslvdTags" :key="s" :value="s">{{ s }}</option>
        </Select>
      </Field>
      <MSwitchRow :label="$t('dns.rule.acceptDefault')" :model-value="!!server.accept_default_resolvers" @update:model-value="server.accept_default_resolvers = $event" />
      <MSwitchRow v-if="server.type !== 'resolved'" :label="$t('dns.acceptSearchDomain')" :model-value="!!server.accept_search_domain" @update:model-value="server.accept_search_domain = $event" />
    </template>

    <!-- openconnect / openvpn: the same three fields tailscale has, over the
         resolvers the VPN server pushed to that endpoint. -->
    <template v-if="['openconnect', 'openvpn'].includes(server.type)">
      <Field :label="$t('objects.endpoint')">
        <Select v-model="server.endpoint">
          <option v-for="e in ocvTags" :key="e" :value="e">{{ e }}</option>
        </Select>
      </Field>
      <MSwitchRow :label="$t('dns.rule.acceptDefault')" :model-value="!!server.accept_default_resolvers" @update:model-value="server.accept_default_resolvers = $event" />
      <MSwitchRow :label="$t('dns.acceptSearchDomain')" :model-value="!!server.accept_search_domain" @update:model-value="server.accept_search_domain = $event" />
    </template>

    <!-- ===================== Dial ===================== -->
    <template v-if="!WithoutDial.includes(server.type)">
      <hr class="form-divider" />
      <Dial :dial="server" />
    </template>

    <!-- ===================== TLS ===================== -->
    <template v-if="HasTls.includes(server.type)">
      <hr class="form-divider" />
      <OutTLS :outbound="server" />
    </template>

    <!-- ===================== Headers (https / h3) ===================== -->
    <template v-if="HasHeaders.includes(server.type)">
      <hr class="form-divider" />
      <Headers :data="server" />
    </template>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Ico from '@/components/ui/Ico.vue'
import IconBtn from '@/components/ui/IconBtn.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MSwitchRow from '@/components/ui/MSwitchRow.vue'
import Headers from '@/components/forms/out/Headers.vue'
import Dial from '@/components/forms/out/Dial.vue'
import OutTLS from '@/components/forms/out/OutTLS.vue'
import RandomUtil from '@/plugins/randomUtil'
import { dnsColor } from '@/plugins/colors'
import { DnsTypes, createDnsServer, DnsServer } from '@/types/dns'

const props = defineProps<{
  open: boolean
  index: number
  data: string
  tsTags: string[]
  rslvdTags: string[]
  ocvTags: string[]
}>()
defineEmits<{ close: []; save: [data: any] }>()

const isNew = computed(() => props.index === -1)

const dnsTypes = Object.keys(DnsTypes).map((key, index) => ({ title: key, value: Object.values(DnsTypes)[index] }))
const HasServer: string[] = [DnsTypes.TCP, DnsTypes.UDP, DnsTypes.TLS, DnsTypes.QUIC, DnsTypes.HTTPS, DnsTypes.HTTP3]
const HasHeaders: string[] = [DnsTypes.HTTPS, DnsTypes.HTTP3]
const HasTls: string[] = [DnsTypes.TLS, DnsTypes.QUIC, DnsTypes.HTTPS, DnsTypes.HTTP3]
// Neither dials out itself: both read what their endpoint was handed.
const WithoutDial: string[] = [
  DnsTypes.Hosts, DnsTypes.Tailscale, DnsTypes.FakeIP, DnsTypes.Resolved,
  DnsTypes.OpenConnect, DnsTypes.OpenVPN,
]

const server = ref<any>(createDnsServer('local', { tag: 'dns-' + RandomUtil.randomSeq(3) }))

const neighborDomain = computed({
  get: (): string => server.value.neighbor_domain?.join(',') ?? '',
  set: (v: string) => {
    const parts = v.split(',').map((s: string) => s.trim()).filter((s: string) => s.length > 0)
    if (parts.length > 0) server.value.neighbor_domain = parts
    else delete server.value.neighbor_domain
  },
})

function init() {
  if (props.index !== -1) {
    server.value = JSON.parse(props.data)
  } else {
    server.value = createDnsServer('local', { tag: 'dns-' + RandomUtil.randomSeq(3) })
  }
}
watch(() => props.open, (v) => { if (v) init() })

// changing the type rebuilds the server with that type's defaults (legacy changeType)
const serverType = computed({
  get: () => server.value.type,
  set: (t: string) => {
    server.value = <DnsServer>createDnsServer(t, { tag: server.value.tag })
  },
})


// ---- hosts: path list + predefined records (legacy computeds) ----
const hostsPath = computed({
  get: () => (Array.isArray(server.value.path) ? server.value.path.join(',') : server.value.path ?? ''),
  set: (v: string) => {
    server.value.path = v.length > 0 ? v.split(',').map((item: string) => item.trim()) : undefined
  },
})

const hostsPredefined = computed<{ name: string; value: string }[]>({
  get: () => {
    const pds: { name: string; value: string }[] = []
    const h = server.value.predefined
    if (h) {
      Object.keys(h).forEach((key) => {
        if (Array.isArray(h[key])) {
          pds.push({ name: key, value: h[key].join(',') })
        } else {
          pds.push({ name: key, value: h[key] })
        }
      })
    }
    return pds
  },
  set: (v: { name: string; value: string }[]) => {
    if (v.length > 0) {
      const pds: any = {}
      v.forEach((pd) => {
        pds[pd.name] = pd.value.split(',').map((item: string) => item.trim())
      })
      server.value.predefined = pds
    } else {
      server.value.predefined = undefined
    }
  },
})
const addHostsPredefined = () => { hostsPredefined.value = [...hostsPredefined.value, { name: 'localhost', value: '127.0.0.1,::1' }] }
const delHostsPredefined = (i: number) => { const pds = [...hostsPredefined.value]; pds.splice(i, 1); hostsPredefined.value = pds }
const updatePdsKey = (i: number, k: string) => { const pds = [...hostsPredefined.value]; pds[i] = { ...pds[i], name: k }; hostsPredefined.value = pds }
const updatePdsValue = (i: number, v: string) => { const pds = [...hostsPredefined.value]; pds[i] = { ...pds[i], value: v }; hostsPredefined.value = pds }
</script>
