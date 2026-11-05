<template>
  <div class="km-tile">
    <div class="km-tile-head">
      <span class="km-tile-label">{{ label }}</span>
      <div
        class="km-tile-icon"
        :style="{
          color: accent,
          background: `color-mix(in srgb, ${accent} 14%, transparent)`,
        }"
      >
        <Ico :name="icon" :size="16" />
      </div>
    </div>
    <div class="km-tile-value mono">{{ value }}</div>
    <div v-if="sub" class="km-tile-sub mono">{{ sub }}</div>
  </div>
</template>

<script lang="ts" setup>
import Ico from '@/components/ui/Ico.vue'

defineProps<{
  icon: string
  label: string
  value: string
  accent: string
  sub?: string
}>()
</script>

<style scoped>
.km-tile {
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 12px 14px;
  background: var(--surface-2);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
  transition: all 0.15s var(--ease);
  min-width: 0;
}
.km-tile:hover {
  background: var(--surface-3);
  border-color: var(--line-2);
}
.km-tile-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-width: 0;
  margin-bottom: 6px;
}
.km-tile-icon {
  width: 28px;
  height: 28px;
  border-radius: 7px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
}
.km-tile-label {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.km-tile-value {
  font-size: 14px;
  font-weight: 700;
  line-height: 1.2;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text);
}
.km-tile-sub {
  font-size: 11px;
  color: var(--text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  margin-top: 4px;
}
</style>
