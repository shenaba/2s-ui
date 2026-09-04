<template>
  <MDrawer
    :open="open"
    icon="dns"
    color="var(--cyan)"
    :title="isNew ? $t('ui.dnsrNew') : $t('objects.dnsrule') + ' #' + (index + 1)"
    :sub="$t('ui.dnsrSub')"
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
      <Field :label="$t('dns.rule.action.title')">
        <Select v-model="form.action">
          <option v-for="a in actions" :key="a.value" :value="a.value">{{ a.title }}</option>
        </Select>
      </Field>
      <Field v-if="['route', 'evaluate'].includes(form.action)" :label="$t('dns.server')">
        <Select v-model="form.server">
          <option v-for="s in serverTags" :key="s" :value="s">{{ s }}</option>
        </Select>
      </Field>
      <!-- Naming the response lets several coexist, picked apart by a
           match_response carrying the same tag. -->
      <Field v-if="form.action === 'evaluate'" :label="$t('dns.rule.action.evaluateTag')">
        <input class="input mono" v-model="evaluateTag" />
      </Field>
      <Field v-if="form.action === 'reject'" :label="$t('rule.method')">
        <Select v-model="rejectMethod">
          <option value="">{{ $t('ui.none') }}</option>
          <option value="default">Default</option>
          <option value="drop">Drop</option>
        </Select>
      </Field>
      <Field v-if="form.action === 'predefined'" :label="$t('dns.rule.action.rcode')">
        <Select v-model="predefRcode">
          <option value="">{{ $t('ui.none') }}</option>
          <option v-for="rc in predefinedRcode" :key="rc.value" :value="rc.value">{{ rc.title }}</option>
        </Select>
      </Field>
    </div>

    <div style="margin-bottom: 15px;">
      <SwitchLabel :label="$t('dns.rule.action.race')" :model-value="race" @update:model-value="race = $event" />
    </div>

    <!-- respond takes nothing of its own: sing-box unmarshals it with unknown
         fields disallowed, so anything else here would be refused. -->
    <template v-if="['route', 'evaluate', 'route-options'].includes(form.action)">
      <div class="grid2">
        <!-- Deprecated in sing-box 1.14 and removed in 1.16, and it stops the
             core outright once any rule sets ip_version or query_type. Shown
             only for a rule that already carries one, so it can be cleared
             without offering it to a rule that does not.
             sing-box reads a strategy on route-options too, but saveChanges
             rebuilds the rule per action and that branch does not carry one, so
             this stays on route: the select would otherwise take a value the
             save throws away. Clearing an imported route-options strategy needs
             no control -- saving the rule at all drops it. -->
        <Field v-if="form.action === 'route' && hadStrategy" :label="$t('rule.strategy')" :hint="$t('dns.rule.legacyStrategy')">
          <Select v-model="routeStrategy">
            <option value="">{{ $t('ui.none') }}</option>
            <option v-for="s in strategies" :key="s.value" :value="s.value">{{ s.title }}</option>
          </Select>
        </Field>
        <Field :label="$t('dns.rule.action.rewriteTtl')">
          <input class="input mono" type="number" min="0" v-model.number="form.rewrite_ttl" />
        </Field>
        <Field :label="$t('dns.rule.action.timeout')">
          <input class="input mono" placeholder="10s" v-model="actionTimeout" />
        </Field>
        <!-- Setting a subnet and removing one are the same field to sing-box:
             whichever is written last wins, so the switch clears the input. -->
        <Field v-if="!removeClientSubnet" :label="$t('dns.rule.action.clientSubnet')">
          <input class="input mono" v-model="form.client_subnet" />
        </Field>
      </div>
      <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
        <SwitchLabel :label="$t('dns.disableCache')" :model-value="!!form.disable_cache" @update:model-value="form.disable_cache = $event" />
        <SwitchLabel :label="$t('dns.rule.action.disableOptimisticCache')" :model-value="disableOptimisticCache" @update:model-value="disableOptimisticCache = $event" />
        <SwitchLabel :label="$t('dns.rule.action.removeClientSubnet')" :model-value="removeClientSubnet" @update:model-value="removeClientSubnet = $event" />
        <SwitchLabel v-if="form.action !== 'route-options'" :label="$t('dns.rule.action.speculative')" :model-value="speculative" @update:model-value="speculative = $event" />
      </div>
    </template>

    <!-- reject extras -->
    <div v-if="form.action === 'reject'" style="margin-bottom: 15px;">
      <SwitchLabel :label="$t('rule.noDrop')" :model-value="!!form.no_drop" @update:model-value="form.no_drop = $event" />
    </div>

    <!-- predefined extras -->
    <template v-if="form.action === 'predefined' && form.rcode === 'NOERROR'">
      <Field :label="$t('dns.rule.action.answer') + ' ' + $t('commaSeparated')">
        <input class="input mono" v-model="answer" />
      </Field>
      <Field :label="$t('dns.rule.action.ns') + ' ' + $t('commaSeparated')">
        <input class="input mono" v-model="ns" />
      </Field>
      <Field :label="$t('dns.rule.action.extra') + ' ' + $t('commaSeparated')">
        <input class="input mono" v-model="extra" />
      </Field>
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
            <option v-for="mk in matchKeysFor(r)" :key="mk" :value="mk" :disabled="mk !== k && r[mk] !== undefined">{{ mk }}</option>
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
          <Select v-else-if="kindOf(k) === 'rcode'" style="height: 38px; font-size: 12.5px;" :model-value="r[k]" @change="r[k] = $event">
            <option v-for="rc in predefinedRcode" :key="rc.value" :value="rc.value">{{ rc.title }}</option>
          </Select>
          <div v-else-if="kindOf(k) === 'bool'" style="display: flex; align-items: center; height: 38px;">
            <Toggle :model-value="!!r[k]" @update:model-value="r[k] = $event" />
          </div>
          <!-- One field for both shapes the option takes: blank means whatever
               the preceding evaluate fetched, text picks the response with that
               tag. It never stores "", which sing-box rejects. -->
          <input
            v-else-if="kindOf(k) === 'matchresp'"
            class="input mono"
            style="height: 38px; font-size: 12.5px;"
            :value="r[k] === true ? '' : r[k]"
            :placeholder="$t('dns.rule.matchResponsePlaceholder')"
            @change="setMatchResponse(r, ($event.target as HTMLInputElement).value)"
          />
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
import { useI18n } from 'vue-i18n'
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
import { dnsRule, actionDnsRuleKeys } from '@/types/dns'

