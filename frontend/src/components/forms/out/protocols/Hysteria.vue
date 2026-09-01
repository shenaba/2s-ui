<template>
  <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
    <SectionLabel>Hysteria</SectionLabel>
    <div style="flex: 1;" />
    <Pop :min-width="200" direction="down">
      <template #trigger="{ toggle }">
        <Btn variant="subtle" sm @click="toggle">{{ $t('types.hy.hyOptions') }}</Btn>
      </template>
      <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
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
    <Field :label="$t('types.hy.obfs')" :mb="0">
      <input class="input mono" v-model="data.obfs" />
    </Field>
    <Field :label="$t('types.hy.auth')" :mb="0">
      <input class="input mono" v-model="data.auth_str" />
    </Field>
    <Network :data="data" />
  </div>
  <!-- recv_window_conn / recv_window / disable_mtu_discovery were hysteria's
       own names for the QUIC options every QUIC protocol now shares. sing-box
       1.14 still reads the old ones, but only as a fallback, so the panel
       writes the new names only. -->
  <QuicFields :data="data" quic />
  <template v-if="optionMPort">
    <Field :label="$t('rule.portRange') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="serverPorts" />
    </Field>
    <Field :label="$t('ruleset.interval') + ' (' + $t('date.s') + ')'">
      <input class="input mono" type="number" min="0" v-model.number="hopInterval" />
    </Field>
  </template>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import Network from '../Network.vue'
import QuicFields from '../QuicFields.vue'

const props = defineProps<{ data: any }>()

const optionMPort = computed({
  get: (): boolean => props.data.server_ports != undefined,
  set: (v: boolean) => { props.data.server_ports = v ? [] : undefined },
})
const serverPorts = computed({
  get: () => props.data.server_ports?.join(',') ?? '',
  set: (v: string) => { props.data.server_ports = v.length > 0 ? v.split(',') : undefined },
})
const hopInterval = computed({
  get: () => props.data.hop_interval ? parseInt(props.data.hop_interval.replace('s', '')) : 0,
  set: (v: number) => { props.data.hop_interval = v > 0 ? v + 's' : undefined },
})
const downMbps = computed({
  get: () => props.data.down_mbps ? props.data.down_mbps : 0,
  set: (v: any) => {
    if (v && String(v).length != 0) {
      props.data.down_mbps = v
      props.data.down = '' + v + ' Mbps'
    } else {
      props.data.down_mbps = 0
      props.data.down = '0 Mbps'
    }
  },
})
const upMbps = computed({
  get: () => props.data.up_mbps ? props.data.up_mbps : 0,
  set: (v: number) => { props.data.up_mbps = v > 0 ? v : 0 },
})
</script>
