<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">Cloudflared</SectionLabel>
    <Field :label="$t('types.cloudflared.token')">
      <input class="input mono" style="font-size: 12px;" v-model="data.token" />
    </Field>
    <div class="grid2">
      <Field :label="$t('types.cloudflared.protocol')">
        <Select v-model="protocol">
          <option v-for="p in protocols" :key="p.value" :value="p.value">{{ p.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('types.cloudflared.haConnections')">
        <input class="input mono" type="number" min="0" v-model.number="haConnections" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.cloudflared.edgeIpVersion')">
        <Select v-model="edgeIpVersion">
          <option v-for="e in edgeIpVersions" :key="e.value" :value="e.value">{{ e.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('types.cloudflared.datagramVersion')">
        <Select v-model="datagramVersion">
          <option value="">{{ $t('none') }}</option>
          <option v-for="d in datagramVersions" :key="d.value" :value="d.value">{{ d.title }}</option>
        </Select>
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.cloudflared.region')">
        <input class="input mono" v-model="region" />
      </Field>
      <Field :label="$t('types.cloudflared.gracePeriod')">
        <input class="input mono" v-model="gracePeriod" placeholder="30s" />
      </Field>
    </div>
    <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="postQuantum" :label="$t('types.cloudflared.postQuantum')" />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

const props = defineProps<{ data: any }>()

const protocols = [
  { title: 'Auto', value: 'auto' },
  { title: 'QUIC', value: 'quic' },
  { title: 'HTTP/2', value: 'http2' },
  { title: 'h2mux', value: 'h2mux' },
]
const edgeIpVersions = [
  { title: 'Auto', value: 0 },
  { title: 'IPv4', value: 4 },
  { title: 'IPv6', value: 6 },
]
const datagramVersions = [
  { title: 'v2', value: 'v2' },
  { title: 'v3', value: 'v3' },
]

// Every optional field is dropped rather than stored empty, so an untouched
// form leaves sing-box on its own defaults.
const protocol = computed({
  get: () => props.data.protocol ?? 'auto',
  set: (v: string) => { props.data.protocol = v },
})
const haConnections = computed({
  get: (): number => props.data.ha_connections ?? 0,
  set: (v: number) => { props.data.ha_connections = v > 0 ? v : undefined },
})
const edgeIpVersion = computed({
  get: (): number => props.data.edge_ip_version ?? 0,
  set: (v: number) => { props.data.edge_ip_version = v > 0 ? v : undefined },
})
const datagramVersion = computed({
  get: () => props.data.datagram_version ?? '',
  set: (v: string) => { props.data.datagram_version = v.length > 0 ? v : undefined },
})
const region = computed({
  get: () => props.data.region ?? '',
  set: (v: string) => { props.data.region = v.length > 0 ? v : undefined },
})
const gracePeriod = computed({
  get: () => props.data.grace_period ?? '',
  set: (v: string) => { props.data.grace_period = v.trim().length > 0 ? v.trim() : undefined },
})
const postQuantum = computed({
  get: (): boolean => props.data.post_quantum ?? false,
  set: (v: boolean) => { if (v) props.data.post_quantum = true; else delete props.data.post_quantum },
})
</script>
