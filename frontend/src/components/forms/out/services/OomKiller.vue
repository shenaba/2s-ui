<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">OOM Killer</SectionLabel>
    <div class="grid2">
      <Field :label="$t('types.oomKiller.memoryLimit')">
        <input class="input mono" v-model="memoryLimit" placeholder="1gb" />
      </Field>
      <Field :label="$t('types.oomKiller.safetyMargin')">
        <input class="input mono" v-model="safetyMargin" placeholder="128mb" />
      </Field>
    </div>
    <div class="grid2">
      <Field :label="$t('types.oomKiller.minInterval')">
        <input class="input mono" v-model="minInterval" placeholder="10s" />
      </Field>
      <Field :label="$t('types.oomKiller.maxInterval')">
        <input class="input mono" v-model="maxInterval" placeholder="1m" />
      </Field>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'

const props = defineProps<{ data: any }>()

// Every field is optional and cleared rather than stored empty: sing-box
// rejects "" for a byte size or a duration, so a blank box has to leave none.
const bind = (key: string) => computed({
  get: () => props.data?.[key] ?? '',
  set: (v: string) => {
    const trimmed = (v ?? '').trim()
    if (trimmed) props.data[key] = trimmed
    else delete props.data[key]
  },
})

const memoryLimit = bind('memory_limit')
const safetyMargin = bind('safety_margin')
const minInterval = bind('min_interval')
const maxInterval = bind('max_interval')
</script>
