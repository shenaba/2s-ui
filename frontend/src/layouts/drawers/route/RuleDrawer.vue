<template>
  <MDrawer
    :open="open"
    icon="rules"
    color="var(--amber)"
    :title="isNew ? $t('ui.ruleNew') : $t('ui.ruleEdit') + ' #' + (index + 1)"
    :sub="$t('ui.ruleSub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="520"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <!-- simple / logical -->
    <Segmented v-model="modeSeg" block :options="[['simple', $t('rule.simple')], ['logical', $t('rule.logical')]]" />

    <Field v-if="logical" :label="$t('rule.mode')">
      <Select v-model="form.mode">
        <option value="and">and</option>
        <option value="or">or</option>
      </Select>
    </Field>

    <!-- action / target -->
    <div class="grid2">
      <Field :label="$t('admin.action')">
        <Select v-model="form.action">
          <option v-for="a in actions" :key="a.value" :value="a.value">{{ a.title }}</option>
        </Select>
      </Field>
      <Field v-if="form.action === 'route'" :label="$t('objects.outbound')">
        <Select v-model="form.outbound">
          <option v-for="o in outTags" :key="o" :value="o">{{ o }}</option>
        </Select>
      </Field>
      <Field v-if="form.action === 'reject'" :label="$t('rule.method')">
        <Select v-model="rejectMethod">
          <option value="">{{ $t('ui.none') }}</option>
          <option value="default">Default</option>
          <option value="drop">Drop</option>
        </Select>
      </Field>
    </div>

    <!-- action extras -->
    <template v-if="form.action === 'route-options'">
      <div class="grid2">
        <Field :label="$t('types.direct.overrideAddr')">
          <input class="input mono" v-model="form.override_address" />
        </Field>
        <Field :label="$t('types.direct.overridePort')">
          <input class="input mono" type="number" min="0" max="65534" v-model.number="form.override_port" />
        </Field>
      </div>
      <Field :label="$t('rule.udpTimeout')">
        <input class="input mono" v-model="form.udp_timeout" />
      </Field>
      <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
        <SwitchLabel :label="$t('rule.udpDisableDomainUnmapping')" :model-value="!!form.udp_disable_domain_unmapping" @update:model-value="form.udp_disable_domain_unmapping = $event" />
        <SwitchLabel :label="$t('rule.udpConnect')" :model-value="!!form.udp_connect" @update:model-value="form.udp_connect = $event" />
      </div>
    </template>

    <div v-if="form.action === 'reject'" style="margin-bottom: 15px;">
      <SwitchLabel :label="$t('rule.noDrop')" :model-value="!!form.no_drop" @update:model-value="form.no_drop = $event" />
    </div>

    <template v-if="form.action === 'sniff'">
      <ChipSelect
        :label="$t('rule.sniffer')"
        :model-value="form.sniffer ?? []"
        :options="sniffers"
        style="margin-bottom: 15px;"
        @update:model-value="form.sniffer = $event"
      />
      <Field :label="$t('rule.timeout')">
        <input class="input mono" v-model="form.timeout" />
      </Field>
    </template>

    <template v-if="form.action === 'resolve'">
      <div class="grid2">
        <Field :label="$t('rule.strategy')">
          <Select v-model="resolveStrategy">
            <option value="">{{ $t('ui.none') }}</option>
            <option v-for="s in strategies" :key="s.value" :value="s.value">{{ s.title }}</option>
          </Select>
        </Field>
        <Field :label="$t('dns.server')">
          <input class="input mono" v-model="form.server" />
        </Field>
      </div>
    </template>

    <!-- match conditions -->
    <MHint v-if="logical" style="margin-bottom: 15px;">{{ $t('ui.logicalHint') }}</MHint>
    <MHint v-if="hasTextList" style="margin-bottom: 15px;">{{ $t('rule.etaHint') }}</MHint>

    <div
      v-for="(r, ri) in (logical ? form.rules : form.rules.slice(0, 1))"
      :key="seq + '-' + ri"
      :class="logical ? 'card' : undefined"
      :style="logical ? { background: 'var(--surface-2)', padding: '13px 14px', marginBottom: '12px' } : { marginBottom: '15px' }"
    >
      <div style="display: flex; align-items: center; margin-bottom: 10px;">
        <SectionLabel>{{ logical ? $t('objects.rule') + ' ' + (Number(ri) + 1) : $t('ui.matchers') }}</SectionLabel>
        <div style="flex: 1;" />
        <IconBtn v-if="logical && form.rules.length > 1" name="trash" danger :title="$t('actions.del')" @click="form.rules.splice(ri, 1)" />
      </div>
      <div style="display: flex; flex-direction: column; gap: 8px;">
        <div
          v-for="k in matcherKeys(r)"
          :key="k"
          style="display: grid; grid-template-columns: 150px 1fr 34px; gap: 8px; align-items: start;"
        >
          <Select style="height: 38px; font-size: 12.5px;" :model-value="k" @change="changeKey(r, k, $event)">
            <option v-for="mk in MATCH_KEYS" :key="mk" :value="mk" :disabled="mk !== k && r[mk] !== undefined">{{ mk }}</option>
          </Select>

          <!-- value control by kind -->
          <ChipSelect
            v-if="kindOf(k) === 'tags' || kindOf(k) === 'multi'"
            :model-value="r[k] ?? []"
            :options="optionsFor(k)"
            @update:model-value="r[k] = $event"
          />
          <Select v-else-if="kindOf(k) === 'ipver'" style="height: 38px; font-size: 12.5px;" :model-value="String(r[k])" @change="r[k] = Number($event)">
            <option value="4">4</option>
            <option value="6">6</option>
          </Select>
          <div v-else-if="kindOf(k) === 'bool'" style="display: flex; align-items: center; height: 38px;">
            <Toggle :model-value="!!r[k]" @update:model-value="r[k] = $event" />
          </div>
          <textarea
            v-else
            class="input mono"
            spellcheck="false"
            dir="ltr"
            :rows="rowsFor(r, k)"
            style="height: auto; padding: 9px 11px; font-size: 12.5px; resize: vertical;"
            :value="csvGet(r, k)"
            :placeholder="PLACEHOLDER[k] ?? ''"
            @change="csvSet(r, k, ($event.target as HTMLTextAreaElement).value)"
          ></textarea>

          <button type="button" class="btn btn-subtle btn-icon" style="height: 38px; width: 34px;" @click="delMatcher(r, k)">
            <Ico name="close" :size="14" />
          </button>
        </div>
        <Btn variant="subtle" sm style="align-self: flex-start;" @click="addCondition(r)">
          <Ico name="plus" :size="14" /> {{ $t('ui.addMatcher') }}
        </Btn>
      </div>
    </div>

    <Btn v-if="logical" sm style="margin-bottom: 15px;" @click="form.rules.push({})">
      <Ico name="plus" :size="14" /> {{ $t('actions.add') + ' ' + $t('objects.rule') }}
    </Btn>

    <!-- invert -->
    <MSwitchRow :label="$t('rule.invert')" :model-value="!!form.invert" @update:model-value="form.invert = $event" />
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import MDrawer from '@/components/ui/MDrawer.vue'
import Segmented from '@/components/ui/Segmented.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import IconBtn from '@/components/ui/IconBtn.vue'
import Toggle from '@/components/ui/Toggle.vue'
import ChipSelect from '@/components/ui/ChipSelect.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MSwitchRow from '@/components/ui/MSwitchRow.vue'
import MHint from '@/components/ui/MHint.vue'
import { rule, actionKeys } from '@/types/rules'