const props = defineProps<{
  open: boolean
  index: number
  data: string
  clients: string[]
  inTags: string[]
  serverTags: string[]
  ruleSets: string[]
}>()
const emit = defineEmits<{ close: []; save: [data: any] }>()

const { t } = useI18n()

const isNew = computed(() => props.index === -1)

// match condition keys — full set of the legacy DNS RuleOptions component (components/DnsRule.vue)
const MATCH_KEYS = [
  'inbound', 'auth_user', 'ip_version', 'query_type', 'query_client_subnet', 'query_dnssec', 'protocol',
  'domain', 'domain_suffix', 'domain_keyword', 'domain_regex',
  'port', 'port_range', 'source_ip_cidr', 'source_ip_is_private', 'source_port', 'source_port_range',
  'source_mac_address', 'source_hostname', 'package_name_regex', 'preferred_by',
  'rule_set', 'match_response',
]
// Only meaningful against a response an evaluate already fetched, and matching
// an address without match_response is the legacy filter 1.14 deprecated -- so
// they appear on a rule once match_response is on it, and not before.
const RESPONSE_KEYS = ['response_rcode', 'response_answer', 'response_ns', 'response_extra', 'ip_cidr', 'ip_is_private']
const PLACEHOLDER: Record<string, string> = {
  domain: 'example.com', domain_suffix: '.ir', domain_keyword: 'google', domain_regex: '^stun\\..+',
  rule_set: 'geosite-ads', ip_cidr: '10.0.0.0/24', source_ip_cidr: '192.168.1.0/24',
  port: '443', port_range: '1000:2000', source_port: '5353', network: 'tcp', protocol: 'quic',
  process_name: 'chrome.exe', clash_mode: 'global',
}
const BOOL_KEYS = ['source_ip_is_private', 'query_dnssec', 'ip_is_private']
const NUM_KEYS = ['port', 'source_port']
// A comma is legal regex syntax, so these split on newlines only: comma
// splitting tore a bounded repeat like `a{2,4}` into two entries that matched
// nothing, leaving no way to enter such a pattern at all.
const NEWLINE_ONLY_KEYS = ['domain_regex', 'response_answer', 'response_ns', 'response_extra']
const MULTI_OPTIONS: Record<string, string[]> = {
  protocol: ['http', 'tls', 'quic', 'stun', 'dns'],
  // The replacement for the action strategy this drawer no longer offers:
  // splitting a rule by record type is how 1.14 wants an A/AAAA preference
  // expressed. sing-box takes any name miekg/dns knows, or a number; these are
  // the ones a routing rule realistically names.
  query_type: ['A', 'AAAA', 'CNAME', 'HTTPS', 'SVCB', 'MX', 'TXT', 'SRV', 'PTR', 'NS', 'SOA'],
}

