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
        <!-- Port hopping and a realm are refused together ("realm and port
             hopping are mutually exclusive"), so neither is offered while the
             other is on. A toggle already on stays visible either way: a stored
             outbound carrying both has to be repairable from here. -->
        <div v-if="optionMPort || !optionRealm" class="pop-item"><SwitchLabel v-model="optionMPort" :label="$t('rule.portRange')" /></div>
        <div v-if="optionRealm || !optionMPort" class="pop-item"><SwitchLabel v-model="optionRealm" :label="$t('types.hy.realm')" /></div>
        <div class="pop-item"><SwitchLabel v-model="disableChromeParrot" :label="$t('types.hy.disableChromeParrot')" /></div>
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
    <Field :label="$t('types.pw')" :mb="0">
      <input class="input mono" v-model="data.password" />
    </Field>
    <Field :label="$t('types.hy.bbrProfile')" :mb="0">
      <Select v-model="bbrProfile">
        <option value="">{{ $t('ui.none') }}</option>
        <option v-for="p in bbrProfiles" :key="p" :value="p">{{ p }}</option>
      </Select>
    </Field>
    <Network :data="data" />
  </div>
  <Hysteria2Obfs v-if="data.obfs != undefined" :data="data" />
  <template v-if="optionMPort">
    <Field :label="$t('rule.portRange') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="serverPorts" />
    </Field>
    <div class="grid2">
      <Field :label="$t('ruleset.interval') + ' (' + $t('date.s') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="hopInterval" />
      </Field>
      <!-- Set alongside hop_interval, 1.14 picks each hop uniformly from the
           range instead of hopping on a fixed beat. -->
      <Field :label="$t('types.hy.hopIntervalMax') + ' (' + $t('date.s') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="hopIntervalMax" />
      </Field>
    </div>
  </template>
  <Hysteria2Realm v-if="data.realm != undefined" :data="data" />
  <QuicFields :data="data" quic />
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import QuicFields from '../QuicFields.vue'
import Hysteria2Obfs from '../Hysteria2Obfs.vue'
import Hysteria2Realm from '../Hysteria2Realm.vue'
import { bbrProfiles, createHysteria2Realm } from '@/types/hysteria2'
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
const hopIntervalMax = computed({
  get: () => props.data.hop_interval_max ? parseInt(props.data.hop_interval_max.replace('s', '')) : 0,
  set: (v: number) => { props.data.hop_interval_max = v > 0 ? v + 's' : undefined },
})
const bbrProfile = computed({
  get: (): string => props.data.bbr_profile ?? '',
  set: (v: string) => {
    if (v.length > 0) props.data.bbr_profile = v
    else delete props.data.bbr_profile
  },
})
const disableChromeParrot = computed({
  get: (): boolean => props.data.disable_chrome_parrot === true,
  set: (v: boolean) => {
    if (v) props.data.disable_chrome_parrot = true
    else delete props.data.disable_chrome_parrot
  },
})
// The realm supplies the address, so the fields that name one have to go with
// it: sing-box refuses "realm conflicts with server, server_port, and
// server_ports", and the drawer's own server row is hidden while it is on.
const optionRealm = computed({
  get: (): boolean => props.data.realm != undefined,
  set: (v: boolean) => {
    if (!v) {
      delete props.data.realm
      return
    }
    props.data.realm = createHysteria2Realm()
    delete props.data.server
    delete props.data.server_port
    delete props.data.server_ports
    delete props.data.hop_interval
    delete props.data.hop_interval_max
  },
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
