<template>
  <div class="grid2">
    <Field :label="$t('types.hy.obfsType')" :mb="0">
      <Select v-model="obfsType">
        <option v-for="o in obfsTypes" :key="o" :value="o">{{ o }}</option>
      </Select>
    </Field>
    <Field :label="$t('types.hy.obfs')" :mb="0">
      <input class="input mono" v-model="data.obfs.password" />
    </Field>
    <!-- Gecko pads packets to a size in this range. sing-box flattens the two
         into the obfs object rather than nesting them, which is why they sit
         beside type and password rather than under a key of their own. -->
    <template v-if="obfsType === 'gecko'">
      <Field :label="$t('types.hy.minPacketSize')" :mb="0">
        <input
          class="input mono" type="number" min="0"
          :value="data.obfs.min_packet_size ?? 0"
          @input="setSize('min_packet_size', $event)"
        />
      </Field>
      <Field :label="$t('types.hy.maxPacketSize')" :mb="0">
        <input
          class="input mono" type="number" min="0"
          :value="data.obfs.max_packet_size ?? 0"
          @input="setSize('max_packet_size', $event)"
        />
      </Field>
    </template>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'

const props = defineProps<{ data: any }>()

const obfsTypes = ['salamander', 'gecko']

// Leaving gecko drops the two sizes with it. sing-box ignores them under
// salamander rather than refusing them, so this is about not storing a key that
// means nothing where it sits -- and not having it reappear, stale, if the type
// is switched back.
const obfsType = computed({
  get: (): string => props.data.obfs?.type ?? 'salamander',
  set: (v: string) => {
    props.data.obfs.type = v
    if (v !== 'gecko') {
      delete props.data.obfs.min_packet_size
      delete props.data.obfs.max_packet_size
    }
  },
})

// Both are ints in sing-box, and json.Unmarshal refuses a fraction rather than
// truncating it, so anything that is not a positive whole number leaves no key.
const setSize = (key: string, event: Event) => {
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isInteger(value) && value > 0) props.data.obfs[key] = value
  else delete props.data.obfs[key]
}
</script>
