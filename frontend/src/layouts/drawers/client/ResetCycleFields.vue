<template>
  <div class="grid2" style="margin-bottom: 15px;">
    <Field :label="$t('client.resetCycle')" :mb="0">
      <Select v-model="mode">
        <option value="monthly">{{ $t('client.resetCycleMonthly') }}</option>
        <option value="days">{{ $t('client.resetCycleDays') }}</option>
      </Select>
    </Field>
    <Field
      :label="mode === 'monthly' ? $t('client.resetDayOfMonth') : $t('client.resetDays')"
      :hint="mode === 'monthly' ? $t('client.resetDayOfMonthHint') : ''"
      :mb="0"
    >
      <div style="display: flex; gap: 8px;">
        <input
          v-if="mode === 'monthly'"
          class="input mono"
          type="number"
          min="1"
          max="31"
          v-model.number="dayOfMonth"
        />
        <input v-else class="input mono" type="number" min="1" v-model.number="days" />
        <div class="input suffix-box">{{ mode === 'monthly' ? $t('date.dayOfMonth') : $t('date.d') }}</div>
      </div>
    </Field>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { coerceResetDayOfMonth, coerceResetDays } from '@/types/clients'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'

// The reset cycle of a client or of a batch: every N days, or a fixed day of the
// month, whichever of the two fields is set. Shared by the client drawer, bulk
// add and bulk edit; like the other form parts it edits the fields of `data` in
// place.
const props = defineProps<{ data: { resetDays?: number; resetDayOfMonth?: number } }>()
// A period typed into the days field, emitted before it is written: the client
// drawer moves the next reset it displays by the difference.
const emit = defineEmits<{ period: [to: number, from: number] }>()

const mode = computed<'days' | 'monthly'>({
  get: () => ((props.data.resetDayOfMonth ?? 0) > 0 ? 'monthly' : 'days'),
  set: (m) => {
    if (m === 'monthly') {
      // Today by default: "the same day every month, starting now" is the
      // usual way a plan is billed.
      props.data.resetDayOfMonth = props.data.resetDayOfMonth || new Date().getDate()
      props.data.resetDays = 0
    } else {
      props.data.resetDayOfMonth = 0
      props.data.resetDays = props.data.resetDays || 30
    }
  },
})

// Not bound to `data` directly: v-model.number hands back the raw string when
// parseFloat fails, so a cleared input is "" rather than 0, and the Go side
// rejects the whole request on an int field holding a string.
const dayOfMonth = computed({
  get: () => props.data.resetDayOfMonth ?? 1,
  set: (v: number | string | null) => {
    props.data.resetDayOfMonth = coerceResetDayOfMonth(v)
  },
})
const days = computed({
  get: () => props.data.resetDays ?? 1,
  set: (v: number | string | null) => {
    const n = coerceResetDays(v)
    emit('period', n, props.data.resetDays ?? 0)
    props.data.resetDays = n
  },
})
</script>
