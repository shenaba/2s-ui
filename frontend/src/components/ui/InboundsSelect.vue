<template>
  <Pop align="start" :width="panelW || undefined">
    <template #trigger="{ toggle, open }">
      <button
        ref="triggerEl"
        type="button"
        class="input ib-trigger"
        role="combobox"
        :aria-expanded="open"
        @click="onTrigger(toggle)"
      >
        <span class="ib-summary" :class="{ placeholder: modelValue.length === 0 }">{{ summary }}</span>
        <Ico name="chevronDown" :size="15" class="ib-caret" :style="open ? 'transform: rotate(180deg)' : ''" />
      </button>
    </template>

    <div class="ib-head">
      <span class="ib-count">{{ modelValue.length }} / {{ items.length }}</span>
      <div style="flex: 1;" />
      <button type="button" class="ib-all" @click="selectAll">{{ $t('all') }}</button>
    </div>
    <div class="ib-list hide-scroll">
      <label
        v-for="it in items"
        :key="it.id"
        class="inb-item"
        :class="{ on: isSel(it.id) }"
        @click.prevent="toggle(it.id)"
      >
        <span class="inb-dot" :class="{ online: it.online }" />
        <div style="flex: 1; min-width: 0;">
          <div class="ib-tag">{{ it.tag }}</div>
          <div class="mono ib-meta">{{ it.type }} · :{{ it.port }}</div>
        </div>
        <span v-if="it.off" class="chip ib-off" :title="$t('in.offHint')">{{ $t('ui.off') }}</span>
        <span v-if="it.node" class="ib-node">{{ it.node }}</span>
        <Check :checked="isSel(it.id)" />
      </label>
      <div v-if="items.length === 0" class="ib-empty">{{ $t('ui.selectHint') }}</div>
    </div>
  </Pop>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Pop from './Pop.vue'
import Ico from './Ico.vue'
import Check from './Check.vue'

// off: the inbound is switched off -- still assignable, so the client is on it
// once it comes back, but serving nothing and giving no link until then.
type Item = { id: number; tag: string; type: string; port: string | number; online: boolean; node?: string; off?: boolean }

const props = defineProps<{
  modelValue: number[]
  items: Item[]
}>()
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()

const { t } = useI18n({ useScope: 'global' })

const triggerEl = ref<HTMLElement>()
const panelW = ref(0)
// 展开前量一下触发器宽度，让浮层面板与之同宽
const onTrigger = (toggle: () => void) => {
  panelW.value = triggerEl.value?.offsetWidth ?? 0
  toggle()
}

const isSel = (id: number) => props.modelValue.includes(id)
const toggle = (id: number) => {
  emit('update:modelValue', isSel(id)
    ? props.modelValue.filter((x) => x !== id)
    : [...props.modelValue, id].sort())
}
const selectAll = () => {
  emit('update:modelValue', props.items.map((i) => i.id).sort())
}

// 触发器摘要：优先显示已选入站的 tag；拿不到 tag（如数据未加载）时回退为「已选 N 项」
const summary = computed(() => {
  const n = props.modelValue.length
  if (n === 0) return t('ui.selectHint')
  const tags = props.items.filter((i) => props.modelValue.includes(i.id)).map((i) => i.tag)
  if (tags.length === 0) return t('ui.selected', { n })
  const extra = n - tags.length
  return extra > 0 ? `${tags.join(', ')} +${extra}` : tags.join(', ')
})
</script>

<style scoped>
.ib-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  text-align: start;
  cursor: pointer;
}
.ib-summary {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ib-summary.placeholder { color: var(--text-3); }
.ib-caret { flex: none; color: var(--text-3); transition: transform .18s var(--ease); }

.ib-head {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 2px 6px 8px;
}
.ib-count { font-size: 11.5px; color: var(--text-3); font-weight: 600; }
.ib-all {
  border: none;
  background: transparent;
  cursor: pointer;
  font-family: var(--font-ui);
  font-size: 12px;
  font-weight: 700;
  color: var(--brand);
  padding: 4px 7px;
  border-radius: 7px;
}
.ib-all:hover { background: var(--brand-soft); }

.ib-list {
  display: flex;
  flex-direction: column;
  gap: 7px;
  max-height: 300px;
  overflow-y: auto;
}
.inb-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 12px;
  border-radius: 11px;
  border: 1px solid var(--line);
  background: transparent;
  cursor: pointer;
  transition: all .15s var(--ease);
}
.inb-item:hover { background: var(--surface-3); }
.inb-item.on {
  background: var(--brand-soft);
  border-color: color-mix(in srgb, var(--brand) 30%, transparent);
}
.inb-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-3); flex: none; }
.inb-dot.online { background: var(--emerald); }
.ib-tag { font-weight: 700; font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ib-meta { font-size: 11.5px; color: var(--text-3); }
.ib-node {
  flex: none; font-size: 11px; font-weight: 600;
  color: var(--brand); background: var(--brand-soft);
  padding: 2px 7px; border-radius: 6px; white-space: nowrap;
}
.ib-off { flex: none; height: 18px; font-size: 10.5px; padding: 0 6px; }
.ib-empty { padding: 16px; text-align: center; color: var(--text-3); font-size: 12.5px; }
</style>
