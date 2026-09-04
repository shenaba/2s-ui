<template>
  <SectionLabel style="margin-bottom: 12px;">{{ $t('types.hy.realm') }}</SectionLabel>
  <MHint v-if="incomplete" style="margin-bottom: 12px;">{{ $t('types.hy.realmHint') }}</MHint>
  <div class="grid2">
    <Field :label="$t('types.hy.realmServerUrl')" :mb="0">
      <input class="input mono" placeholder="https://realm.example.com" v-model="data.realm.server_url" />
    </Field>
    <Field :label="$t('types.hy.realmId')" :mb="0">
      <input class="input mono" v-model="data.realm.realm_id" />
    </Field>
    <Field :label="$t('types.hy.realmToken')" :mb="0">
      <input class="input mono" autocomplete="off" v-model="data.realm.token" />
    </Field>
    <Field :label="$t('types.hy.realmIpVersion')" :hint="$t('types.hy.realmIpVersionHint')" :mb="0">
      <Select v-model="ipVersion">
        <option v-for="v in ipVersions" :key="v.value" :value="v.value">{{ v.title }}</option>
      </Select>
    </Field>
  </div>
  <Field :label="$t('types.hy.stunServers') + ' ' + $t('commaSeparated')" style="margin-top: 12px;">
    <input class="input mono" placeholder="stun.l.google.com:19302" v-model="stunServers" />
  </Field>
  <!-- Port mapping is refused outright over IPv6 ("port mapping requires
       IPv4"), so the row goes away with the choice rather than offering a
       toggle that cannot be saved. -->
  <template v-if="ipVersion !== 6">
    <div style="margin-bottom: 15px;">
      <SwitchLabel v-model="portMapping" :label="$t('types.hy.portMapping')" />
    </div>
    <div v-if="portMapping" class="grid2">
      <Field :label="$t('types.hy.portMappingTimeout')" :mb="0">
        <input class="input mono" placeholder="5s" v-model="portMappingTimeout" />
      </Field>
      <Field :label="$t('types.hy.portMappingLifetime')" :mb="0">
        <input class="input mono" placeholder="2m" v-model="portMappingLifetime" />
      </Field>
    </div>
  </template>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import MHint from '@/components/ui/MHint.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'

const props = defineProps<{ data: any }>()

const ipVersions = [
  { title: 'Auto', value: 0 },
  { title: '4', value: 4 },
  { title: '6', value: 6 },
]

// realm.NewServer refuses all three by name -- "control server URL is
// required", "realm ID is required", "at least one STUN server is required" --
// and it runs while the box is being built, so an unfinished realm stops the
// core rather than degrading. Say so here instead of leaving it to the log,
// which the panel then repeats every five seconds.
const incomplete = computed(
  () => !props.data.realm?.server_url || !props.data.realm?.realm_id || !props.data.realm?.stun_servers?.length,
)

const ipVersion = computed({
  get: (): number => props.data.realm.ip_version ?? 0,
  set: (v: number) => {
    props.data.realm.ip_version = v > 0 ? v : undefined
    if (v === 6) delete props.data.realm.port_mapping
  },
})

const stunServers = computed({
  get: (): string => props.data.realm.stun_servers?.join(',') ?? '',
  set: (v: string) => {
    const parts = v.split(',').map((s) => s.trim()).filter((s) => s.length > 0)
    props.data.realm.stun_servers = parts.length > 0 ? parts : undefined
  },
})

// Switching it on writes only the flag sing-box reads, so the two durations
// stay absent until they are typed -- it rejects "" for either.
const portMapping = computed({
  get: (): boolean => props.data.realm.port_mapping?.enabled === true,
  set: (v: boolean) => {
    if (v) props.data.realm.port_mapping = { enabled: true }
    else delete props.data.realm.port_mapping
  },
})

const duration = (key: string) =>
  computed({
    get: (): string => props.data.realm.port_mapping?.[key] ?? '',
    set: (v: string) => {
      const trimmed = (v ?? '').trim()
      if (trimmed) props.data.realm.port_mapping[key] = trimmed
      else delete props.data.realm.port_mapping[key]
    },
  })

const portMappingTimeout = duration('timeout')
const portMappingLifetime = duration('lifetime')
</script>
