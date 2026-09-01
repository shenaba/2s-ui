<template>
  <div v-if="acme" class="fade-up">
    <Field :label="$t('rule.domain') + ' ' + $t('commaSeparated')">
      <input class="input mono" v-model="domains" />
    </Field>
    <div class="grid2">
      <Field :label="$t('email')">
        <input class="input mono" v-model="email" placeholder="you@example.com" />
      </Field>
      <Field :label="$t('tls.acme.caProvider')">
        <Select v-model="caProvider">
          <option value="">{{ $t('none') }}</option>
          <option v-for="p in providerList" :key="p.value" :value="p.value">{{ p.title }}</option>
        </Select>
      </Field>
    </div>
    <Field v-if="caProvider == 'custom'" :label="$t('tls.acme.customCa')">
      <input class="input mono" v-model="acme.provider" />
    </Field>

    <!-- Reaching the CA over a shared HTTP client rather than the default
         outbound, for a panel whose own egress is proxied. -->
    <Field :label="$t('httpClient.title')">
      <Select v-model="httpClient">
        <option value="">{{ $t('none') }}</option>
        <option v-for="c in httpClients" :key="c" :value="c">{{ c }}</option>
      </Select>
    </Field>

    <div class="grid2" v-if="optionDir || optionDefault">
      <Field v-if="optionDir" :label="$t('tls.acme.dataDir')">
        <input class="input mono" v-model="acme.data_directory" />
      </Field>
      <Field v-if="optionDefault" :label="$t('tls.acme.defaultDomain')">
        <Select v-model="acme.default_server_name">
          <option v-for="d in acme.domain" :key="d" :value="d">{{ d }}</option>
        </Select>
      </Field>
    </div>

    <div v-if="optionChallenge" style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="disableHttpChallenge" :label="$t('tls.acme.httpChallenge')" />
      <SwitchLabel v-model="disableTlsChallenge" :label="$t('tls.acme.tlsChallenge')" />
    </div>

    <div class="grid2" v-if="optionPorts">
      <Field :label="$t('tls.acme.altHport')">
        <input class="input mono" type="number" min="1" max="65532" v-model.number="acme.alternative_http_port" />
      </Field>
      <Field :label="$t('tls.acme.altTport')">
        <input class="input mono" type="number" min="1" max="65532" v-model.number="acme.alternative_tls_port" />
      </Field>
    </div>

    <div class="grid2" v-if="acme.external_account != undefined">
      <Field label="Key ID">
        <input class="input mono" v-model="acme.external_account.key_id" />
      </Field>
      <Field label="MAC Key">
        <input class="input mono" v-model="acme.external_account.mac_key" />
      </Field>
    </div>

    <template v-if="acme.dns01_challenge != undefined">
      <Field :label="$t('tls.acme.dns01Provider')">
        <Select :model-value="acme.dns01_challenge.provider"
          @change="acme.dns01_challenge = { provider: $event }">
          <option v-for="d in dnsProviders" :key="d.provider" :value="d.provider">{{ d.provider }}</option>
        </Select>
      </Field>
      <div class="grid2">
        <Field
          v-for="item in dnsProviders.filter((d) => d.provider == acme.dns01_challenge?.provider)[0]?.params"
          :key="item"
          :label="$t('tls.acme.dns01Params.' + item)"
        >
          <input class="input mono" v-model="acme.dns01_challenge[item]" />
        </Field>
      </div>
    </template>

    <div style="display: flex; margin-bottom: 14px;">
      <div style="flex: 1;" />
      <Pop :min-width="220" direction="up">
        <template #trigger="{ toggle }">
          <Btn variant="subtle" sm @click="toggle">{{ $t('tls.acme.options') }}</Btn>
        </template>
        <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
          <div class="pop-item"><SwitchLabel v-model="optionDir" :label="$t('tls.acme.dataDir')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionDefault" :label="$t('tls.acme.defaultDomain')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionChallenge" :label="$t('tls.acme.disableChallenges')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionPorts" :label="$t('tls.acme.altPorts')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionExt" :label="$t('tls.acme.extAcc')" /></div>
          <div class="pop-item"><SwitchLabel v-model="optionDns01" :label="$t('tls.acme.dns01')" /></div>
        </div>
      </Pop>
    </div>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed } from 'vue'
