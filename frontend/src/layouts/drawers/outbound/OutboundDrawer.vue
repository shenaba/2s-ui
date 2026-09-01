<template>
  <MDrawer
    :open="visible"
    icon="outbound"
    :color="outColor(outbound.type)"
    :title="isNew ? $t('ui.outNew') : outbound.tag"
    :sub="isNew ? $t('ui.outSub') : subTitle"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <Tabs v-model="tab" :tabs="tabs" />

    <!-- ===================== basics ===================== -->
    <div v-if="tab === 'basics'">
      <div class="grid2">
        <Field :label="$t('type')">
          <Select v-model="outboundType">
            <option v-for="(value, key) in outTypes" :key="value" :value="value">{{ key }}</option>
          </Select>
        </Field>
        <Field :label="$t('objects.tag')">
          <input class="input mono" v-model="outbound.tag" />
        </Field>
      </div>

      <div v-if="!NoServer.includes(outbound.type)" class="grid2">
        <Field :label="$t('out.addr')">
          <input class="input mono" v-model="outbound.server" />
        </Field>
        <Field :label="$t('out.port')">
          <input class="input mono" type="number" min="0" v-model.number="outbound.server_port" />
        </Field>
      </div>

      <Direct v-if="outbound.type == outTypes.Direct" :data="outbound" />
      <Socks v-if="outbound.type == outTypes.SOCKS" :data="outbound" />
      <Http v-if="outbound.type == outTypes.HTTP" :data="outbound" />
      <Shadowsocks v-if="outbound.type == outTypes.Shadowsocks" :data="outbound" />
      <Snell v-if="outbound.type == outTypes.Snell" direction="out" :data="outbound" />
      <Bridge v-if="outbound.type == outTypes.Bridge" direction="out" :data="outbound" />
      <Vmess v-if="outbound.type == outTypes.VMess" :data="outbound" />
      <Trojan v-if="outbound.type == outTypes.Trojan" :data="outbound" />
      <Hysteria v-if="outbound.type == outTypes.Hysteria" :data="outbound" />
      <Naive v-if="outbound.type == outTypes.Naive" :data="outbound" />
      <ShadowTls v-if="outbound.type == outTypes.ShadowTLS" :data="outbound" />
      <Vless v-if="outbound.type == outTypes.VLESS" :data="outbound" />
      <Tuic v-if="outbound.type == outTypes.TUIC" :data="outbound" />
      <Hysteria2 v-if="outbound.type == outTypes.Hysteria2" :data="outbound" />
      <AnyTls v-if="outbound.type == outTypes.AnyTls" :data="outbound" />
      <Tor v-if="outbound.type == outTypes.Tor" :data="outbound" />
      <Ssh v-if="outbound.type == outTypes.SSH" :data="outbound" />
      <Selector v-if="outbound.type == outTypes.Selector" :data="outbound" :tags="tags" />
      <UrlTest v-if="outbound.type == outTypes.URLTest" :data="outbound" :tags="tags" />

      <Transport v-if="Object.hasOwn(outbound, 'transport')" :data="outbound" />
      <OutTLS v-if="Object.hasOwn(outbound, 'tls')" :outbound="outbound" />
      <Multiplex v-if="Object.hasOwn(outbound, 'multiplex')" :data="outbound" />
      <Dial v-if="!NoDial.includes(outbound.type)" :dial="outbound" />
    </div>

    <!-- ===================== external link ===================== -->
    <div v-else>
      <Field :label="$t('client.external')">
        <input class="input mono" dir="ltr" v-model="link" />
      </Field>
      <div style="display: flex; justify-content: center; margin-bottom: 15px;">
        <Btn variant="primary" :loading="loading" @click="linkConvert">{{ $t('submit') }}</Btn>
      </div>
    </div>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import RandomUtil from '@/plugins/randomUtil'
