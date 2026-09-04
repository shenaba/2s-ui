<template>
  <div>
    <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
      <SectionLabel style="flex: 1;">Hysteria2</SectionLabel>
      <Pop :min-width="220">
        <template #trigger="{ toggle }">
          <Btn sm @click="toggle">
            <Ico name="settings" :size="14" /> {{ $t('types.hy.hy2Options') }}
          </Btn>
        </template>
        <div style="display: flex; flex-direction: column; gap: 2px; padding: 5px;">
          <SwitchLabel v-model="optionObfs" :label="$t('types.hy.obfs')" />
          <SwitchLabel v-model="optionMasq" label="Masquerade" />
          <SwitchLabel v-model="optionRealm" :label="$t('types.hy.realm')" />
        </div>
      </Pop>
    </div>
    <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
      <SwitchLabel v-model="data.ignore_client_bandwidth" :label="$t('types.hy.ignoreBw')" />
    </div>
    <div v-if="!data.ignore_client_bandwidth" class="grid2 fade-up">
      <Field :label="$t('stats.upload') + ' (' + $t('stats.Mbps') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="up_mbps" />
      </Field>
      <Field :label="$t('stats.download') + ' (' + $t('stats.Mbps') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="down_mbps" />
      </Field>
    </div>
    <Field :label="$t('types.hy.bbrProfile')">
      <Select v-model="bbrProfile">
        <option value="">{{ $t('ui.none') }}</option>
        <option v-for="p in bbrProfiles" :key="p" :value="p">{{ p }}</option>
      </Select>
    </Field>
    <Hysteria2Obfs v-if="data.obfs != undefined" :data="data" />
    <template v-if="data.masquerade != undefined">
      <SectionLabel style="margin-bottom: 12px;">Hysteria2 Masquerade</SectionLabel>
      <Field :label="$t('type')">
        <Select v-model="masqueradeType">
          <option v-for="m in masqTypes" :key="m.value" :value="m.value">{{ m.title }}</option>
        </Select>
      </Field>
      <Field v-if="masqueradeType == ''" label="HTTP3 server on auth fails">
        <input class="input mono" placeholder="file:///var/www | http://127.0.0.1:8080" v-model="data.masquerade" />
      </Field>
      <Field v-if="masqueradeType == 'file'" label="File server root directory">
        <input class="input mono" placeholder="/var/www" v-model="data.masquerade.directory" />
      </Field>
      <template v-if="masqueradeType == 'proxy'">
        <Field label="Target URL">
          <input class="input mono" placeholder="http://example.com:8080" v-model="data.masquerade.url" />
        </Field>
        <div style="display: flex; gap: 24px; flex-wrap: wrap; margin-bottom: 15px;">
          <SwitchLabel v-model="data.masquerade.rewrite_host" label="Rewrite Host" />
        </div>
      </template>
      <template v-if="masqueradeType == 'string'">
        <Field label="HTTP Code">
          <input class="input mono" type="number" min="100" max="599" v-model.number="data.masquerade.status_code" />
        </Field>
        <Field label="Content">
          <input class="input" v-model="data.masquerade.content" />
        </Field>
        <Headers :data="data.masquerade" />
      </template>
    </template>
    <Hysteria2Realm v-if="data.realm != undefined" :data="data" />
  </div>
  <QuicFields :data="data" quic />
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import QuicFields from '@/components/forms/out/QuicFields.vue'
import { useI18n } from 'vue-i18n'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import Pop from '@/components/ui/Pop.vue'
import Ico from '@/components/ui/Ico.vue'
import Btn from '@/components/ui/Btn.vue'
import Headers from '../out/Headers.vue'
import Hysteria2Obfs from '../out/Hysteria2Obfs.vue'
import Hysteria2Realm from '../out/Hysteria2Realm.vue'
import { bbrProfiles, createHysteria2Realm } from '@/types/hysteria2'

const props = defineProps<{ data: any }>()
const { t } = useI18n({ useScope: 'global' })

const masqTypes = computed(() => [
  { title: t('rule.simple'), value: '' },
  { title: 'File server', value: 'file' },
  { title: 'Reverse Proxy', value: 'proxy' },
  { title: 'Fixed response', value: 'string' },
])

const down_mbps = computed({
  get: (): number => props.data.down_mbps ?? 0,
  set: (v: number) => { props.data.down_mbps = v > 0 ? v : undefined },
})

const up_mbps = computed({
  get: (): number => props.data.up_mbps ?? 0,
  set: (v: number) => { props.data.up_mbps = v > 0 ? v : undefined },
})

const masqueradeType = computed({
  get: (): string => (typeof props.data.masquerade === 'object' ? props.data.masquerade.type ?? '' : ''),
  set: (v: string) => {
    if (v == '') {
      props.data.masquerade = ''
    } else {
      props.data.masquerade = { type: v }
    }
  },
})

const optionObfs = computed({
  get: (): boolean => props.data.obfs != undefined,
  set: (v: boolean) => { props.data.obfs = v ? { type: 'salamander', password: '' } : undefined },
})

const optionMasq = computed({
  get: (): boolean => props.data.masquerade != undefined,
  set: (v: boolean) => { props.data.masquerade = v ? '' : undefined },
})

const optionRealm = computed({
  get: (): boolean => props.data.realm != undefined,
  set: (v: boolean) => {
    if (v) props.data.realm = createHysteria2Realm()
    else delete props.data.realm
  },
})

const bbrProfile = computed({
  get: (): string => props.data.bbr_profile ?? '',
  set: (v: string) => {
    if (v.length > 0) props.data.bbr_profile = v
    else delete props.data.bbr_profile
  },
})
</script>
