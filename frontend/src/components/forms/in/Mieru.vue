<template>
  <div>
    <SectionLabel v-if="direction != 'out_json'" style="margin-bottom: 12px;">Mieru</SectionLabel>
    <!-- Inbound -->
    <template v-if="direction == 'in'">
      <Field :label="$t('types.mieru.transport')">
        <Select class="mono" v-model="transport">
          <option v-for="t in ['TCP', 'UDP']" :key="t" :value="t">{{ t }}</option>
        </Select>
      </Field>
      <MHint>{{ $t('types.mieru.transportHint') }}</MHint>
      <Field :label="$t('types.mieru.trafficPattern')">
        <input class="input mono" v-model="trafficPattern" :placeholder="$t('none')" />
      </Field>
      <MHint>{{ $t('types.mieru.trafficPatternHint') }}</MHint>
      <div style="margin-bottom: 15px;">
        <SwitchLabel v-model="userHint" :label="$t('types.mieru.userHint')" />
      </div>
    </template>
    <!-- client side (out_json) -->
    <template v-else>
      <Field :label="$t('types.mieru.multiplexing')">
        <Select class="mono" v-model="multiplexing">
          <option value="">{{ $t('types.mieru.multiplexingDefault') }}</option>
          <option v-for="m in multiplexingLevels" :key="m" :value="m">{{ m }}</option>
        </Select>
      </Field>
    </template>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import MHint from '@/components/ui/MHint.vue'

const props = defineProps<{ data: any; direction?: string }>()

// mieru's own level names, as its client and mihomo spell them.
const multiplexingLevels = ['MULTIPLEXING_OFF', 'MULTIPLEXING_LOW', 'MULTIPLEXING_MIDDLE', 'MULTIPLEXING_HIGH']

// The core takes the name in either case, so an inbound written through the
// API as "udp" is a UDP one and has to show as that.
const transport = computed({
  get: (): string => (String(props.data.transport ?? '').toUpperCase() == 'UDP' ? 'UDP' : 'TCP'),
  set: (v: string) => { props.data.transport = v },
})

// Pasted from mieru's own tooling, so stray whitespace is likely; an empty
// field means none and is left out of the config.
const trafficPattern = computed({
  get: (): string => props.data.traffic_pattern ?? '',
  set: (v: string) => {
    const trimmed = v.trim()
    props.data.traffic_pattern = trimmed.length > 0 ? trimmed : undefined
  },
})

const userHint = computed({
  get: (): boolean => props.data.user_hint_is_mandatory ?? false,
  set: (v: boolean) => { props.data.user_hint_is_mandatory = v ? true : undefined },
})

const multiplexing = computed({
  get: (): string => props.data.multiplexing ?? '',
  set: (v: string) => { props.data.multiplexing = v.length > 0 ? v : undefined },
})
</script>
