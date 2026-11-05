<template>
  <div class="card" style="display: flex; flex-direction: column; overflow: hidden;">
    <div :style="{ display: 'flex', alignItems: 'center', gap: '12px', padding: `12px ${headPad}px`, minHeight: '56px', borderBottom: '1px solid var(--line)', flexWrap: 'wrap' }">
      <div style="display: flex; align-items: center; gap: 12px; min-width: 0;">
        <div style="font-size: 14px; font-weight: 700; white-space: nowrap; flex: none;">{{ title }}</div>
        <slot name="sub">
          <div v-if="sub" style="font-size: 11.5px; color: var(--text-3); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ sub }}</div>
        </slot>
      </div>
      <div v-if="$slots.right" style="margin-inline-start: auto; display: flex; align-items: center; gap: 8px;">
        <slot name="right" />
      </div>
    </div>
    <div :style="{ padding: pad + 'px', flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center' }">
      <slot />
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  title: string
  sub?: string
  pad?: number
}>(), {
  pad: 20,
})

const headPad = computed(() => (props.pad === 0 ? 20 : props.pad))
</script>
