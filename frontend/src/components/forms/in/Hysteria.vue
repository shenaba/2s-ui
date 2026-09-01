<template>
  <div>
    <SectionLabel style="margin-bottom: 12px;">Hysteria</SectionLabel>
    <div class="grid2">
      <Field :label="$t('stats.upload') + ' (' + $t('stats.Mbps') + ')'">
        <input class="input mono" type="number" v-model.number="up_mbps" />
      </Field>
      <Field :label="$t('stats.download') + ' (' + $t('stats.Mbps') + ')'">
        <input class="input mono" type="number" min="0" v-model.number="down_mbps" />
      </Field>
    </div>
    <Field :label="$t('types.hy.obfs')">
      <input class="input mono" v-model="data.obfs" />
    </Field>
    <!-- recv_window_conn / recv_window_client / max_conn_client /
         disable_mtu_discovery were hysteria's own names for the QUIC options
         every QUIC protocol now shares. sing-box 1.14 still reads the old ones
         but only as a fallback, so the panel writes the new names only. -->
    <QuicFields :data="data" quic />
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import QuicFields from '@/components/forms/out/QuicFields.vue'

const props = defineProps<{ data: any }>()

const down_mbps = computed({
  get: (): any => (props.data.down_mbps ? props.data.down_mbps : 0),
  set: (v: any) => {
    if (v?.length != 0) {
      props.data.down_mbps = v
      props.data.down = '' + v + ' Mbps'
    } else {
      props.data.down_mbps = 0
      props.data.down = '0 Mbps'
    }
  },
})

const up_mbps = computed({
  get: (): number => (props.data.up_mbps ? props.data.up_mbps : 0),
  set: (v: number) => { props.data.up_mbps = v > 0 ? v : 0 },
})
</script>
