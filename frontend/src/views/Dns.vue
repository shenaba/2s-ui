<template>
  <DnsServerDrawer
    :open="dnsDrawer.open"
    :index="dnsDrawer.index"
    :data="dnsDrawer.data"
    :ts-tags="tsTags"
    :rslvd-tags="rslvdTags"
    @close="dnsDrawer.open = false"
    @save="saveDnsDrawer"
  />
  <DnsRuleDrawer
    :open="dnsRuleDrawer.open"
    :index="dnsRuleDrawer.index"
    :data="dnsRuleDrawer.data"
    :clients="clients"
    :in-tags="inboundTags"
    :server-tags="dnsServerTags"
    :rule-sets="ruleSets"
    @close="dnsRuleDrawer.open = false"
    @save="saveDnsRuleDrawer"
  />

  <!-- delete confirmation -->
  <DeleteConfirm :open="del.open" @close="del.open = false" @confirm="confirmDelete" />

  <div class="page-stack-lg fade-up">
    <!-- ===================== toolbar ===================== -->
    <div v-if="!failed" class="toolbar" style="justify-content: center;">
      <Btn variant="primary" sm @click="showDnsDrawer(-1)">
        <Ico name="plus" :size="15" /> {{ $t('ui.addDnsServer') }}
      </Btn>
      <Btn variant="primary" sm @click="showDnsRuleDrawer(-1)">
        <Ico name="plus" :size="15" /> {{ $t('ui.addDnsRule') }}
      </Btn>
      <Btn :variant="unchanged ? 'ghost' : 'primary'" sm :loading="loading" :disabled="unchanged" @click="saveConfig">
        <Ico name="check" :size="15" /> {{ $t('actions.save') }}
      </Btn>
    </div>

    <!-- No config arrived: say why instead of leaving a blank page under a
         spinning save button. -->
    <div v-if="failed" style="padding: 24px 0;">
      <EmptyState icon="link" :title="$t('ws.offlineTitle')" :desc="$t('ws.offlineDesc')" />
    </div>

    <template v-if="ready">
      <!-- ===================== dns base config ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.basics') }}</SectionLabel>
        <div class="card" style="padding: 18px; margin-top: 10px;">
          <div class="field-grid">
            <Field :label="$t('dns.final')" :mb="0">
              <Select v-model="finalDns">
                <option value="">{{ $t('dns.firstServer') }}</option>
                <option v-for="tag in dnsServerTags" :key="tag" :value="tag">{{ tag }}</option>
              </Select>
            </Field>
            <Field :label="$t('dns.domainStrategy')" :mb="0">
              <Select v-model="dnsStrategy">
                <option value="">{{ $t('ui.none') }}</option>
                <option v-for="s in strategies" :key="s" :value="s">{{ s }}</option>
              </Select>
            </Field>
            <Field :label="$t('dns.rule.action.clientSubnet')" :mb="0">
              <input class="input mono" v-model="clientSubnet" placeholder="0.0.0.0/0" />
            </Field>
            <Field :label="$t('dns.cacheCapacity')" :mb="0">
              <input class="input mono" type="number" min="1024" v-model="cacheCapacity" />
            </Field>
          </div>
          <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-top: 16px;">
            <CheckLabel v-model="disableCache" :label="$t('dns.disableCache')" />
            <CheckLabel v-model="disableExpire" :label="$t('dns.disableExpire')" />
            <CheckLabel v-model="independentCache" :label="$t('dns.independentCache')" />
            <CheckLabel v-model="reverseMapping" :label="$t('dns.reverseMapping')" />
          </div>
        </div>
      </div>

      <!-- ===================== dns servers ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.dnsServers') }}</SectionLabel>
        <div class="entity-grid" style="margin-top: 10px;">
          <EntityCard
            v-for="(item, index) in dnsServers"
            :key="index"
            :title="item.tag"
            :type="item.type"
            :color="dnsColor(item.type)"
            icon="dns"
            :rows="[
              { k: $t('dns.server'), v: item.server ?? $t('ui.none'), mono: !!item.server },
              { k: $t('in.port'), v: item.server_port ?? $t('ui.none'), mono: !!item.server_port },
              {
                k: $t('objects.tls'),
                v: Object.hasOwn(item, 'tls') ? $t(item.tls?.enabled ? 'enable' : 'disable') : $t('ui.none'),
                color: item.tls?.enabled ? 'var(--emerald)' : undefined,
              },
            ]"
          >
            <template #actions>
              <CardBtn icon="edit" :title="$t('actions.edit')" @click="showDnsDrawer(index)" />
              <CardBtn icon="trash" border danger :title="$t('actions.del')" @click="askDelete('server', index)" />
            </template>
          </EntityCard>
        </div>
      </div>

      <!-- ===================== dns rules ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.dnsRules') }}</SectionLabel>
        <div class="entity-grid" style="margin-top: 10px;">
          <div
            v-for="(item, index) in dnsRules"
            :key="index"
            style="display: flex;"
            draggable="true"
            @dragstart="onDragStart(index)"
            @dragover.prevent
            @drop="onDrop(index)"
          >
            <RuleCard
              style="flex: 1;"
              :n="index + 1"
              :logical="item.type != undefined"
              :mode="item.mode"
              :action="item.action"
              :target-key="$t('dns.server')"
              :target="item.server"
              :rules="matcherCount(item)"
              :invert="item.invert ?? false"
            >
              <template #actions>
                <CardBtn icon="edit" :title="$t('actions.edit')" @click="showDnsRuleDrawer(index)" />
                <button type="button" class="mv-btn" :disabled="index === 0" @click="moveRule(index, -1)">
                  <Ico name="chevronDown" :size="16" style="transform: rotate(180deg);" />
                </button>
                <button type="button" class="mv-btn" :disabled="index === dnsRules.length - 1" @click="moveRule(index, 1)">
                  <Ico name="chevronDown" :size="16" />
                </button>
                <CardBtn icon="trash" border danger :title="$t('actions.del')" @click="askDelete('rule', index)" />
              </template>
            </RuleCard>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, onBeforeMount, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import DnsServerDrawer from '@/layouts/drawers/dns/DnsServerDrawer.vue'
