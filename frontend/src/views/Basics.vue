<template>
  <div class="page-stack fade-up" style="gap: 14px; max-width: 880px;">
    <!-- toolbar -->
    <div v-if="!failed" class="toolbar" style="justify-content: center;">
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
      <!-- ===================== Log ===================== -->
      <div class="card panel">
        <div class="panel-head">{{ $t('basic.log.title') }}</div>
        <div class="panel-body">
          <div class="field-grid">
            <Field :label="$t('basic.log.level')" :mb="0">
              <Select v-model="logLevel">
                <option value="">{{ $t('ui.none') }}</option>
                <option v-for="l in levels" :key="l" :value="l">{{ l }}</option>
              </Select>
            </Field>
            <Field :label="$t('basic.log.output')" :mb="0">
              <input class="input mono" v-model="appConfig.log.output" />
            </Field>
          </div>
          <div class="switch-row">
            <SwitchLabel v-model="logDisabled" :label="$t('disable')" />
            <SwitchLabel v-model="logTimestamp" :label="$t('basic.log.timestamp')" />
          </div>
        </div>
      </div>

      <!-- ===================== NTP ===================== -->
      <div class="card panel">
        <div class="panel-head">{{ $t('ui.ntp') }}</div>
        <div class="panel-body">
          <div style="margin-bottom: 14px;"><SwitchLabel v-model="enableNtp" :label="$t('enable')" /></div>
          <template v-if="appConfig.ntp?.enabled">
            <div class="field-grid">
              <Field :label="$t('out.addr')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.server" />
              </Field>
              <Field :label="$t('out.port')" :mb="0">
                <input class="input mono" type="number" min="1" v-model="ntpPort" />
              </Field>
              <Field :label="$t('ruleset.interval')" :hint="$t('date.m')" :mb="0">
                <input class="input mono" type="number" min="0" v-model.number="ntpInterval" />
              </Field>
            </div>

            <!-- dial options (same field set as the old shared Dial sub-form) -->
            <div class="sub-label">{{ $t('objects.dial') }}</div>
            <div class="field-grid">
              <Field v-if="optDetour" :label="$t('dial.detourText')" :mb="0">
                <Select v-model="appConfig.ntp.detour">
                  <option v-for="tag in outboundTags" :key="tag" :value="tag">{{ tag }}</option>
                </Select>
              </Field>
              <Field v-if="optBind" :label="$t('dial.bindIf')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.bind_interface" />
              </Field>
              <Field v-if="optIPV4" :label="$t('dial.bindIp4')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.inet4_bind_address" />
              </Field>
              <Field v-if="optIPV6" :label="$t('dial.bindIp6')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.inet6_bind_address" />
              </Field>
              <Field v-if="optRM" label="Linux Routing Mark" :mb="0">
                <input class="input mono" type="number" min="0" v-model="routingMark" />
              </Field>
              <Field v-if="optCT" :label="$t('dial.connTimeout')" :hint="$t('date.s')" :mb="0">
                <input class="input mono" type="number" min="1" v-model="connectTimeout" />
              </Field>
              <Field v-if="optTcpKeepAlive" :label="$t('dial.tcpKeepAlive')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.tcp_keep_alive" />
              </Field>
              <Field v-if="optTcpKeepAlive" :label="$t('dial.tcpKeepAliveInterval')" :mb="0">
                <input class="input mono" v-model="appConfig.ntp.tcp_keep_alive_interval" />
              </Field>
              <Field v-if="optDR" :label="$t('dial.domainResolver')" :mb="0">
                <Select v-model="appConfig.ntp.domain_resolver">
                  <option v-for="tag in dialDnsTags" :key="tag" :value="tag">{{ tag }}</option>
                </Select>
              </Field>
            </div>
            <div v-if="optBindNoPort || optRA || optTCP || optUDP || optTcpKeepAlive" class="switch-row">
              <SwitchLabel v-if="optBindNoPort" v-model="bindNoPort" :label="$t('dial.bindNoPort')" />
              <SwitchLabel v-if="optRA" v-model="reuseAddr" :label="$t('dial.reuseAddr')" />
              <SwitchLabel v-if="optTCP" v-model="tcpFastOpen" label="TCP Fast Open" />
              <SwitchLabel v-if="optTCP" v-model="tcpMultiPath" label="TCP Multi Path" />
              <SwitchLabel v-if="optUDP" v-model="udpFragment" label="UDP Fragment" />
              <SwitchLabel v-if="optTcpKeepAlive" v-model="disableTcpKeepAlive" :label="$t('dial.disableTcpKeepAlive')" />
            </div>
            <div style="display: flex; justify-content: flex-end; margin-top: 14px;">
              <Pop :min-width="240" direction="up">
                <template #trigger="{ toggle }">
                  <Btn variant="subtle" sm @click="toggle">{{ $t('dial.options') }} <Ico name="chevronDown" :size="14" /></Btn>
                </template>
                <label class="pop-item" @click.prevent="optDetour = !optDetour"><Toggle :model-value="optDetour" style="pointer-events: none;" /> {{ $t('listen.detour') }}</label>
                <label class="pop-item" @click.prevent="optBind = !optBind"><Toggle :model-value="optBind" style="pointer-events: none;" /> {{ $t('dial.bindIf') }}</label>
                <label class="pop-item" @click.prevent="optIPV4 = !optIPV4"><Toggle :model-value="optIPV4" style="pointer-events: none;" /> {{ $t('dial.bindIp4') }}</label>
                <label class="pop-item" @click.prevent="optIPV6 = !optIPV6"><Toggle :model-value="optIPV6" style="pointer-events: none;" /> {{ $t('dial.bindIp6') }}</label>
                <label class="pop-item" @click.prevent="optBindNoPort = !optBindNoPort"><Toggle :model-value="optBindNoPort" style="pointer-events: none;" /> {{ $t('dial.bindNoPort') }}</label>
                <label class="pop-item" @click.prevent="optRM = !optRM"><Toggle :model-value="optRM" style="pointer-events: none;" /> Routing Mark</label>
                <label class="pop-item" @click.prevent="optRA = !optRA"><Toggle :model-value="optRA" style="pointer-events: none;" /> {{ $t('dial.reuseAddr') }}</label>
                <label class="pop-item" @click.prevent="optTCP = !optTCP"><Toggle :model-value="optTCP" style="pointer-events: none;" /> {{ $t('listen.tcpOptions') }}</label>
                <label class="pop-item" @click.prevent="optUDP = !optUDP"><Toggle :model-value="optUDP" style="pointer-events: none;" /> {{ $t('listen.udpOptions') }}</label>
                <label class="pop-item" @click.prevent="optCT = !optCT"><Toggle :model-value="optCT" style="pointer-events: none;" /> {{ $t('dial.connTimeout') }}</label>
                <label class="pop-item" @click.prevent="optTcpKeepAlive = !optTcpKeepAlive"><Toggle :model-value="optTcpKeepAlive" style="pointer-events: none;" /> {{ $t('dial.tcpKeepAlive') }}</label>
                <label class="pop-item" @click.prevent="optDR = !optDR"><Toggle :model-value="optDR" style="pointer-events: none;" /> {{ $t('dial.domainResolver') }}</label>
              </Pop>
            </div>
          </template>
        </div>
      </div>

      <!-- ===================== Experimental ===================== -->
      <div class="card panel">
        <div class="panel-head">{{ $t('ui.experimental') }}</div>
        <div class="panel-body">
          <!-- Cache file -->
          <div class="sub-label" style="margin-top: 0;">{{ $t('ui.cacheFile') }}</div>
          <div style="margin-bottom: 12px;"><SwitchLabel v-model="enableCacheFile" :label="$t('enable')" /></div>
          <template v-if="appConfig.experimental.cache_file">
            <div class="field-grid">
              <Field :label="$t('transport.path')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.cache_file.path" />
              </Field>
              <Field :label="$t('ui.cacheId')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.cache_file.cache_id" />
              </Field>
            </div>
            <div style="margin-top: 12px;"><SwitchLabel v-model="storeFakeip" :label="$t('basic.exp.storeFakeIp')" /></div>
          </template>

          <!-- Clash API -->
          <div class="sub-label">{{ $t('ui.clashApi') }}</div>
          <div style="margin-bottom: 12px;"><SwitchLabel v-model="enableClashApi" :label="$t('enable')" /></div>
          <template v-if="appConfig.experimental.clash_api">
            <div class="field-grid">
              <Field :label="$t('basic.exp.extController')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.clash_api.external_controller" />
              </Field>
              <Field :label="$t('basic.exp.secret')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.clash_api.secret" />
              </Field>
              <Field :label="$t('basic.exp.extUi')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.clash_api.external_ui" />
              </Field>
              <Field :label="$t('basic.exp.extUiDownloadUrl')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.clash_api.external_ui_download_url" />
              </Field>
              <Field :label="$t('basic.exp.extUiDownloadDetour')" :mb="0">
                <Select v-model="extUiDownloadDetour">
                  <option value="">{{ $t('ui.none') }}</option>
                  <option v-for="tag in outboundTags" :key="tag" :value="tag">{{ tag }}</option>
                </Select>
              </Field>
              <Field :label="$t('basic.exp.defaultMode')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.clash_api.default_mode" />
              </Field>
              <Field :label="$t('basic.exp.allowOrigin') + ' ' + $t('commaSeparated')" :mb="0">
                <input class="input mono" v-model="origin" />
              </Field>
            </div>
            <div style="margin-top: 12px;"><SwitchLabel v-model="allowPrivateNetwork" :label="$t('basic.exp.allowPrivate')" /></div>
          </template>

          <!-- V2Ray API -->
          <div class="sub-label">{{ $t('ui.v2rayApi') }}</div>
          <div class="switch-row" style="margin-top: 0; margin-bottom: 12px;">
            <SwitchLabel v-model="enableV2rayApi" :label="$t('enable')" />
            <SwitchLabel v-if="appConfig.experimental.v2ray_api" v-model="appConfig.experimental.v2ray_api.stats.enabled" :label="$t('stats.enable')" />
          </div>
          <template v-if="appConfig.experimental.v2ray_api">
            <div class="field-grid">
              <Field :label="$t('objects.listen')" :mb="0">
                <input class="input mono" v-model="appConfig.experimental.v2ray_api.listen" />
              </Field>
            </div>
            <div v-if="appConfig.experimental.v2ray_api.stats?.enabled" class="field-grid" style="margin-top: 14px;">
              <ChipSelect v-model="appConfig.experimental.v2ray_api.stats.inbounds" :options="inboundOptions" :label="$t('pages.inbounds')" :placeholder="$t('ui.selectHint')" />
              <ChipSelect v-model="appConfig.experimental.v2ray_api.stats.outbounds" :options="outboundOptions" :label="$t('pages.outbounds')" :placeholder="$t('ui.selectHint')" />
              <ChipSelect v-model="appConfig.experimental.v2ray_api.stats.users" :options="clientOptions" :label="$t('pages.clients')" :placeholder="$t('ui.selectHint')" />
            </div>
          </template>
        </div>
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, onBeforeMount, ref } from 'vue'
import Data from '@/store/modules/data'
import { Config, Ntp } from '@/types/config'
import { FindDiff } from '@/plugins/utils'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Field from '@/components/ui/Field.vue'
import Pop from '@/components/ui/Pop.vue'
import Toggle from '@/components/ui/Toggle.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import ChipSelect from '@/components/ui/ChipSelect.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const oldConfig = ref({})
const loading = ref(false)
const ready = ref(false)
const failed = ref(false)

