<template>
  <div>
    <SectionLabel v-if="direction != 'out_json'" style="margin-bottom: 12px;">Snell</SectionLabel>
    <div class="grid2">
      <Field :label="$t('types.snell.version')">
        <Select v-model="version">
          <option v-for="v in versions" :key="v.value" :value="v.value">{{ v.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('types.snell.psk')">
        <KeyInput v-if="direction == 'in'" v-model="data.psk" :title="$t('actions.generate')" @regenerate="generatePsk" />
        <input v-else class="input mono" v-model="data.psk" />
      </Field>
    </div>
    <Field v-if="direction === 'out'" :label="$t('types.snell.userKey')">
      <input class="input mono" v-model="userKey" />
    </Field>

    <!-- The version decides which extras apply: v5 inbound / v4 outbound carry
         obfs, v6 carries a mode. -->
    <div v-if="obfsVersion" class="grid2">
      <Field :label="$t('types.snell.obfsMode')">
        <Select v-model="obfsMode">
          <option value="">{{ $t('none') }}</option>
          <option v-for="o in obfsModes" :key="o.value" :value="o.value">{{ o.title }}</option>
        </Select>
      </Field>
      <Field v-if="direction === 'out'" :label="$t('types.snell.obfsHost')">
        <input class="input mono" v-model="obfsHost" />
      </Field>
    </div>
    <Field v-else :label="$t('types.snell.mode')">
      <Select v-model="mode">
        <option value="">{{ $t('none') }}</option>
        <option v-for="m in modes" :key="m.value" :value="m.value">{{ m.title }}</option>
      </Select>
    </Field>
    <!-- The psk is shared by the whole listener and identifies nobody; the
         per-client key is what tells one client from another. sing-box picks
         psk-only or multi-user at start-up from whether the listener has any
         clients, so one with none silently reports no user for any connection
         -- which the panel can only show as a client that moved no traffic
         (#143). -->
    <MHint v-if="direction === 'in'">{{ $t('types.snell.userKeyHint') }}</MHint>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import KeyInput from '@/components/ui/KeyInput.vue'
import MHint from '@/components/ui/MHint.vue'
import RandomUtil from '@/plugins/randomUtil'

const props = defineProps<{ data: any; direction?: string }>()

const obfsModes = [
  { title: 'None', value: 'none' },
  { title: 'HTTP', value: 'http' },
  { title: 'TLS', value: 'tls' },
]
const modes = [
  { title: 'Default', value: 'default' },
  { title: 'Unshaped', value: 'unshaped' },
  { title: 'Unsafe Raw', value: 'unsafe-raw' },
]

// Inbounds support v5 and v6, outbounds v4 and v6.
const legacyVersion = computed((): number => (props.direction === 'in' ? 5 : 4))

const versions = computed(() => [
  { title: 'v' + legacyVersion.value, value: legacyVersion.value },
  { title: 'v6', value: 6 },
])

const obfsVersion = computed((): boolean => props.data.version === legacyVersion.value)

const version = computed({
  get: (): number => props.data.version,
  // The version selects which extra options apply, so the ones belonging to
  // the version being left are dropped rather than sent along unread.
  set: (v: number) => {
    props.data.version = v
    if (v === 6) {
      delete props.data.obfs_mode
      delete props.data.obfs_host
    } else {
      delete props.data.mode
    }
  },
})

// sing-box requires a psk of 12-255 bytes.
const generatePsk = () => { props.data.psk = RandomUtil.randomSeq(32) }

const userKey = computed({
  get: () => props.data.userkey ?? '',
  set: (v: string) => { props.data.userkey = v.length > 0 ? v : undefined },
})
const obfsMode = computed({
  get: () => props.data.obfs_mode ?? '',
  set: (v: string) => { props.data.obfs_mode = v.length > 0 ? v : undefined },
})
const obfsHost = computed({
  get: () => props.data.obfs_host ?? '',
  set: (v: string) => { props.data.obfs_host = v.length > 0 ? v : undefined },
})
const mode = computed({
  get: () => props.data.mode ?? '',
  set: (v: string) => { props.data.mode = v.length > 0 ? v : undefined },
})
</script>
