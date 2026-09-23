<template>
  <MDrawer
    :open="visible"
    icon="clients"
    color="var(--violet)"
    :title="$t('actions.addbulk')"
    :sub="$t('pages.clients')"
    :save-label="$t('actions.save')"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <Field :label="$t('count')">
      <input class="input mono" type="number" min="1" max="100" v-model.number="count" />
    </Field>

    <!-- name / desc pattern builders (legacy combobox: Order / Random tokens + free text) -->
    <Field v-for="fk in (['name', 'desc'] as const)" :key="fk" :label="$t('client.' + fk)">
      <div class="pat-box">
        <Chip
          v-for="(p, i) in bulkData[fk]"
          :key="fk + '-' + i"
          :color="typeof p === 'object' ? 'brand' : undefined"
          style="cursor: pointer; gap: 5px;"
          @click="bulkData[fk].splice(i, 1)"
        >{{ typeof p === 'object' ? p.title : p }} <Ico name="close" :size="11" /></Chip>
        <span v-if="bulkData[fk].length === 0" style="color: var(--text-3); font-size: 12.5px;">{{ $t('ui.none') }}</span>
      </div>
      <div class="pat-controls">
        <Btn sm @click="bulkData[fk].push({ ...patterns.order })"><Ico name="plus" :size="13" /> {{ $t('bulk.order') }}</Btn>
        <Btn sm @click="bulkData[fk].push({ ...patterns.random })"><Ico name="plus" :size="13" /> {{ $t('bulk.random') }}</Btn>
        <input
          class="input"
          style="flex: 1; min-width: 80px; height: 32px; font-size: 12.5px;"
          v-model="textInput[fk]"
          @keyup.enter="addText(fk)"
        />
      </div>
    </Field>

    <div class="grid2">
      <Field :label="$t('client.group')">
        <input class="input" v-model="bulkData.group" :list="dlId" />
        <datalist :id="dlId">
          <option v-for="g in groupItems" :key="g" :value="g" />
        </datalist>
      </Field>
      <Field :label="$t('stats.volume')">
        <div style="display: flex; gap: 8px;">
          <input class="input mono" type="number" min="0" v-model.number="bulkData.Volume" />
          <div class="input suffix-box">{{ $t('stats.GB') }}</div>
        </div>
      </Field>
    </div>

    <Field :label="$t('ui.ipLimit')" :hint="$t('ui.unlimitedHint')">
      <input class="input mono" type="number" min="0" v-model.number="bulkData.limitIp" />
    </Field>

    <div class="grid2" style="margin-bottom: 15px;">
      <SwitchLabel v-model="delayStart" :label="$t('client.delayStart')" />
      <SwitchLabel v-model="autoReset" :label="$t('client.autoReset')" />
    </div>

    <Field
      v-if="!(bulkData.delayStart && bulkData.planDays > 0)"
      :label="$t('date.expiry')"
    >
      <DateTimeInput v-model="bulkData.expiry" />
    </Field>

    <div v-if="bulkData.autoReset" class="grid2" style="margin-bottom: 15px;">
      <Field :label="$t('client.resetCycle')" :mb="0">
        <Select v-model="resetMode">
          <option value="monthly">{{ $t('client.resetCycleMonthly') }}</option>
          <option value="days">{{ $t('client.resetCycleDays') }}</option>
        </Select>
      </Field>
      <Field
        :label="resetMode === 'monthly' ? $t('client.resetDayOfMonth') : $t('client.resetDays')"
        :hint="resetMode === 'monthly' ? $t('client.resetDayOfMonthHint') : ''"
        :mb="0"
      >
        <div style="display: flex; gap: 8px;">
          <input
            v-if="resetMode === 'monthly'"
            class="input mono"
            type="number"
            min="1"
            max="31"
            v-model.number="resetDayOfMonth"
          />
          <input v-else class="input mono" type="number" min="1" v-model.number="resetDays" />
          <div class="input suffix-box">{{ resetMode === 'monthly' ? $t('date.dayOfMonth') : $t('date.d') }}</div>
        </div>
      </Field>
    </div>
    <Field v-if="bulkData.delayStart" :label="$t('client.validDays')" :hint="$t('client.validDaysHint')">
      <div style="display: flex; gap: 8px;">
        <input class="input mono" type="number" min="0" v-model.number="planDays" />
        <div class="input suffix-box">{{ $t('date.d') }}</div>
      </div>
    </Field>

    <Field :label="$t('client.inboundTags')">
      <div style="display: flex; flex-direction: column; gap: 8px;">
        <div style="display: flex;">
          <Btn sm variant="subtle" style="margin-inline-start: auto;" @click="setAllInbounds">{{ $t('all') }}</Btn>
        </div>
        <label
          v-for="it in inboundItems"
          :key="it.id"
          class="inb-item"
          :class="{ on: bulkData.clientInbounds.includes(it.id) }"
          @click.prevent="toggleInbound(it.id)"
        >
          <span class="inb-dot" :class="{ online: it.online }" />
          <div style="flex: 1; min-width: 0;">
            <div style="font-weight: 700; font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{{ it.tag }}</div>
            <div class="mono" style="font-size: 11.5px; color: var(--text-3);">{{ it.type }} · :{{ it.port }}</div>
          </div>
          <Check :checked="bulkData.clientInbounds.includes(it.id)" />
        </label>
      </div>
    </Field>
  </MDrawer>