import DnsRuleDrawer from '@/layouts/drawers/dns/DnsRuleDrawer.vue'
import { Config } from '@/types/config'
import { actionDnsRuleKeys, dnsRule } from '@/types/dns'
import { FindDiff } from '@/plugins/utils'
import { useReorder } from '@/plugins/useReorder'
import { dnsColor } from '@/plugins/colors'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import DeleteConfirm from '@/components/ui/DeleteConfirm.vue'
import Field from '@/components/ui/Field.vue'
import CheckLabel from '@/components/ui/CheckLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import EntityCard from '@/components/ui/EntityCard.vue'
import RuleCard from '@/components/ui/RuleCard.vue'
import CardBtn from '@/components/ui/CardBtn.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const oldConfig = ref(<any>{})
const loading = ref(false)
const ready = ref(false)
const failed = ref(false)

const appConfig = computed((): Config => {
  return <Config>Data().config
})

const init = () => {
  // fix old configs
  if (!appConfig.value.dns) appConfig.value.dns = { servers: [], rules: [] }
  if (!appConfig.value.dns.servers) appConfig.value.dns.servers = []
  if (!appConfig.value.dns.rules) appConfig.value.dns.rules = []

  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  ready.value = true
  failed.value = false
  loading.value = false
}

onBeforeMount(async () => {
  loading.value = true
  // Never touch the config without real data: the "fix old configs" writes
  // below mutate the live object, so on an empty one they would seed a config
  // that a save then writes over the panel's real one.
  if (!await Data().waitReady()) {
    loading.value = false
    failed.value = true
    return
  }
  init()
})

// waitReady gives up after 15s, but the socket's reconnect backoff can put a
// successful attempt past that. Without this the page keeps claiming it cannot
// load the configuration while the rest of the panel is live again.
watch(() => Data().lastLoad, (lu) => { if (lu > 0 && failed.value) init() })

const unchanged = computed(() => FindDiff.deepCompare(appConfig.value.dns, oldConfig.value.dns))

const saveConfig = async () => {
  loading.value = true
  const success = await Data().save('config', 'set', appConfig.value)
  if (success) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  }
  loading.value = false
}

const tsTags = computed((): string[] => {
  return Data().endpoints?.filter((e: any) => e.type == 'tailscale').map((e: any) => e.tag)
})

const rslvdTags = computed((): string[] => {
  return Data().services?.filter((e: any) => e.type == 'resolved').map((e: any) => e.tag)
})