const appConfig = computed((): Config => {
  return <Config>Data().config
})

onBeforeMount(async () => {
  loading.value = true
  // Never render the form without real data: it edits the live config object,
  // so an empty one would let a save wipe the panel's whole configuration.
  // Show the offline notice instead — a bare spinner reads as "still loading"
  // forever, and the usual cause (a proxy not forwarding Upgrade) is something
  // only the operator can fix.
  if (!await Data().waitReady()) {
    loading.value = false
    failed.value = true
    return
  }
  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  ready.value = true
  loading.value = false
})

const unchanged = computed(() => {
  return FindDiff.deepCompare(appConfig.value, oldConfig.value)
})

const saveConfig = async () => {
  loading.value = true
  const success = await Data().save('config', 'set', appConfig.value)
  if (success) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  }
  loading.value = false
}

const inboundTags = computed((): string[] => Data().inboundTags)

const clientNames = computed((): string[] => {
  const clients = <any[]>Data().clients
  return clients?.map(c => c.name)
})

const outboundTags = computed((): string[] => Data().outboundTags)

const toOptions = (tags: string[]) => (tags ?? []).map(t => ({ title: t, value: t }))
const inboundOptions = computed(() => toOptions(inboundTags.value))
const outboundOptions = computed(() => toOptions(outboundTags.value))
const clientOptions = computed(() => toOptions(clientNames.value))