</template>

<script lang="ts" setup>
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import RandomUtil from '@/plugins/randomUtil'
import { Client, coercePlanDays, coerceResetDayOfMonth, coerceResetDays, createClient, randomConfigs } from '@/types/clients'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import Check from '@/components/ui/Check.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import DateTimeInput from '@/components/ui/DateTimeInput.vue'
import Select from '@/components/ui/Select.vue'

const props = defineProps<{
  visible: boolean
  groups: string[]
  inboundTags: { title: string; value: number }[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })
const dlId = `dl${useId().replace(/[:-]/g, '')}`

type PatternPart = string | { title: string; value: string }

const count = ref(1)
const loading = ref(false)
const bulkData = ref({
  name: [] as PatternPart[],
  desc: [] as PatternPart[],
  group: '',
  clientInbounds: [] as number[],
  expiry: 0,
  Volume: 0,
  limitIp: 0,
  delayStart: false,
  autoReset: false,
  planDays: 0,
  resetDays: 0,
  resetDayOfMonth: 0,
})
const textInput = ref({ name: '', desc: '' })

// 开关走 computed 而不是裸 v-model:裸绑定时打开一个模式会留下值为 0 的一行,
// 后端当"没配置"跳过,这批客户静默地既不重置也不过期。两个开关各管各的列,
// 所以不需要跨开关联动
const autoReset = computed({
  get: () => bulkData.value.autoReset,
  set: (v: boolean) => {
    bulkData.value.autoReset = v
    if (!v) {
      bulkData.value.resetDays = 0
      bulkData.value.resetDayOfMonth = 0
    } else if (!bulkData.value.resetDays && !bulkData.value.resetDayOfMonth) {
      bulkData.value.resetDays = 30
    }
  },
})
const delayStart = computed({
  get: () => bulkData.value.delayStart,
  set: (v: boolean) => {
    bulkData.value.delayStart = v
    if (v && !bulkData.value.planDays) bulkData.value.planDays = 30
  },
})
const planDays = computed({
  get: () => bulkData.value.planDays,
  set: (v: number | string | null) => { bulkData.value.planDays = coercePlanDays(v) },
})

// 数字输入不能直接绑到 bulkData 上:v-model.number 在 parseFloat 失败时原样
// 返回字符串,清空输入框得到的是 "" 而不是 0,提交后 Go 端的 int 字段解析失败,
// 整批创建带着一条 json 错误中止
const resetDays = computed({
  get: () => bulkData.value.resetDays,
  set: (v: number | string | null) => { bulkData.value.resetDays = coerceResetDays(v) },
})
const resetDayOfMonth = computed({
  get: () => bulkData.value.resetDayOfMonth,
  set: (v: number | string | null) => { bulkData.value.resetDayOfMonth = coerceResetDayOfMonth(v) },
})

// 和抽屉里同一套:两种周期共用一个数字输入,谁有值谁决定模式
const resetMode = computed<'days' | 'monthly'>({
  get: () => (bulkData.value.resetDayOfMonth > 0 ? 'monthly' : 'days'),
  set: (m) => {
    if (m === 'monthly') {
      bulkData.value.resetDayOfMonth = bulkData.value.resetDayOfMonth || new Date().getDate()
      bulkData.value.resetDays = 0
    } else {
      bulkData.value.resetDayOfMonth = 0
      bulkData.value.resetDays = bulkData.value.resetDays || 30
    }
  },
})

