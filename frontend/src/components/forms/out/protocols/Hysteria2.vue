<template>
  <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
    <SectionLabel>Hysteria2</SectionLabel>
    <div style="flex: 1;" />
    <Pop :min-width="200" direction="down">
      <template #trigger="{ toggle }">
        <Btn variant="subtle" sm @click="toggle">{{ $t('types.hy.hy2Options') }}</Btn>
      </template>
      <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
        <div class="pop-item"><SwitchLabel v-model="optionObfs" :label="$t('types.hy.obfs')" /></div>
        <div class="pop-item"><SwitchLabel v-model="optionMPort" :label="$t('rule.portRange')" /></div>
      </div>
    </Pop>
  </div>
  <div class="grid2" style="margin-bottom: 15px;">
    <Field :label="$t('stats.upload') + ' (' + $t('stats.Mbps') + ')'" :mb="0">
      <input class="input mono" type="number" min="0" v-model.number="upMbps" />
    </Field>
    <Field :label="$t('stats.download') + ' (' + $t('stats.Mbps') + ')'" :mb="0">
      <input class="input mono" type="number" min="0" v-model.number="downMbps" />
    </Field>
    <Field v-if="data.obfs != undefined" :label="$t('types.hy.obfs')" :mb="0">
      <input class="input mono" v-model="data.obfs.password" />
    </Field>
    <Field :label="$t('types.pw')" :mb="0">
      <input class="input mono" v-model="data.password" />
    </Field>
    <Network :data="data" />
  </div>
  <template v-if="optionMPort">
    <Field :label="$t('rule.portRange') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="serverPorts" />
    </Field>
    <Field :label="$t('ruleset.interval') + ' (' + $t('date.s') + ')'">
      <input class="input mono" type="number" min="0" v-model.number="hopInterval" />
    </Field>
  </template>
  <QuicFields :data="data" quic />
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import QuicFields from '../QuicFields.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import Network from '../Network.vue'

const props = defineProps<{ data: any }>()

const downMbps = computed({
  get: () => props.data.down_mbps ?? 0,
  set: (v: number) => { props.data.down_mbps = v > 0 ? v : undefined },
})
const upMbps = computed({
  get: () => props.data.up_mbps ?? 0,
  set: (v: number) => { props.data.up_mbps = v > 0 ? v : undefined },
})
const serverPorts = computed({
  get: () => props.data.server_ports?.join(',') ?? '',
  set: (v: string) => { props.data.server_ports = v.length > 0 ? v.split(',') : undefined },
})
const hopInterval = computed({
  get: () => props.data.hop_interval ? parseInt(props.data.hop_interval.replace('s', '')) : 0,
  set: (v: number) => { props.data.hop_interval = v > 0 ? v + 's' : undefined },
})
const optionObfs = computed({
  get: (): boolean => props.data.obfs != undefined,
  set: (v: boolean) => { props.data.obfs = v ? { type: 'salamander', password: '' } : undefined },
})
const optionMPort = computed({
  get: (): boolean => props.data.server_ports != undefined,
  set: (v: boolean) => { props.data.server_ports = v ? [] : undefined },
})
</script>
