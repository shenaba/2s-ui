<template>
  <RuleDrawer
    :open="ruleDrawer.open"
    :index="ruleDrawer.index"
    :data="ruleDrawer.data"
    :clients="clients"
    :in-tags="inboundTags"
    :out-tags="outboundTags"
    :rs-tags="rulesetTags"
    @close="ruleDrawer.open = false"
    @save="saveRuleDrawer"
  />
  <RulesetDrawer
    :open="rulesetDrawer.open"
    :index="rulesetDrawer.index"
    :data="rulesetDrawer.data"
    :out-tags="outboundTags"
    @close="rulesetDrawer.open = false"
    @save="saveRulesetDrawer"
  />
  <RuleImportModal
    :open="importModal.open"
    :mode="importModal.mode"
    :existing-rules-count="rules.length"
    :existing-rulesets-count="rulesets.length"
    :existing-ruleset-tags="rulesetTags"
    :out-tags="outboundTags"
    :rs-tags="rulesetTags"
    @close="importModal.open = false"
    @save-rules="saveImportRules"
    @save-rulesets="saveImportRulesets"
  />

  <!-- delete confirmation -->
  <DeleteConfirm :open="del.open" @close="del.open = false" @confirm="confirmDelete" />

  <div class="page-stack-lg fade-up">
    <!-- ===================== toolbar ===================== -->
    <div v-if="!failed" class="toolbar" style="justify-content: center;">
      <Btn variant="primary" sm @click="showRuleDrawer(-1)">
        <Ico name="plus" :size="15" /> {{ $t('ui.addRule') }}
      </Btn>
      <Btn variant="primary" sm @click="showRulesetDrawer(-1)">
        <Ico name="plus" :size="15" /> {{ $t('ui.addRuleset') }}
      </Btn>
      <Pop :min-width="280">
        <template #trigger="{ toggle }">
          <Btn variant="subtle" sm icon :title="$t('ui.import')" @click="toggle">
            <Ico name="copy" :size="15" />
          </Btn>
        </template>
        <template #default="{ close }">
          <button type="button" class="pop-item" @click="openImport('rules'); close()">
            <Ico name="download" :size="15" /> {{ $t('rule.import.rulesTitle') }}
          </button>
          <button type="button" class="pop-item" @click="openImport('rulesets'); close()">
            <Ico name="download" :size="15" /> {{ $t('rule.import.title') }}
          </button>
        </template>
      </Pop>
      <Btn :variant="unchanged ? 'ghost' : 'primary'" sm :loading="loading" :disabled="unchanged" @click="saveConfig">
        <Ico name="check" :size="15" /> {{ $t('actions.save') }}
      </Btn>
    </div>

    <!-- No config arrived: say why instead of leaving a blank page under a
         spinning save button. -->
    <div v-if="failed" style="padding: 24px 0;">
      <EmptyState icon="link" :title="$t('ws.offlineTitle')" :desc="$t('ws.offlineDesc')" />
    </div>

    <template v-if="ready">
      <!-- ===================== routing base config ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.routing') }}</SectionLabel>
        <div class="card" style="padding: 18px; margin-top: 10px;">
          <div class="field-grid">
            <Field :label="$t('basic.routing.defaultOut')" :mb="0">
              <Select v-model="routeFinal">
                <option value="">{{ $t('ui.none') }}</option>
                <option v-for="tag in outboundTags" :key="tag" :value="tag">{{ tag }}</option>
              </Select>
            </Field>
            <Field :label="$t('basic.routing.defaultIf')" :mb="0">
              <input class="input mono" v-model="defaultInterface" placeholder="eth0" />
            </Field>
            <Field :label="$t('basic.routing.defaultRm')" :mb="0">
              <input class="input mono" type="number" min="0" v-model.number="routeMark" />
            </Field>
            <div style="display: flex; align-items: flex-end; padding-bottom: 6px;">
              <SwitchLabel v-model="autoDetect" :label="$t('basic.routing.autoBind')" />
            </div>
          </div>
        </div>
      </div>

      <!-- ===================== rule sets ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.ruleset') }}</SectionLabel>
        <div class="entity-grid" style="margin-top: 10px;">
          <EntityCard
            v-for="(item, index) in rulesets"
            :key="item.tag"
            :title="item.tag"
            :type="$t('ruleset.' + item.type)"
            :color="item.type === 'remote' ? 'var(--violet)' : 'var(--cyan)'"
            icon="rules"
            :rows="[
              { k: $t('ruleset.format'), v: item.format, mono: true },
              { k: $t('objects.outbound'), v: item.download_detour ?? $t('ui.none'), mono: !!item.download_detour },
              { k: $t('actions.update'), v: item.update_interval ?? $t('ui.none'), mono: !!item.update_interval },
            ]"
          >
            <template #actions>
              <CardBtn icon="edit" :title="$t('actions.edit')" @click="showRulesetDrawer(index)" />
              <CardBtn icon="trash" border danger :title="$t('actions.del')" @click="askDelete('ruleset', index)" />
            </template>
          </EntityCard>
        </div>
      </div>

      <!-- ===================== rules ===================== -->
      <div>
        <SectionLabel>{{ $t('ui.rules') }}</SectionLabel>
        <div class="entity-grid" style="margin-top: 10px;">
          <div
            v-for="(item, index) in rules"
            :key="index"
            style="display: flex;"
            draggable="true"
            @dragstart="onDragStart(index)"
            @dragover.prevent
            @drop="onDrop(index)"
          >
            <RuleCard
              style="flex: 1;"
              :n="index + 1"
              :logical="item.type != undefined"
              :mode="item.mode"
              :action="item.action"
              :target-key="$t('objects.outbound')"
              :target="item.outbound"
              :rules="matcherCount(item)"
              :invert="item.invert ?? false"
            >
              <template #actions>
                <CardBtn icon="edit" :title="$t('actions.edit')" @click="showRuleDrawer(index)" />
                <button type="button" class="mv-btn" :disabled="index === 0" @click="moveRule(index, -1)">
                  <Ico name="chevronDown" :size="16" style="transform: rotate(180deg);" />
                </button>
                <button type="button" class="mv-btn" :disabled="index === rules.length - 1" @click="moveRule(index, 1)">
                  <Ico name="chevronDown" :size="16" />
                </button>
                <CardBtn icon="trash" border danger :title="$t('actions.del')" @click="askDelete('rule', index)" />
              </template>
            </RuleCard>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, onBeforeMount, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import RuleDrawer from '@/layouts/drawers/route/RuleDrawer.vue'
