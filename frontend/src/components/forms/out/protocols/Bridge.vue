<template>
  <div>
    <SectionLabel v-if="direction != 'out_json'" style="margin-bottom: 12px;">Bridge</SectionLabel>
    <div class="grid2">
      <Field :label="$t('types.bridge.interface')">
        <input class="input mono" v-model="iface" />
      </Field>
      <Field :label="$t('types.bridge.bridgeName')">
        <input class="input mono" v-model="bridgeName" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.bridge.tableIndex')">
        <input class="input mono" type="number" min="0" v-model.number="tableIndex" />
      </Field>
      <Field :label="$t('types.bridge.ruleIndex')">
        <input class="input mono" type="number" min="0" v-model.number="ruleIndex" />
      </Field>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'

const props = defineProps<{ data: any; direction?: string }>()

// Every field is optional and dropped when empty: sing-box picks its own
// interface and indices when they are absent, but rejects an empty string.
const iface = computed({
  get: () => props.data.interface ?? '',
  set: (v: string) => { props.data.interface = v.length > 0 ? v : undefined },
})
const bridgeName = computed({
  get: () => props.data.bridge_name ?? '',
  set: (v: string) => { props.data.bridge_name = v.length > 0 ? v : undefined },
})
const tableIndex = computed({
  get: (): number => props.data.iproute2_table_index ?? 0,
  set: (v: number) => { props.data.iproute2_table_index = v > 0 ? v : undefined },
})
const ruleIndex = computed({
  get: (): number => props.data.iproute2_rule_index ?? 0,
  set: (v: number) => { props.data.iproute2_rule_index = v > 0 ? v : undefined },
})
</script>