const actions = [
  { title: t('dns.rule.action.route'), value: 'route' },
  { title: t('dns.rule.action.evaluate'), value: 'evaluate' },
  { title: t('dns.rule.action.respond'), value: 'respond' },
  { title: t('dns.rule.action.routeOptions'), value: 'route-options' },
  { title: t('dns.rule.action.reject'), value: 'reject' },
  { title: t('dns.rule.action.predefined'), value: 'predefined' },
]
const strategies = [
  { title: 'Prefer IPv4', value: 'prefer_ipv4' },
  { title: 'Prefer IPv6', value: 'prefer_ipv6' },
  { title: 'IPv4 Only', value: 'ipv4_only' },
  { title: 'IPv6 Only', value: 'ipv6_only' },
]
const predefinedRcode = [
  { title: t('dns.rule.action.rcodes.noError'), value: 'NOERROR' },
  { title: t('dns.rule.action.rcodes.formerr'), value: 'FORMERR' },
  { title: t('dns.rule.action.rcodes.servFail'), value: 'SERVFAIL' },
  { title: t('dns.rule.action.rcodes.nxDomain'), value: 'NXDOMAIN' },
  { title: t('dns.rule.action.rcodes.notImp'), value: 'NOTIMP' },
  { title: t('dns.rule.action.rcodes.refused'), value: 'REFUSED' },
]

const form = ref<any>({ type: 'simple', mode: 'and', rules: [{}], invert: false, action: 'route', server: 'local' })
const seq = ref(0)

