<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">API</SectionLabel>
    <Field :label="$t('types.api.secret')">
      <input class="input mono" type="password" v-model="secret" />
    </Field>
    <Field :label="$t('types.api.allowOrigin') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="allowOrigin" />
    </Field>
    <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="allowPrivateNetwork" :label="$t('types.api.allowPrivateNetwork')" />
      <SwitchLabel v-model="dashboardEnabled" :label="$t('types.api.dashboard')" />
    </div>

    <template v-if="dashboardEnabled">
      <Field :label="$t('types.api.dashboardPath')">
        <input class="input mono" v-model="dashboardPath" />
      </Field>
      <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
        <SwitchLabel v-model="dashboardDownload" :label="$t('types.api.dashboardDownload')" />
      </div>
      <template v-if="dashboardDownload">
        <Field :label="$t('types.api.downloadUrl')">
          <input class="input mono" style="font-size: 12px;" v-model="dashboardDownloadUrl" />
        </Field>
        <div class="grid2">
          <Field :label="$t('httpClient.title')">
            <Select v-model="dashboardHttpClient">
              <option value="">{{ $t('none') }}</option>
              <option v-for="c in httpClients" :key="c" :value="c">{{ c }}</option>
            </Select>
          </Field>
          <Field :label="$t('ruleset.interval') + ' (' + $t('date.d') + ')'">
            <input class="input mono" type="number" min="0" v-model.number="dashboardInterval" />
          </Field>
        </div>
      </template>
    </template>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import { httpClientTags, refTag } from '@/plugins/httpClient'

const props = defineProps<{ data: any }>()

const httpClients = computed((): string[] => httpClientTags())

const secret = computed({
  get: () => props.data.secret ?? '',
  set: (v: string) => { props.data.secret = v.length > 0 ? v : undefined },
})
const allowOrigin = computed({
  get: () => props.data.access_control_allow_origin?.join(',') ?? '',
  set: (v: string) => {
    if (v.endsWith(',')) return
    props.data.access_control_allow_origin = v.length > 0 ? v.split(',') : undefined
  },
})
const allowPrivateNetwork = computed({
  get: (): boolean => props.data.access_control_allow_private_network ?? false,
  set: (v: boolean) => {
    if (v) props.data.access_control_allow_private_network = true
    else delete props.data.access_control_allow_private_network
  },
})

// `dashboard` accepts a bool, a path string or the full object. Anything past
// enabled and path needs the object form, so the short ones are widened only
// when a field that requires it is filled in.
function asObject(): any {
  const dashboard = props.data?.dashboard
  if (dashboard && typeof dashboard === 'object') return dashboard
  const widened: any = { enabled: true }
  if (typeof dashboard === 'string' && dashboard.length > 0) widened.path = dashboard
  props.data.dashboard = widened
  return widened
}

const dashboardEnabled = computed({
  get: (): boolean => {
    const d = props.data?.dashboard
    if (typeof d === 'boolean') return d
    if (typeof d === 'string') return d.length > 0
    return d?.enabled === true
  },
  set: (v: boolean) => {
    if (!v) delete props.data.dashboard
    else props.data.dashboard = true
  },
})
const dashboardPath = computed({
  get: (): string => {
    const d = props.data?.dashboard
    if (typeof d === 'string') return d
    if (d && typeof d === 'object') return d.path ?? ''
    return ''
  },
  set: (v: string) => {
    const d = props.data?.dashboard
    if (d && typeof d === 'object') {
      if (v) d.path = v
      else delete d.path
      return
    }
    props.data.dashboard = v ? v : true
  },
})
// Turning this off drops the download settings and lets the value fall back to
// its short form, so a dashboard that only needs a path does not keep an object.
const dashboardDownload = computed({
  get: (): boolean => {
    const d = props.data?.dashboard
    if (!d || typeof d !== 'object') return false
    return d.download_url != undefined || d.http_client != undefined || d.update_interval != undefined
  },
  set: (v: boolean) => {
    if (v) { asObject().download_url = ''; return }
    const d = props.data?.dashboard
    if (!d || typeof d !== 'object') return
    props.data.dashboard = d.path ? d.path : true
  },
})
const dashboardDownloadUrl = computed({
  get: () => props.data?.dashboard?.download_url ?? '',
  set: (v: string) => {
    const d = asObject()
    if (v) d.download_url = v
    else delete d.download_url
  },
})
const dashboardHttpClient = computed({
  get: () => refTag(props.data?.dashboard?.http_client) ?? '',
  set: (v: string) => {
    const d = asObject()
    if (v) d.http_client = v
    else delete d.http_client
  },
})
const dashboardInterval = computed({
  get: (): number => {
    const interval = props.data?.dashboard?.update_interval
    return interval ? parseInt(String(interval).replace('d', '')) : 0
  },
  set: (v: number) => {
    const d = asObject()
    if (v > 0) d.update_interval = v + 'd'
    else delete d.update_interval
  },
})
</script>
