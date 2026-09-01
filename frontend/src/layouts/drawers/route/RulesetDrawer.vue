<template>
  <MDrawer
    :open="open"
    icon="rules"
    :color="form.type === 'remote' ? 'var(--violet)' : 'var(--cyan)'"
    :title="isNew ? $t('ui.rsNew') : form.tag"
    :sub="$t('ui.rsSub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <Field :label="$t('objects.tag')">
      <input class="input mono" v-model="form.tag" placeholder="geosite-ads" />
    </Field>

    <!-- local / remote -->
    <Segmented v-model="kind" block :options="[['local', $t('ruleset.local')], ['remote', $t('ruleset.remote')]]" />

    <Field :label="$t('ruleset.format')">
      <Select v-model="form.format">
        <option value="source">source</option>
        <option value="binary">binary</option>
      </Select>
    </Field>

    <template v-if="form.type === 'local'">
      <Field :label="$t('transport.path')">
        <input class="input mono" v-model="form.path" placeholder="/etc/sing-box/rules.srs" />
      </Field>
    </template>
    <template v-else>
      <Field label="URL">
        <input class="input mono" style="font-size: 12px;" v-model="form.url" placeholder="https://…/geosite-ads.srs" />
      </Field>
      <!-- A shared client and a single outbound are alternatives: http_client
           holds one or the other, so picking either clears the other. -->
      <div class="grid2">
        <Field :label="$t('httpClient.title')">
          <Select v-model="httpClient">
            <option value="">{{ $t('ui.none') }}</option>
            <option v-for="c in httpClients" :key="c" :value="c">{{ c }}</option>
          </Select>
        </Field>
        <Field :label="$t('objects.outbound')">
          <Select v-model="downloadDetour">
            <option value="">{{ $t('ui.none') }}</option>
            <option v-for="o in outTags" :key="o" :value="o">{{ o }}</option>
          </Select>
        </Field>
      </div>
      <Field :label="$t('ruleset.interval') + ' (' + $t('date.d') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="updateIntervals" />
      </Field>
    </template>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import MDrawer from '@/components/ui/MDrawer.vue'
import Segmented from '@/components/ui/Segmented.vue'
import Field from '@/components/ui/Field.vue'
import RandomUtil from '@/plugins/randomUtil'
import { ruleset } from '@/types/rules'
import { downloadHttpClient, httpClientTags, refDetour, refTag } from '@/plugins/httpClient'

const props = defineProps<{
  open: boolean
  index: number
  data: string
  outTags: string[]
}>()
const emit = defineEmits<{ close: []; save: [data: ruleset] }>()

const isNew = computed(() => props.index === -1)

const form = ref<ruleset>(<ruleset>{})

function init() {
  if (props.index !== -1) {
    form.value = <ruleset>JSON.parse(props.data)
  } else {
    form.value = <ruleset>{ type: 'local', tag: 'rs-' + RandomUtil.randomSeq(3), format: 'binary' }
  }
}
watch(() => props.open, (v) => { if (v) init() })

// switching type clears the fields of the other kind (legacy updateType)
const kind = computed({
  get: () => form.value.type,
  set: (t) => {
    form.value.type = <'local' | 'remote'>t
    if (t === 'local') {
      delete form.value.url
      delete form.value.http_client
      delete form.value.update_interval
    } else {
      delete form.value.path
    }
  },
})

const httpClients = computed(() => httpClientTags())

// Naming a shared client replaces any inline options: http_client is either a
// tag or an options object, never both.
const httpClient = computed({
  get: () => refTag(form.value.http_client) ?? '',
  set: (v: string) => {
    if (v.length > 0) form.value.http_client = v
    else delete form.value.http_client
  },
})

// The inline form, carrying only the outbound to download over. An outbound
// that would dial exactly what no detour dials is dropped rather than written
// out -- sing-box rejects that as pointless (see downloadHttpClient).
const downloadDetour = computed({
  get: () => refDetour(form.value.http_client) ?? '',
  set: (v: string) => {
    const client = v.length > 0 ? downloadHttpClient(v) : undefined
    if (client) form.value.http_client = client
    else delete form.value.http_client
  },
})

const updateIntervals = computed({
  get: () => (form.value.update_interval !== undefined ? parseInt(form.value.update_interval.replace('d', '')) : 0),
  set: (v: number) => { form.value.update_interval = v > 0 ? v + 'd' : undefined },
})

function saveChanges() {
  emit('save', form.value)
}
</script>