const levels = ["trace", "debug", "info", "warn", "error", "fatal", "panic"]

/* ---------------- log ---------------- */
const logLevel = computed({
  get: () => appConfig.value.log?.level ?? '',
  set: (v: string) => { if (v) appConfig.value.log.level = v; else delete appConfig.value.log.level }
})

const logDisabled = computed({
  get: () => appConfig.value.log?.disabled ?? false,
  set: (v: boolean) => { appConfig.value.log.disabled = v }
})

const logTimestamp = computed({
  get: () => appConfig.value.log?.timestamp ?? false,
  set: (v: boolean) => { appConfig.value.log.timestamp = v }
})

/* ---------------- ntp ---------------- */
const enableNtp = computed({
  get() { return appConfig.value.ntp?.enabled ?? false },
  set(v: boolean) {
    if (v) {
      appConfig.value.ntp = <Ntp>{ enabled: true, server: 'time.apple.com', server_port: 123, interval: '30m' }
    } else { appConfig.value.ntp = <Ntp>{} }
  }
})

const ntpPort = computed({
  get: (): any => appConfig.value.ntp?.server_port ?? '',
  set: (v: any) => {
    if (!appConfig.value.ntp) return
    const n = parseInt(v)
    if (!isNaN(n)) appConfig.value.ntp.server_port = n
    else delete appConfig.value.ntp.server_port
  }
})

