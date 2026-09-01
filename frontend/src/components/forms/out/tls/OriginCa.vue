<template>
  <div class="fade-up">
    <Field :label="$t('rule.domain') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="domains" />
    </Field>

    <!-- Either credential works on its own; the API token is the narrower of
         the two, so it is offered first. -->
    <div class="grid2">
      <Field :label="$t('tls.provider.apiToken')">
        <input class="input mono" type="password" v-model="apiToken" />
      </Field>
      <Field :label="$t('tls.provider.originCaKey')">
        <input class="input mono" type="password" v-model="originCaKey" />
      </Field>
    </div>

    <div class="grid2">
      <Field :label="$t('tls.provider.requestType')">
        <Select v-model="requestType">
          <option value="">{{ $t('none') }}</option>
          <option v-for="r in requestTypes" :key="r.value" :value="r.value">{{ r.title }}</option>
        </Select>
      </Field>
      <Field :label="$t('tls.provider.validity') + ' (' + $t('date.d') + ')'">
        <Select v-model="validity">
          <option :value="0">{{ $t('none') }}</option>
          <option v-for="v in validities" :key="v" :value="v">{{ v }}</option>
        </Select>
      </Field>
    </div>

    <div class="grid2">
      <Field :label="$t('tls.acme.dataDir')">
        <input class="input mono" v-model="dataDirectory" />
      </Field>
      <Field :label="$t('httpClient.title')">
        <Select v-model="httpClient">
          <option value="">{{ $t('none') }}</option>
          <option v-for="c in httpClients" :key="c" :value="c">{{ c }}</option>
        </Select>
      </Field>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Select from '@/components/ui/Select.vue'
import { originCaProvider } from '@/types/tls'
import { httpClientTags } from '@/plugins/httpClient'

const props = defineProps<{ data: any }>()

const provider = computed((): originCaProvider => props.data)

const requestTypes = [
  { title: 'RSA', value: 'origin-rsa' },
  { title: 'ECC', value: 'origin-ecc' },
]
// The only validities Cloudflare accepts, in days.
const validities = [7, 30, 90, 365, 730, 1095, 5475]

const httpClients = computed((): string[] => httpClientTags())

const domains = computed({
  get: () => provider.value.domain ? provider.value.domain.join(',') : '',
  set: (v: string) => {
    // Mid-typing a trailing comma would otherwise produce an empty last entry
    // and rewrite what the operator is still typing.
    if (!v.endsWith(',')) {
      provider.value.domain = v.length > 0 ? v.split(',') : []
    }
  },
})

// Every optional field is dropped rather than stored empty: sing-box treats an
// empty string as a value, and a blank api_token would shadow origin_ca_key.
const apiToken = computed({
  get: () => provider.value.api_token ?? '',
  set: (v: string) => { provider.value.api_token = v.length > 0 ? v : undefined },
})
const originCaKey = computed({
  get: () => provider.value.origin_ca_key ?? '',
  set: (v: string) => { provider.value.origin_ca_key = v.length > 0 ? v : undefined },
})
const requestType = computed({
  get: () => provider.value.request_type ?? '',
  set: (v: string) => { provider.value.request_type = v.length > 0 ? <'origin-rsa' | 'origin-ecc'>v : undefined },
})
const validity = computed({
  get: () => provider.value.requested_validity ?? 0,
  set: (v: number) => { provider.value.requested_validity = v > 0 ? v : undefined },
})
const dataDirectory = computed({
  get: () => provider.value.data_directory ?? '',
  set: (v: string) => { provider.value.data_directory = v.length > 0 ? v : undefined },
})
const httpClient = computed({
  get: () => provider.value.http_client ?? '',
  set: (v: string) => { provider.value.http_client = v.length > 0 ? v : undefined },
})
</script>