const props = defineProps<{
  open: boolean
  index: number
  data: string
  clients: string[]
  inTags: string[]
  outTags: string[]
  rsTags: string[]
}>()
const emit = defineEmits<{ close: []; save: [data: any] }>()

const isNew = computed(() => props.index === -1)

// match condition keys — full set of the legacy RuleOptions component (components/Rule.vue)
const MATCH_KEYS = [
  'inbound', 'auth_user', 'ip_version', 'network', 'protocol',
  'domain', 'domain_suffix', 'domain_keyword', 'domain_regex', 'ip_cidr', 'ip_is_private',
  'port', 'port_range', 'source_ip_cidr', 'source_ip_is_private', 'source_port', 'source_port_range',
  'preferred_by', 'interface_address', 'network_interface_address', 'default_interface_address',
  'rule_set', 'rule_set_ip_cidr_match_source',
]
// placeholders from design handoff (modals2.jsx MATCH_PLACEHOLDER)
const PLACEHOLDER: Record<string, string> = {
  domain: 'example.com', domain_suffix: '.ir', domain_keyword: 'google', domain_regex: '^stun\\..+',
  rule_set: 'geosite-ads', ip_cidr: '10.0.0.0/24', source_ip_cidr: '192.168.1.0/24',
  port: '443', port_range: '1000:2000', source_port: '5353', network: 'tcp', protocol: 'quic',
  process_name: 'chrome.exe', clash_mode: 'global',
}
const BOOL_KEYS = ['ip_is_private', 'source_ip_is_private', 'rule_set_ip_cidr_match_source']
const NUM_KEYS = ['port', 'source_port']
// A comma is legal regex syntax, so these split on newlines only: comma
// splitting tore a bounded repeat like `a{2,4}` into two entries that matched
// nothing, leaving no way to enter such a pattern at all.
const NEWLINE_ONLY_KEYS = ['domain_regex']
const MULTI_OPTIONS: Record<string, string[]> = {
  network: ['tcp', 'udp', 'icmp'],
  protocol: ['http', 'tls', 'quic', 'stun', 'dns', 'bittorrent', 'dtls', 'ssh', 'rdp', 'ntp'],
}