import RulesetDrawer from '@/layouts/drawers/route/RulesetDrawer.vue'
import RuleImportModal from '@/layouts/drawers/route/RuleImportModal.vue'
import { Config } from '@/types/config'
import { actionKeys, ruleset } from '@/types/rules'
import { FindDiff } from '@/plugins/utils'
import { useReorder } from '@/plugins/useReorder'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Pop from '@/components/ui/Pop.vue'
import DeleteConfirm from '@/components/ui/DeleteConfirm.vue'
import Field from '@/components/ui/Field.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import EntityCard from '@/components/ui/EntityCard.vue'
import RuleCard from '@/components/ui/RuleCard.vue'
import CardBtn from '@/components/ui/CardBtn.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const oldConfig = ref({})
const loading = ref(false)
const ready = ref(false)
const failed = ref(false)

const appConfig = computed((): Config => {
  return <Config>Data().config
})

const init = () => {
  oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  ready.value = true
  failed.value = false
  loading.value = false
}

onBeforeMount(async () => {
  loading.value = true
  // Never render the form without real data: it edits the live config object,
  // so an empty one would let a save wipe the panel's whole configuration.
  if (!await Data().waitReady()) {
    loading.value = false
    failed.value = true
    return
  }
  init()
})

// waitReady gives up after 15s, but the socket's reconnect backoff can put a
// successful attempt past that. Without this the page keeps claiming it cannot
// load the configuration while the rest of the panel is live again.
watch(() => Data().lastLoad, (lu) => { if (lu > 0 && failed.value) init() })

const unchanged = computed(() => FindDiff.deepCompare(appConfig.value, oldConfig.value))

const saveConfig = async () => {
  loading.value = true
  const success = await Data().save('config', 'set', appConfig.value)
  if (success) {
    oldConfig.value = JSON.parse(JSON.stringify(Data().config))
  }
  loading.value = false
}

const clients = computed((): string[] => Data().clients.map((c: any) => c.name))
const route = computed((): any => appConfig.value.route ?? {})