const patterns = computed(() => ({
  random: { title: t('bulk.random'), value: 'random' },
  order: { title: t('bulk.order'), value: 'order' },
}))

const groupItems = computed(() => Array.from(new Set(props.groups.filter((g) => g.length > 0))))

const inboundItems = computed(() => props.inboundTags.map((it) => {
  const inb = Data().inbounds?.find((i: any) => i.id == it.value)
  return {
    id: it.value,
    tag: it.title,
    type: inb?.type ?? '',
    port: inb?.listen_port ?? '',
    online: Data().onlines?.inbound ? Data().onlines.inbound.includes(it.title) : false,
  }
}))

const addText = (fk: 'name' | 'desc') => {
  const v = textInput.value[fk]
  if (v.length === 0) return
  bulkData.value[fk].push(v)
  textInput.value[fk] = ''
}

const toggleInbound = (id: number) => {
  const list = bulkData.value.clientInbounds
  bulkData.value.clientInbounds = list.includes(id) ? list.filter((i) => i !== id) : [...list, id].sort()
}
const setAllInbounds = () => {
  bulkData.value.clientInbounds = props.inboundTags.map((i) => i.value).sort()
}

const resetData = () => {
  count.value = 1
  loading.value = false
  bulkData.value = {
    name: [{ ...patterns.value.order }, '-', { ...patterns.value.random }],
    desc: [],
    group: '',
    clientInbounds: [],
    expiry: 0,
    Volume: 0,
    limitIp: 0,
    delayStart: false,
    autoReset: false,
    planDays: 0,
    resetDays: 0,
    resetDayOfMonth: 0,
  }
  textInput.value = { name: '', desc: '' }
}

// identical to legacy modals/ClientAddBulk.vue genByPattern
const genByPattern = (pattern: PatternPart[], order: number): string => {
  if (pattern.length == 0) return RandomUtil.randomSeq(8)
  let result = ''
  pattern.forEach((p) => {
    switch (typeof p) {
      case 'object':
        switch (p.value) {
          case 'random':
            result += RandomUtil.randomSeq(8)
            break
          case 'order':
            result += order + 1
        }
        break
      default:
        result += p
    }
  })
  return result
}

const saveChanges = async () => {
  if (!props.visible) return
  if (bulkData.value.name.findIndex((n) => typeof n == 'object') == -1) {
    push.error(t('error.dplData'))
    return
  }
  loading.value = true
  const newClients: Client[] = []
  for (let i = 0; i < count.value; i++) {
    const name = genByPattern(bulkData.value.name, i)
    newClients.push(createClient({
      enable: true,
      name: name,
      config: randomConfigs(name),
      inbounds: bulkData.value.clientInbounds.length > 0 ? bulkData.value.clientInbounds.sort() : [],
      links: [],
      volume: bulkData.value.Volume * (1024 ** 3),
      limitIp: bulkData.value.limitIp > 0 ? Math.floor(bulkData.value.limitIp) : 0,
      expiry: (bulkData.value.delayStart && bulkData.value.planDays > 0) ? 0 : bulkData.value.expiry,
      up: 0,
      down: 0,
      desc: genByPattern(bulkData.value.desc, i),
      group: bulkData.value.group,
      delayStart: bulkData.value.delayStart,
      autoReset: bulkData.value.autoReset,
      planDays: bulkData.value.planDays,
      resetDays: bulkData.value.resetDays,
      resetDayOfMonth: bulkData.value.resetDayOfMonth,
    }))
  }
  // Check duplicate names
  const isDuplicateName = Data().checkBulkClientNames(newClients.map((c) => c.name))
  if (isDuplicateName) {
    loading.value = false
    return
  }
  const success = await Data().save('clients', 'addbulk', newClients)
  if (success) emit('close')
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) resetData()
})
</script>

<style scoped>
.pat-box {
  min-height: 42px;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  background: var(--surface-3);
  border: 1px solid var(--line);
}
.pat-controls {
  display: flex;
  gap: 6px;
  margin-top: 7px;
  flex-wrap: wrap;
}
.inb-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 11px 14px;
  border-radius: 11px;
  border: 1px solid var(--line);
  background: var(--surface);
  cursor: pointer;
  transition: all 0.15s var(--ease);
}
.inb-item.on { background: var(--brand-soft); }
.inb-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-3); flex: none; }
.inb-dot.online { background: var(--emerald); }
.suffix-box {
  width: 70px;
  flex: none;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-2);
}
</style>
