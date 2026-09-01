<template>
  <MDrawer
    :open="open"
    icon="tls"
    color="var(--amber)"
    :title="isNew ? $t('ui.certProviderNew') : form.tag"
    :sub="$t('tls.provider.sub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="560"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <div class="grid2">
      <Field :label="$t('type')">
        <Select v-model="providerType">
          <option v-for="t in types" :key="t.value" :value="t.value">{{ t.title }}</option>
        </Select>
      </Field>
      <!-- The tag is how a TLS config points at this provider, so it has to be
           set and unique; a dangling reference stops the core from starting. -->
      <Field :label="$t('objects.tag')" :hint="tagError">
        <input class="input mono" v-model="form.tag" />
      </Field>
    </div>

    <Acme v-if="form.type === 'acme'" :data="form" />
    <OriginCa v-else-if="form.type === 'cloudflare-origin-ca'" :data="form" />
    <Field v-else :label="$t('tls.provider.endpoint')" :hint="$t('tls.provider.endpointHint')">
      <input class="input mono" v-model="tailscaleEndpoint" />
    </Field>
  </MDrawer>
</template>

<script lang="ts" setup>
import { computed, ref, watch } from 'vue'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import Acme from '@/components/forms/out/tls/Acme.vue'
import OriginCa from '@/components/forms/out/tls/OriginCa.vue'
import Data from '@/store/modules/data'
import RandomUtil from '@/plugins/randomUtil'
import { i18n } from '@/locales'
import { certProvider, CertProviderType, createCertProvider } from '@/types/tls'

const props = defineProps<{
  open: boolean
  index: number
  data: string
  tags: string[]
}>()
const emit = defineEmits<{ close: []; save: [data: certProvider] }>()

const isNew = computed(() => props.index === -1)

const form = ref<certProvider>(createCertProvider('acme'))

function init() {
  if (props.index !== -1) {
    form.value = <certProvider>JSON.parse(props.data)
  } else {
    form.value = createCertProvider('acme', 'cert-' + RandomUtil.randomSeq(3))
  }
}
watch(() => props.open, (v) => { if (v) init() })

// ACME does not work on Windows (#1189) and is hidden there, but an existing
// one stays editable rather than silently changing type under the operator.
const types = computed((): { title: string; value: CertProviderType }[] => {
  const all: { title: string; value: CertProviderType }[] = [
    { title: 'ACME', value: 'acme' },
    { title: 'Tailscale', value: 'tailscale' },
    { title: 'Cloudflare Origin CA', value: 'cloudflare-origin-ca' },
  ]
  if (Data().os === 'windows' && form.value.type !== 'acme') {
    return all.filter(t => t.value !== 'acme')
  }
  return all
})

// Each type has its own option set, so switching drops the fields of the type
// being left rather than carrying them over as keys sing-box would reject.
// The tag survives: it names the provider rather than describing it.
const providerType = computed({
  get: () => form.value.type,
  set: (t: CertProviderType) => { form.value = createCertProvider(t, form.value.tag) },
})

const tailscaleEndpoint = computed({
  get: () => (<any>form.value).endpoint ?? '',
  set: (v: string) => { (<any>form.value).endpoint = v.length > 0 ? v : undefined },
})

const tagError = computed((): string => {
  const tag = form.value.tag?.trim() ?? ''
  if (tag.length === 0) return i18n.global.t('error.invalidData') + ': ' + i18n.global.t('objects.tag')
  const taken = props.index === -1
    ? props.tags
    : props.tags.filter((_, i) => i !== props.index)
  if (taken.includes(tag)) return i18n.global.t('error.dplData') + ': ' + i18n.global.t('objects.tag')
  return ''
})

function saveChanges() {
  if (tagError.value.length > 0) return
  form.value.tag = form.value.tag.trim()
  emit('save', form.value)
}
</script>