const rules = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rules' in data) || !Array.isArray(data.rules)) data.rules = []
  return data.rules
})

const rulesets = computed((): any[] => {
  const data = route.value
  if (!data) return []
  if (!('rule_set' in data) || !Array.isArray(data.rule_set)) data.rule_set = []
  return data.rule_set
})

const rulesetTags = computed((): string[] => rulesets.value.map((rs: any) => rs.tag))

const outboundTags = computed((): string[] => Data().outboundTags)

const inboundTags = computed((): string[] => Data().inboundTags)

/* ---------------- routing base fields ---------------- */
const routeFinal = computed({
  get: () => route.value.final ?? '',
  set: (v: string) => {
    if (v.length > 0) route.value.final = v
    else delete route.value.final
  },
})

const defaultInterface = computed({
  get: () => route.value.default_interface ?? '',
  set: (v: string) => {
    if (v.length > 0) route.value.default_interface = v
    else delete route.value.default_interface
  },
})

const routeMark = computed({
  get() { return route.value.default_mark ?? 0 },
  set(v: number) {
    if (v > 0) route.value.default_mark = v
    else delete route.value.default_mark
  },
})

const autoDetect = computed({
  get: () => route.value.auto_detect_interface ?? false,
  set: (v: boolean) => { route.value.auto_detect_interface = v },
})

/* ---------------- rule / ruleset drawers ---------------- */
const matcherCount = (item: any): number =>
  item.rules ? item.rules.length : Object.keys(item).filter((r) => !actionKeys.includes(r)).length

const ruleDrawer = ref({ open: false, index: -1, data: '' })
const showRuleDrawer = (index: number) => {
  ruleDrawer.value.index = index
  ruleDrawer.value.data = index == -1 ? '' : JSON.stringify(rules.value[index])
  ruleDrawer.value.open = true
}
const saveRuleDrawer = (data: any) => {
  if (ruleDrawer.value.index == -1) rules.value.push(data)
  else rules.value[ruleDrawer.value.index] = data
  ruleDrawer.value.open = false
}

const rulesetDrawer = ref({ open: false, index: -1, data: '' })
const showRulesetDrawer = (index: number) => {
  rulesetDrawer.value.index = index
  rulesetDrawer.value.data = index == -1 ? '' : JSON.stringify(rulesets.value[index])
  rulesetDrawer.value.open = true
}
const saveRulesetDrawer = (data: ruleset) => {
  if (rulesetDrawer.value.index == -1) rulesets.value.push(data)
  else rulesets.value[rulesetDrawer.value.index] = data
  rulesetDrawer.value.open = false
}

/* ---------------- delete confirmation ---------------- */
const del = ref<{ open: boolean; kind: 'rule' | 'ruleset'; index: number }>({ open: false, kind: 'rule', index: -1 })
const askDelete = (kind: 'rule' | 'ruleset', index: number) => {
  del.value = { open: true, kind, index }
}
const confirmDelete = () => {
  if (del.value.kind === 'rule') rules.value.splice(del.value.index, 1)
  else rulesets.value.splice(del.value.index, 1)
  del.value.open = false
}

/* ---------------- ordering (drag & drop + move buttons) ---------------- */
const { onDragStart, onDrop, move: moveRule } = useReorder(() => rules.value)

/* ---------------- import (rules JSON / rulesets URL list) ---------------- */
const importModal = ref<{ open: boolean; mode: 'rules' | 'rulesets' }>({ open: false, mode: 'rules' })
const openImport = (mode: 'rules' | 'rulesets') => {
  importModal.value = { open: true, mode }
}

const saveImportRules = (block: any, mode: 'merge' | 'replace', applyFinal: boolean) => {
  if (mode === 'replace') {
    route.value.rules = block.rules ?? []
    route.value.rule_set = block.rule_set ?? []
  } else {
    const existingTags = new Set(rulesetTags.value)
    if (block.rules) rules.value.push(...block.rules)
    if (block.rule_set) {
      for (const rs of block.rule_set) {
        if (!existingTags.has(rs.tag)) rulesets.value.push(rs)
      }
    }
  }
  if (applyFinal && block.final) route.value.final = block.final
  importModal.value.open = false
}

const saveImportRulesets = (items: any[]) => {
  rulesets.value.push(...items)
  importModal.value.open = false
}
</script>
