<template>
  <MDrawer
    :open="visible"
    icon="services"
    color="var(--brand)"
    :title="isNew ? $t('ui.svcNew') : srv.tag"
    :sub="isNew ? $t('ui.svcSub') : srv.type"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <div class="grid2">
      <Field :label="$t('type')">
        <Select v-model="srvType">
          <option v-for="(value, key) in srvTypes" :key="value" :value="value">{{ key }}</option>
        </Select>
      </Field>
      <Field :label="$t('objects.tag')">
        <input class="input mono" v-model="srv.tag" />
      </Field>
    </div>

    <!-- oom-killer watches the process rather than listening on a socket. -->
    <Listen v-if="srv.type != srvTypes.OOMKiller" :data="srv" :inTags="inTags" />
    <Derp v-if="srv.type == srvTypes.DERP" :data="srv" :tsTags="tsTags" :inTags="inTags" />
    <SSMapi v-if="srv.type == srvTypes.SSMAPI" :data="srv" :ssTags="ssTags" />
    <Ocm v-if="srv.type == srvTypes.OCM" :data="srv" />
    <Ccm v-if="srv.type == srvTypes.CCM" :data="srv" />
    <Api v-if="srv.type == srvTypes.API" :data="srv" />
    <OomKiller v-if="srv.type == srvTypes.OOMKiller" :data="srv" />
    <InTLS v-if="HasTls.includes(srv.type)" :inbound="srv" :tlsConfigs="tlsConfigs" />
    <MHint v-if="srv.type == srvTypes.Resolved">{{ $t('ui.noFields') }}</MHint>
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import Data from '@/store/modules/data'
import RandomUtil from '@/plugins/randomUtil'
import { SrvTypes, Srv, createSrv } from '@/types/services'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import MHint from '@/components/ui/MHint.vue'
import Listen from '@/components/forms/out/Listen.vue'
import Derp from '@/components/forms/out/services/Derp.vue'
import Ocm from '@/components/forms/out/services/Ocm.vue'
import Ccm from '@/components/forms/out/services/Ccm.vue'
import SSMapi from '@/components/forms/out/services/SSMAPI.vue'
import Api from '@/components/forms/out/services/Api.vue'
import OomKiller from '@/components/forms/out/services/OomKiller.vue'
import InTLS from '@/components/forms/in/InTLS.vue'

const props = defineProps<{
  visible: boolean
  id: number
  data: string
  inTags?: string[]
  tsTags?: string[]
  ssTags?: string[]
  tlsConfigs?: any[]
}>()
const emit = defineEmits<{ close: [] }>()

const srvTypes = SrvTypes
const HasTls = [SrvTypes.DERP, SrvTypes.SSMAPI, SrvTypes.OCM, SrvTypes.CCM, SrvTypes.API]

const srv = ref<Srv>(createSrv('derp', { tag: '' }))
const loading = ref(false)

const isNew = computed(() => props.id === 0)

const updateData = (id: number) => {
  if (id > 0) {
    const newData = JSON.parse(props.data)
    srv.value = createSrv(newData.type, newData)
  } else {
    const port = RandomUtil.randomIntRange(10000, 60000)
    srv.value = createSrv('derp', {
      tag: 'derp-' + RandomUtil.randomSeq(3),
      listen: '::',
      listen_port: port,
    })
  }
}

// Tag change only in add service; keep previous listen basics (legacy changeType)
const srvType = computed({
  get: () => srv.value.type,
  set: (v: string) => {
    const tag = props.id > 0 ? srv.value.tag : v + '-' + RandomUtil.randomSeq(3)
    const prevConfig = { id: srv.value.id, tag: tag, listen: srv.value.listen, listen_port: srv.value.listen_port }
    srv.value = createSrv(v, prevConfig)
  },
})

const saveChanges = async () => {
  if (!props.visible) return

  // check duplicate tag
  const isDuplicatedTag = Data().checkTag('service', srv.value.id, srv.value.tag)
  if (isDuplicatedTag) return

  // save data
  loading.value = true
  const success = await Data().save('services', props.id == 0 ? 'new' : 'edit', srv.value)
  if (success) emit('close')
  loading.value = false
}

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>