const ntpInterval = computed({
  get(): any { return appConfig.value.ntp?.interval ? parseInt(appConfig.value.ntp?.interval.replace('m', '')) : null },
  set(v: number) { if (appConfig.value.ntp) v > 0 ? appConfig.value.ntp.interval = v + 'm' : delete appConfig.value.ntp.interval }
})

/* ---------------- ntp dial (mirrors the old shared Dial.vue) ---------------- */
const dialDnsTags = computed((): string[] => Data().config.dns?.servers?.map((d: any) => d.tag) ?? [])

const connectTimeout = computed({
  get: () => appConfig.value.ntp?.connect_timeout ? parseInt(appConfig.value.ntp.connect_timeout.replace('s', '')) : 5,
  set: (v: number) => { if (appConfig.value.ntp) appConfig.value.ntp.connect_timeout = v > 0 ? v + 's' : '5s' }
})

const routingMark = computed({
  get: () => appConfig.value.ntp?.routing_mark ?? 0,
  set: (v: number) => { if (appConfig.value.ntp) appConfig.value.ntp.routing_mark = v > 0 ? v : 0 }
})

const optDetour = computed({
  get: () => appConfig.value.ntp?.detour != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.detour = outboundTags.value[0] ?? '' : delete d.detour }
})

const optBind = computed({
  get: () => appConfig.value.ntp?.bind_interface != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.bind_interface = '' : delete d.bind_interface }
})

const optIPV4 = computed({
  get: () => appConfig.value.ntp?.inet4_bind_address != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.inet4_bind_address = '' : delete d.inet4_bind_address }
})

const optIPV6 = computed({
  get: () => appConfig.value.ntp?.inet6_bind_address != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.inet6_bind_address = '' : delete d.inet6_bind_address }
})

const optBindNoPort = computed({
  get: () => appConfig.value.ntp?.bind_address_no_port != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.bind_address_no_port = true : delete d.bind_address_no_port }
})

const optTcpKeepAlive = computed({
  get: () => {
    const d = appConfig.value.ntp
    return d?.disable_tcp_keep_alive != undefined || d?.tcp_keep_alive != undefined || d?.tcp_keep_alive_interval != undefined
  },
  set: (v: boolean) => {
    const d = appConfig.value.ntp
    if (!d) return
    if (v) {
      d.tcp_keep_alive = '5m'
      d.tcp_keep_alive_interval = '75s'
    } else {
      delete d.disable_tcp_keep_alive
      delete d.tcp_keep_alive
      delete d.tcp_keep_alive_interval
    }
  }
})

const optRM = computed({
  get: () => appConfig.value.ntp?.routing_mark != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.routing_mark = 0 : delete d.routing_mark }
})

const optRA = computed({
  get: () => appConfig.value.ntp?.reuse_addr != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.reuse_addr = true : delete d.reuse_addr }
})

const optTCP = computed({
  get: () => appConfig.value.ntp?.tcp_fast_open != undefined && appConfig.value.ntp?.tcp_multi_path != undefined,
  set: (v: boolean) => {
    const d = appConfig.value.ntp
    if (!d) return
    if (v) {
      d.tcp_fast_open = false
      d.tcp_multi_path = false
    } else {
      delete d.tcp_fast_open
      delete d.tcp_multi_path
    }
  }
})