import { acme as acmeType } from '@/types/tls'
import { httpClientTags } from '@/plugins/httpClient'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Pop from '@/components/ui/Pop.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'

// The provider object itself. Since sing-box 1.14 ACME is a certificate
// provider in the config's own list rather than a block inside a TLS config,
// so this form edits the provider directly instead of reaching through `tls`.
const props = defineProps<{ data: any }>()

const providerList = [
  { title: "Let's Encrypt", value: 'letsencrypt' },
  { title: 'ZeroSSL', value: 'zerossl' },
  { title: 'Custom', value: 'custom' },
]
const dnsProviders = [
  { provider: 'cloudflare', params: ['api_token', 'zone_token'] },
  { provider: 'alidns', params: ['access_key_id', 'access_key_secret', 'region_id', 'security_token'] },
  { provider: 'acmedns', params: ['username', 'password', 'subdomain', 'server_url'] },
]

const acme = computed((): acmeType => props.data)

const httpClients = computed((): string[] => httpClientTags())
const httpClient = computed({
  get: () => acme.value?.http_client ?? '',
  set: (v: string) => { acme.value.http_client = v.length > 0 ? v : undefined },
})

const domains = computed({
  get: () => acme.value?.domain ? acme.value.domain.join(',') : '',
  set: (v: string) => {
    if (!v.endsWith(',')) {
      acme.value.domain = v.length > 0 ? v.split(',') : []
    }
  },
})
const email = computed({
  get: () => acme.value?.email ?? '',
  set: (v: string) => { acme.value.email = v.length > 0 ? v : undefined },
})
const caProvider = computed({
  get: () => {
    if (acme.value?.provider == undefined) return ''
    return ['letsencrypt', 'zerossl'].includes(acme.value.provider) ? acme.value.provider : 'custom'
  },
  set: (v: string) => {
    if (v == '') delete acme.value.provider
    else acme.value.provider = ['letsencrypt', 'zerossl'].includes(v) ? v : 'https://'
  },
})
const disableHttpChallenge = computed({
  get: () => acme.value?.disable_http_challenge ?? false,
  set: (v: boolean) => { acme.value.disable_http_challenge = v },
})
const disableTlsChallenge = computed({
  get: () => acme.value?.disable_tls_alpn_challenge ?? false,
  set: (v: boolean) => { acme.value.disable_tls_alpn_challenge = v },
})

const optionDir = computed({
  get: (): boolean => acme.value?.data_directory != undefined,
  set: (v: boolean) => { acme.value.data_directory = v ? '' : undefined },
})
const optionDefault = computed({
  get: (): boolean => acme.value?.default_server_name != undefined,
  set: (v: boolean) => { acme.value.default_server_name = v ? (acme.value.domain.length > 0 ? acme.value.domain[0] : '') : undefined },
})
const optionChallenge = computed({
  get: (): boolean => acme.value?.disable_http_challenge != undefined || acme.value?.disable_tls_alpn_challenge != undefined,
  set: (v: boolean) => {
    if (v) {
      acme.value.disable_http_challenge = false
      acme.value.disable_tls_alpn_challenge = false
    } else {
      delete acme.value.disable_http_challenge
      delete acme.value.disable_tls_alpn_challenge
    }
  },
})
const optionPorts = computed({
  get: (): boolean => acme.value?.alternative_http_port != undefined || acme.value?.alternative_tls_port != undefined,
  set: (v: boolean) => {
    if (v) {
      acme.value.alternative_http_port = 80
      acme.value.alternative_tls_port = 443
    } else {
      delete acme.value.alternative_http_port
      delete acme.value.alternative_tls_port
    }
  },
})
const optionExt = computed({
  get: (): boolean => acme.value?.external_account != undefined,
  set: (v: boolean) => { acme.value.external_account = v ? { key_id: '', mac_key: '' } : undefined },
})
const optionDns01 = computed({
  get: (): boolean => acme.value?.dns01_challenge != undefined,
  set: (v: boolean) => {
    if (v) {
      acme.value.dns01_challenge = { provider: 'cloudflare' }
    } else {
      delete acme.value.dns01_challenge
    }
  },
})
</script>
