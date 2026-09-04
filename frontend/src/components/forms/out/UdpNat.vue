<template>
  <div>
    <SwitchLabel v-model="show" :label="$t('udpNat.title')" />
    <div v-if="show" class="grid2" style="margin-top: 12px;">
      <Field :label="$t('udpNat.mapping')" :mb="0">
        <Select v-model="mapping">
          <option v-for="b in behaviors" :key="b" :value="b">{{ b }}</option>
        </Select>
      </Field>
      <Field :label="$t('udpNat.filtering')" :mb="0">
        <Select v-model="filtering">
          <option v-for="b in behaviors" :key="b" :value="b">{{ b }}</option>
        </Select>
      </Field>
      <Field :label="$t('udpNat.max')" :mb="0">
        <input class="input mono" type="number" min="0" :value="data.udp_nat_max ?? 0" @input="setMax" />
      </Field>
    </div>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import Field from '@/components/ui/Field.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

// The NAT mapping and filtering behaviours 1.14 made configurable, shared by
// everything that owns a UDP NAT: tun, redirect, tproxy and the WireGuard
// endpoint. endpoint_independent is sing-box's default and is what an empty
// value means, so selecting it writes no key.
const behaviors = ['endpoint_independent', 'address_dependent', 'address_and_port_dependent']

const KEYS = ['udp_mapping', 'udp_filtering', 'udp_nat_max']

const props = defineProps<{ data: any }>()

// Revealing the group writes nothing, so clearing the last field does not make
// the section collapse under the operator mid-edit.
const show = ref(false)
watch(
  () => props.data,
  () => { show.value = KEYS.some((k) => props.data?.[k] != undefined) },
  { immediate: true },
)
watch(show, (v) => { if (!v) KEYS.forEach((k) => delete props.data[k]) })

const behavior = (key: string) =>
  computed({
    get: (): string => props.data?.[key] ?? 'endpoint_independent',
    set: (v: string) => {
      if (v && v !== 'endpoint_independent') props.data[key] = v
      else delete props.data[key]
    },
  })
const mapping = behavior('udp_mapping')
const filtering = behavior('udp_filtering')

// uint32 in sing-box, so a fraction is refused rather than truncated and would
// take the whole config down over one field.
const setMax = (event: Event) => {
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isInteger(value) && value > 0) props.data.udp_nat_max = value
  else delete props.data.udp_nat_max
}
</script>