function init() {
  if (props.index !== -1) {
    const newData = JSON.parse(props.data)
    if (newData.type) {
      form.value = newData
    } else {
      const f: any = { type: 'simple', mode: 'and', rules: <dnsRule[]>[{}] }
      Object.keys(newData).forEach((key) => {
        if (actionDnsRuleKeys.includes(key)) {
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
      rules: <dnsRule[]>[{}],
      invert: false,
      action: 'route',
      server: props.serverTags[0] ?? 'local',
    }
  }
  hadStrategy.value = !!form.value.strategy
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
// Latched at load rather than tracking form.strategy, so clearing the select
// leaves the row on screen instead of removing the control mid-edit.
const hadStrategy = ref(false)
const evaluateTag = computed({
  get: () => form.value.tag ?? '',
  set: (v: string) => { if (v.length > 0) form.value.tag = v; else delete form.value.tag },
})
const actionTimeout = computed({
  get: () => form.value.timeout ?? '',
  set: (v: string) => {
    const trimmed = (v ?? '').trim()
    if (trimmed) form.value.timeout = trimmed
    else delete form.value.timeout
  },
})
// Flags sing-box only reads when true, so false leaves no key rather than
// writing one that says nothing.
const flag = (key: string) =>
  computed({
    get: (): boolean => form.value[key] === true,
    set: (v: boolean) => { if (v) form.value[key] = true; else delete form.value[key] },
  })
const race = flag('race')
const speculative = flag('speculative')
const disableOptimisticCache = flag('disable_optimistic_cache')
const removeClientSubnet = computed({
  get: (): boolean => form.value.remove_client_subnet === true,
  set: (v: boolean) => {
    if (!v) {
      delete form.value.remove_client_subnet
      return
    }
    form.value.remove_client_subnet = true
    // Both write the same field on the query; keeping a subnet here would only
    // be the value the removal then discards.
    delete form.value.client_subnet
  },
})
const routeStrategy = computed({
  get: () => form.value.strategy ?? '',
  set: (v: string) => { if (v.length > 0) form.value.strategy = v; else delete form.value.strategy },
})
const predefRcode = computed({
  get: () => form.value.rcode ?? '',
  set: (v: string) => { if (v.length > 0) form.value.rcode = v; else delete form.value.rcode },
})

// predefined NOERROR answers (comma separated, legacy computed)
const answer = computed({
  get: () => (form.value.answer?.length > 0 ? form.value.answer.join(',') : ''),
  set: (v: string) => { form.value.answer = v.length > 0 ? v.split(',') : undefined },
})
const ns = computed({
  get: () => (form.value.ns?.length > 0 ? form.value.ns.join(',') : ''),
  set: (v: string) => { form.value.ns = v.length > 0 ? v.split(',') : undefined },
})
const extra = computed({
  get: () => (form.value.extra?.length > 0 ? form.value.extra.join(',') : ''),
  set: (v: string) => { form.value.extra = v.length > 0 ? v.split(',') : undefined },
})

// ---- matcher helpers ----
const matchKeysFor = (r: any): string[] =>
  r.match_response !== undefined || RESPONSE_KEYS.some((k) => r[k] !== undefined)
    ? MATCH_KEYS.concat(RESPONSE_KEYS)
    : MATCH_KEYS
const matcherKeys = (r: any): string[] => matchKeysFor(r).filter((k) => r[k] !== undefined)

// true rather than "" when no tag is given: sing-box refuses an empty tag, and
// true is what "any evaluated response" is spelled as.
function setMatchResponse(r: any, value: string) {
  const tag = (value ?? '').trim()
  r.match_response = tag.length > 0 ? tag : true
}

// Only worth explaining the one-per-line convention while a free-text list is on
// screen; a rule matching purely on chips and toggles has nothing to type into.
const hasTextList = computed(() =>
  (logical.value ? form.value.rules : form.value.rules.slice(0, 1)).some((r: any) =>
    matcherKeys(r).some((k) => kindOf(k) === 'csv' || kindOf(k) === 'nums'),
  ),
)

function kindOf(k: string): string {
  if (k === 'ip_version') return 'ipver'
  if (k === 'match_response') return 'matchresp'
  if (k === 'response_rcode') return 'rcode'
  if (BOOL_KEYS.includes(k)) return 'bool'
  if (['inbound', 'auth_user', 'rule_set'].includes(k)) return 'tags'
  if (MULTI_OPTIONS[k]) return 'multi'
  if (NUM_KEYS.includes(k)) return 'nums'
  return 'csv'
}

function optionsFor(k: string): { title: string; value: string }[] {
  let items: string[] = []
  switch (k) {
    case 'inbound': items = props.inTags; break
    case 'auth_user': items = props.clients; break
    case 'rule_set': items = props.ruleSets; break
    default: items = MULTI_OPTIONS[k] ?? []
  }
  return items.map((i) => ({ title: i, value: i }))
}

function defaultFor(k: string): any {
  if (k === 'ip_version') return 4
  if (k === 'match_response') return true
  if (k === 'response_rcode') return 'NOERROR'
  if (k === 'protocol') return ['http']
  if (BOOL_KEYS.includes(k)) return false
  return []
}

function addMatcherKey(r: any, k: string) {
  r[k] = defaultFor(k)
}

function delMatcher(r: any, k: string) {
  delete r[k]
}

function changeKey(r: any, oldK: string, newK: string) {
  if (oldK === newK) return
  delMatcher(r, oldK)
  if (r[newK] === undefined) addMatcherKey(r, newK)
}

function addCondition(r: any) {
  const free = matchKeysFor(r).filter((k) => r[k] === undefined)
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
    race: form.value.race ? true : undefined,
  }

  // The options every query-issuing action shares.
  const queryOptions = () => {
    newRule.disable_cache = form.value.disable_cache ? true : undefined
    newRule.disable_optimistic_cache = form.value.disable_optimistic_cache ? true : undefined
    newRule.rewrite_ttl = form.value.rewrite_ttl > 0 ? form.value.rewrite_ttl : undefined
    newRule.timeout = form.value.timeout?.length > 0 ? form.value.timeout : undefined
    newRule.remove_client_subnet = form.value.remove_client_subnet ? true : undefined
    newRule.client_subnet =
      !form.value.remove_client_subnet && form.value.client_subnet?.length > 0 ? form.value.client_subnet : undefined
  }

  // Filter action data
  switch (newRule.action) {
    case 'route':
      newRule.server = form.value.server
      newRule.strategy = form.value.strategy?.length > 0 ? form.value.strategy : undefined
      newRule.speculative = form.value.speculative ? true : undefined
      queryOptions()
      break
    case 'evaluate':
      newRule.server = form.value.server
      newRule.tag = form.value.tag?.length > 0 ? form.value.tag : undefined
      newRule.speculative = form.value.speculative ? true : undefined
      queryOptions()
      break
    case 'respond':
      // Nothing: sing-box unmarshals this action with unknown fields
      // disallowed, so any option here is refused outright.
      break
    case 'route-options':
      queryOptions()
      break
    case 'reject':
      newRule.method = form.value.method?.length > 0 ? form.value.method : undefined
      newRule.no_drop = form.value.no_drop ? true : undefined
      break
    case 'predefined':
      newRule.rcode = form.value.rcode?.length > 0 ? form.value.rcode : undefined
      if (form.value.rcode === 'NOERROR') {
        newRule.answer = form.value.answer
        newRule.ns = form.value.ns
        newRule.extra = form.value.extra
      }
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