const actions = [
  { title: 'Route', value: 'route' },
  { title: 'Route Options', value: 'route-options' },
  { title: 'Bypass', value: 'bypass' },
  { title: 'Reject', value: 'reject' },
  { title: 'Hijack DNS', value: 'hijack-dns' },
  { title: 'Sniff', value: 'sniff' },
  { title: 'Resolve', value: 'resolve' },
]
const sniffers = [
  { title: 'HTTP', value: 'http' },
  { title: 'TLS', value: 'tls' },
  { title: 'QUIC', value: 'quic' },
  { title: 'STUN', value: 'stun' },
  { title: 'DNS', value: 'dns' },
  { title: 'BitTorrent', value: 'bittorrent' },
  { title: 'DTLS', value: 'dtls' },
  { title: 'SSH', value: 'ssh' },
  { title: 'RDP', value: 'rdp' },
  { title: 'NTP', value: 'ntp' },
]
const strategies = [
  { title: 'Prefer IPv4', value: 'prefer_ipv4' },
  { title: 'Prefer IPv6', value: 'prefer_ipv6' },
  { title: 'IPv4 Only', value: 'ipv4_only' },
  { title: 'IPv6 Only', value: 'ipv6_only' },
]

const form = ref<any>({ type: 'simple', mode: 'and', rules: [{}], invert: false, action: 'route', outbound: 'direct' })
const seq = ref(0)

function init() {
  if (props.index !== -1) {
    const newData = JSON.parse(props.data)
    if (newData.type) {
      form.value = newData
    } else {
      const f: any = { type: 'simple', mode: 'and', rules: <rule[]>[{}] }
      Object.keys(newData).forEach((key) => {
        if (actionKeys.includes(key)) {
          f[key] = newData[key]
        } else {
          f.rules[0][key] = newData[key]
        }
      })
      form.value = f
    }
  } else {
    form.value = {
      type: 'simple',
      mode: 'and',
      rules: <rule[]>[{}],
      invert: false,
      action: 'route',
      outbound: props.outTags[0] ?? 'direct',
    }
  }
  seq.value++
}
watch(() => props.open, (v) => { if (v) init() })

const logical = computed(() => form.value.type === 'logical')
const modeSeg = computed({
  get: () => (form.value.type === 'logical' ? 'logical' : 'simple'),
  set: (v) => { form.value.type = v === 'logical' ? 'logical' : 'simple' },
})
const rejectMethod = computed({
  get: () => form.value.method ?? '',
  set: (v: string) => { if (v.length > 0) form.value.method = v; else delete form.value.method },
})
const resolveStrategy = computed({
  get: () => form.value.strategy ?? '',
  set: (v: string) => { if (v.length > 0) form.value.strategy = v; else delete form.value.strategy },
})

// ---- matcher helpers ----
const matcherKeys = (r: any): string[] => MATCH_KEYS.filter((k) => r[k] !== undefined)

// Only worth explaining the one-per-line convention while a free-text list is on
// screen; a rule matching purely on chips and toggles has nothing to type into.
const hasTextList = computed(() =>
  (logical.value ? form.value.rules : form.value.rules.slice(0, 1)).some((r: any) =>
    matcherKeys(r).some((k) => kindOf(k) === 'csv' || kindOf(k) === 'nums'),
  ),
)

function kindOf(k: string): string {
  if (k === 'ip_version') return 'ipver'
  if (BOOL_KEYS.includes(k)) return 'bool'
  if (['inbound', 'auth_user', 'rule_set', 'preferred_by'].includes(k)) return 'tags'
  if (MULTI_OPTIONS[k]) return 'multi'
  if (NUM_KEYS.includes(k)) return 'nums'
  return 'csv'
}

