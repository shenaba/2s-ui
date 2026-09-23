<template>
  <!-- fa keeps the Persian calendar picker; other locales use the native control -->
  <div v-if="isFa">
    <div class="input mono dt-display" :id="elId">
      <Ico name="basics" :size="15" style="color: var(--text-3); flex: none;" />
      {{ display }}
    </div>
    <DatePicker
      :model-value="pickerValue"
      @update:model-value="onPick"
      locale="fa"
      :element="elId"
      compact-time
      type="datetime"
    />
    <button v-if="modelValue > 0" type="button" class="dt-clear" @click="$emit('update:modelValue', 0)">
      {{ emptyText }}
    </button>
  </div>
  <div v-else style="display: flex; gap: 8px;">
    <input
      class="input mono"
      type="datetime-local"
      :value="localValue"
      @change="onNative"
    />
    <Btn
      v-if="modelValue > 0"
      sm
      style="height: 40px; flex: none;"
      :title="emptyText"
      @click="$emit('update:modelValue', 0)"
    ><Ico name="close" :size="14" /></Btn>
  </div>
</template>

<script lang="ts" setup>
import { computed, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Ico from './Ico.vue'
import Btn from './Btn.vue'

// modelValue: epoch seconds; 0 = no date. emptyLabel says what that means for the
// field -- "unlimited" by default, which is right for an expiry but not for every
// date: an empty next reset is computed from the cycle
const props = defineProps<{ modelValue: number; emptyLabel?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: number] }>()

const { t, locale } = useI18n({ useScope: 'global' })
const isFa = computed(() => String(locale.value) === 'fa')
const elId = `dt${useId().replace(/[:-]/g, '')}`
const emptyText = computed(() => props.emptyLabel ?? t('unlimited'))

const display = computed(() => {
  if (!props.modelValue) return emptyText.value
  return new Date(props.modelValue * 1000).toLocaleString('fa-IR')
})
const pickerValue = computed(() => (props.modelValue ? new Date(props.modelValue * 1000) : new Date()))
const onPick = (v: string | Date) => {
  const d = new Date(v)
  if (!isNaN(d.getTime())) emit('update:modelValue', Math.floor(d.getTime() / 1000))
}

// native datetime-local needs "YYYY-MM-DDTHH:mm" in local time
const localValue = computed(() => {
  if (!props.modelValue) return ''
  const d = new Date(props.modelValue * 1000)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}`
})
const onNative = (e: Event) => {
  const v = (e.target as HTMLInputElement).value
  emit('update:modelValue', v ? Math.floor(new Date(v).getTime() / 1000) : 0)
}
</script>

<style scoped>
.dt-display {
  display: flex; align-items: center; gap: 8px;
  cursor: pointer; font-size: 13px;
}
.dt-clear {
  border: none; background: none; cursor: pointer;
  font-family: var(--font-ui); font-size: 11.5px; font-weight: 600;
  color: var(--brand); padding: 4px 2px;
}
</style>

<style>
/* Persian picker re-skin to design tokens */
.vpd-content,
.vpd-addon-list,
.vpd-addon-list-item {
  background-color: var(--surface-2) !important;
  border-color: var(--line) !important;
  color: var(--text) !important;
}
.vpd-addon-list-item.vpd-selected,
.vpd-addon-list-item:hover {
  background-color: var(--brand) !important;
  color: #fff !important;
}
.vpd-close-addon { color: var(--text) !important; background-color: transparent; }
.vpd-controls { overflow-x: hidden; }
.vpd-month-label { width: auto; }
.vpd-actions { background-color: var(--surface-2) !important; }
.vpd-actions button { color: var(--brand) !important; }
.vpd-actions button:hover { background-color: transparent; }
.vpd-day-effect { background-color: var(--brand) !important; }
.vpd-time .vpd-time-h .vpd-counter-item,
.vpd-time .vpd-time-m .vpd-counter-item { vertical-align: top; }
.vpd-wrapper[data-type=datetime].vpd-compact-time .vpd-time { border-top: 0; }
</style>