const optUDP = computed({
  get: () => appConfig.value.ntp?.udp_fragment != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.udp_fragment = true : delete d.udp_fragment }
})

const optCT = computed({
  get: () => appConfig.value.ntp?.connect_timeout != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; v ? d.connect_timeout = '5s' : delete d.connect_timeout }
})

const optDR = computed({
  get: () => appConfig.value.ntp?.domain_resolver != undefined,
  set: (v: boolean) => { const d = appConfig.value.ntp; if (!d) return; d.domain_resolver = v ? dialDnsTags.value[0] ?? '' : undefined }
})

const bindNoPort = computed({
  get: () => appConfig.value.ntp?.bind_address_no_port ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.bind_address_no_port = v }
})

const reuseAddr = computed({
  get: () => appConfig.value.ntp?.reuse_addr ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.reuse_addr = v }
})

const tcpFastOpen = computed({
  get: () => appConfig.value.ntp?.tcp_fast_open ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.tcp_fast_open = v }
})

const tcpMultiPath = computed({
  get: () => appConfig.value.ntp?.tcp_multi_path ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.tcp_multi_path = v }
})

const udpFragment = computed({
  get: () => appConfig.value.ntp?.udp_fragment ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.udp_fragment = v }
})

const disableTcpKeepAlive = computed({
  get: () => appConfig.value.ntp?.disable_tcp_keep_alive ?? false,
  set: (v: boolean) => { if (appConfig.value.ntp) appConfig.value.ntp.disable_tcp_keep_alive = v }
})

/* ---------------- experimental ---------------- */
const enableCacheFile = computed({
  get() { return appConfig.value.experimental.cache_file?.enabled ?? false },
  set(v: boolean) {
    if (v) {
      appConfig.value.experimental.cache_file = { enabled: true }
    } else { delete appConfig.value.experimental.cache_file }
  }
})

const storeFakeip = computed({
  get: () => appConfig.value.experimental.cache_file?.store_fakeip ?? false,
  set: (v: boolean) => { if (appConfig.value.experimental.cache_file) appConfig.value.experimental.cache_file.store_fakeip = v }
})

const enableClashApi = computed({
  get() { return appConfig.value.experimental.clash_api != undefined },
  set(v: boolean) { appConfig.value.experimental.clash_api = v ? { external_controller: '127.0.0.1:9090' } : undefined }
})

const extUiDownloadDetour = computed({
  get: () => appConfig.value.experimental.clash_api?.external_ui_download_detour ?? '',
  set: (v: string) => {
    const c = appConfig.value.experimental.clash_api
    if (!c) return
    if (v) c.external_ui_download_detour = v
    else delete c.external_ui_download_detour
  }
})

const allowPrivateNetwork = computed({
  get: () => appConfig.value.experimental.clash_api?.access_control_allow_private_network ?? false,
  set: (v: boolean) => { if (appConfig.value.experimental.clash_api) appConfig.value.experimental.clash_api.access_control_allow_private_network = v }
})

const enableV2rayApi = computed({
  get() { return appConfig.value.experimental.v2ray_api != undefined },
  set(v: boolean) { appConfig.value.experimental.v2ray_api = v ? { listen: '127.0.0.1:8080', stats: { enabled: false, inbounds: [], outbounds: [], users: [] } } : undefined }
})

const origin = computed({
  get() {
    return appConfig.value.experimental.clash_api?.access_control_allow_origin &&
      appConfig.value.experimental.clash_api.access_control_allow_origin.length > 0 ? appConfig.value.experimental.clash_api.access_control_allow_origin.join(',') : ''
  },
  set(v: string) {
    if (appConfig.value.experimental.clash_api?.access_control_allow_origin)
      appConfig.value.experimental.clash_api.access_control_allow_origin = v.length > 0 ? v.split(',') : undefined
  }
})
</script>

<style scoped>
.panel { padding: 0; overflow: hidden; }
.panel-head {
  display: flex; align-items: center; gap: 10px;
  padding: 15px 18px;
  color: var(--text); font-size: 14px; font-weight: 700;
}
.panel-body { padding: 16px 18px 18px; border-top: 1px solid var(--line); }
.sub-label {
  font-size: 12px; font-weight: 700; color: var(--text-3);
  letter-spacing: .04em; text-transform: uppercase;
  margin: 16px 0 10px;
}
.switch-row { display: flex; align-items: center; gap: 24px; margin-top: 16px; flex-wrap: wrap; }
</style>