function optionsFor(k: string): { title: string; value: string }[] {
  let items: string[] = []
  switch (k) {
    case 'inbound': items = props.inTags; break
    case 'auth_user': items = props.clients; break
    case 'rule_set': items = props.rsTags; break
    case 'preferred_by': items = props.outTags?.length ? props.outTags : props.inTags; break
    default: items = MULTI_OPTIONS[k] ?? []
  }
  return items.map((i) => ({ title: i, value: i }))
}

function defaultFor(k: string): any {
  if (k === 'ip_version') return 4
  if (k === 'protocol') return ['http']
  if (BOOL_KEYS.includes(k)) return false
  return []
}

function addMatcherKey(r: any, k: string) {
  if (k === 'rule_set') {
    r.rule_set = []
    r.rule_set_ip_cidr_match_source = false
    return
  }
  r[k] = defaultFor(k)
}

function delMatcher(r: any, k: string) {
  if (k === 'rule_set') {
    delete r.rule_set
    delete r.rule_set_ip_cidr_match_source
    return
  }
  delete r[k]
}

function changeKey(r: any, oldK: string, newK: string) {
  if (oldK === newK) return
  delMatcher(r, oldK)
  if (r[newK] === undefined) addMatcherKey(r, newK)
}

function addCondition(r: any) {
  const free = MATCH_KEYS.filter((k) => r[k] === undefined)
  if (free.length === 0) return
  addMatcherKey(r, r.domain_suffix === undefined ? 'domain_suffix' : free[0])
}

const csvGet = (r: any, k: string): string => (Array.isArray(r[k]) ? r[k].join('\n') : '')

// One entry per line is how these lists are pasted, and the single-line input
// silently ate the newlines (the HTML value sanitiser strips them), collapsing a
// pasted list into one unmatchable entry. Comma stays accepted so anything typed
// or stored the old way still parses.
function csvSet(r: any, k: string, v: string) {
  const sep = NEWLINE_ONLY_KEYS.includes(k) ? /\n+/ : /[\n,]+/
  const parts = [...new Set(v.split(sep).map((s) => s.trim()).filter((s) => s.length > 0))]
  if (NUM_KEYS.includes(k)) {
    r[k] = parts.length > 0 ? parts.map((s) => parseInt(s, 10)).filter((n) => !isNaN(n)) : []
  } else {
    r[k] = parts
  }
}

// Grow with the list so a long one is not read through a slit, with a ceiling so
// it cannot push the rest of the drawer out of reach.
const rowsFor = (r: any, k: string): number =>
  Math.min(Math.max((Array.isArray(r[k]) ? r[k].length : 0) + 1, 2), 12)

// ---- save (identical payload shaping to the legacy modal) ----
function saveChanges() {
  let newRule = <any>{
    action: form.value.action,
    invert: form.value.invert ? form.value.invert : undefined,
  }

  // Filter action data
  switch (newRule.action) {
    case 'route':
      newRule.outbound = form.value.outbound
      break
    case 'route-options':
      newRule.override_address = form.value.override_address?.length > 0 ? form.value.override_address : undefined
      newRule.override_port = form.value?.override_port > 0 ? form.value.override_port : undefined
      newRule.network_strategy = form.value.network_strategy?.length > 0 ? form.value.network_strategy : undefined
      newRule.fallback_delay = form.value.fallback_delay?.length > 0 ? form.value.fallback_delay : undefined
      newRule.udp_disable_domain_unmapping = form.value.udp_disable_domain_unmapping ? true : undefined
      newRule.udp_connect = form.value.udp_connect ? true : undefined
      newRule.udp_timeout = form.value.udp_timeout?.length > 0 ? form.value.udp_timeout : undefined
      break
    case 'reject':
      newRule.method = form.value.method?.length > 0 ? form.value.method : undefined
      newRule.no_drop = form.value.no_drop ? true : undefined
      break
    case 'sniff':
      newRule.sniffer = form.value.sniffer?.length > 0 ? form.value.sniffer : undefined
      newRule.timeout = form.value.timeout?.length > 0 ? form.value.timeout : undefined
      break
    case 'resolve':
      newRule.strategy = form.value.strategy?.length > 0 ? form.value.strategy : undefined
      newRule.server = form.value.server?.length > 0 ? form.value.server : undefined
      break
  }

  // Add rules
  if (form.value.type === 'simple') {
    newRule = { ...form.value.rules[0], ...newRule }
  } else {
    newRule.type = 'logical'
    newRule.mode = form.value.mode
    newRule.rules = form.value.rules
  }
  emit('save', newRule)
}
</script>