const clients = computed((): string[] => Data().clients.map((c: any) => c.name))

const inboundTags = computed((): string[] => Data().inboundTags)

const dns = computed((): any => appConfig.value.dns)

const dnsServers = computed((): any[] => dns.value?.servers ?? [])

const dnsServerTags = computed((): string[] => {
  return dns.value?.servers?.filter((s: any) => s.tag && s.tag != '')?.map((s: any) => s.tag) ?? []
})

const dnsRules = computed((): any[] => dns.value?.rules ?? [])

const ruleSets = computed((): string[] => {
  return appConfig.value?.route?.rule_set?.map((r: any) => r.tag) ?? []
})

/* ---------------- dns base fields ---------------- */
const strategies = ['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only']

const finalDns = computed({
  get() { return dns.value?.final ?? '' },
  set(v: string) { dns.value.final = v.length > 0 ? v : undefined },
})

const dnsStrategy = computed({
  get: () => dns.value?.strategy ?? '',
  set: (v: string) => {
    if (v.length > 0) dns.value.strategy = v
    else delete dns.value.strategy
  },
})

const clientSubnet = computed({
  get: () => dns.value?.client_subnet ?? '',
  set: (v: string) => {
    if (v.length > 0) dns.value.client_subnet = v
    else delete dns.value.client_subnet
  },
})

const cacheCapacity = computed({
  get: (): any => dns.value?.cache_capacity ?? '',
  set: (v: any) => {
    const n = parseInt(v)
    if (!isNaN(n)) dns.value.cache_capacity = n
    else delete dns.value.cache_capacity
  },
})

const disableCache = computed({
  get: () => dns.value?.disable_cache ?? false,
  set: (v: boolean) => { dns.value.disable_cache = v },
})

const disableExpire = computed({
  get: () => dns.value?.disable_expire ?? false,
  set: (v: boolean) => { dns.value.disable_expire = v },
})

const independentCache = computed({
  get: () => dns.value?.independent_cache ?? false,
  set: (v: boolean) => { dns.value.independent_cache = v },
})

const reverseMapping = computed({
  get: () => dns.value?.reverse_mapping ?? false,
  set: (v: boolean) => { dns.value.reverse_mapping = v },
})

/* ---------------- dns server / rule drawers ---------------- */
const matcherCount = (item: any): number =>
  item.rules ? item.rules.length : Object.keys(item).filter((r) => !actionDnsRuleKeys.includes(r)).length

const dnsDrawer = ref({ open: false, index: -1, data: '' })
const showDnsDrawer = (index: number) => {
  dnsDrawer.value.index = index
  dnsDrawer.value.data = index == -1 ? '' : JSON.stringify(dns.value.servers[index])
  dnsDrawer.value.open = true
}
const saveDnsDrawer = (data: any) => {
  if (dnsDrawer.value.index == -1) dns.value.servers.push(data)
  else dns.value.servers[dnsDrawer.value.index] = data
  dnsDrawer.value.open = false
}

const dnsRuleDrawer = ref({ open: false, index: -1, data: '' })
const showDnsRuleDrawer = (index: number) => {
  dnsRuleDrawer.value.index = index
  dnsRuleDrawer.value.data = index == -1 ? '' : JSON.stringify(dnsRules.value[index])
  dnsRuleDrawer.value.open = true
}
const saveDnsRuleDrawer = (data: dnsRule) => {
  if (dnsRuleDrawer.value.index == -1) dnsRules.value.push(data)
  else dnsRules.value[dnsRuleDrawer.value.index] = data
  dnsRuleDrawer.value.open = false
}

/* ---------------- delete confirmation ---------------- */
const del = ref<{ open: boolean; kind: 'server' | 'rule'; index: number }>({ open: false, kind: 'server', index: -1 })
const askDelete = (kind: 'server' | 'rule', index: number) => {
  del.value = { open: true, kind, index }
}
const confirmDelete = () => {
  if (del.value.kind === 'server') dns.value.servers.splice(del.value.index, 1)
  else dnsRules.value.splice(del.value.index, 1)
  del.value.open = false
}

/* ---------------- ordering (drag & drop + move buttons) ---------------- */
const { onDragStart, onDrop, move: moveRule } = useReorder(() => dnsRules.value)
</script>