import { outColor } from '@/plugins/colors'
import { OutTypes, Outbound, createOutbound } from '@/types/outbounds'
import MDrawer from '@/components/ui/MDrawer.vue'
import Tabs from '@/components/ui/Tabs.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Dial from '@/components/forms/out/Dial.vue'
import Multiplex from '@/components/forms/out/Multiplex.vue'
import Transport from '@/components/forms/out/Transport.vue'
import OutTLS from '@/components/forms/out/OutTLS.vue'
import Direct from '@/components/forms/out/protocols/Direct.vue'
import Socks from '@/components/forms/out/protocols/Socks.vue'
import Snell from '@/components/forms/out/protocols/Snell.vue'
import Bridge from '@/components/forms/out/protocols/Bridge.vue'
import Http from '@/components/forms/out/protocols/Http.vue'
import Shadowsocks from '@/components/forms/out/protocols/Shadowsocks.vue'
import Vmess from '@/components/forms/out/protocols/Vmess.vue'
import Trojan from '@/components/forms/out/protocols/Trojan.vue'
import Hysteria from '@/components/forms/out/protocols/Hysteria.vue'
import Naive from '@/components/forms/out/protocols/Naive.vue'
import ShadowTls from '@/components/forms/out/protocols/OutShadowTls.vue'
import Vless from '@/components/forms/out/protocols/Vless.vue'
import Tuic from '@/components/forms/out/protocols/Tuic.vue'
import Hysteria2 from '@/components/forms/out/protocols/Hysteria2.vue'
import AnyTls from '@/components/forms/out/protocols/AnyTls.vue'
import Tor from '@/components/forms/out/protocols/Tor.vue'
import Ssh from '@/components/forms/out/protocols/Ssh.vue'
import Selector from '@/components/forms/out/protocols/Selector.vue'
import UrlTest from '@/components/forms/out/protocols/UrlTest.vue'

const props = defineProps<{
  visible: boolean
  id: number
  data: string
  tags: string[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const outTypes = OutTypes
const NoDial = [OutTypes.Selector, OutTypes.URLTest]
const NoServer = [OutTypes.Direct, OutTypes.Selector, OutTypes.URLTest, OutTypes.Tor, OutTypes.Bridge]

const outbound = ref<Outbound>(createOutbound('direct', { tag: '' }))
const tab = ref('basics')
const link = ref('')
const loading = ref(false)

const isNew = computed(() => props.id === 0)
const subTitle = computed(() =>
  outbound.value.type + (outbound.value.server ? ' · ' + outbound.value.server : ''))
const tabs = computed((): [string, string][] => [
  ['basics', t('client.basics')],
  ['external', t('client.external')],
])

const updateData = (id: number) => {
  if (id > 0) {
    const newData = JSON.parse(props.data)
    outbound.value = createOutbound(newData.type, newData)
  } else {
    outbound.value = createOutbound('direct', { tag: 'direct-' + RandomUtil.randomSeq(3) })
  }
  tab.value = 'basics'
  link.value = ''
}

// Tag change only in add outbound; keep previous basics (legacy changeType)
const outboundType = computed({
  get: () => outbound.value.type,
  set: (v: string) => {
    outbound.value.type = v
    const tag = props.id > 0 ? outbound.value.tag : outbound.value.type + '-' + RandomUtil.randomSeq(3)
    const prevConfig = {
      id: outbound.value.id,
      tag: tag,
      listen: outbound.value.listen,
      listen_port: outbound.value.listen_port,
    }
    outbound.value = createOutbound(outbound.value.type, prevConfig)
  },
})

const saveChanges = async () => {
  if (!props.visible) return
  // check duplicate tag
  const isDuplicatedTag = Data().checkTag('outbound', props.id, outbound.value.tag)
  if (isDuplicatedTag) return

  // save data
  loading.value = true
  const success = await Data().save('outbounds', props.id == 0 ? 'new' : 'edit', outbound.value)
  if (success) emit('close')
  loading.value = false
}

const linkConvert = async () => {
  if (link.value.length > 0) {
    loading.value = true
    const msg = await HttpUtils.post('api/linkConvert', { link: link.value })
    loading.value = false
    if (msg.success) {
      outbound.value = msg.obj
      if (props.id > 0) outbound.value.id = props.id
      tab.value = 'basics'
      link.value = ''
    }
  }
}

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>
