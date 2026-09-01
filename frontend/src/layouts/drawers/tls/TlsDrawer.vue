<template>
  <MDrawer
    :open="visible"
    icon="tls"
    color="var(--brand)"
    :title="isNew ? $t('ui.tlsNew') : tlsData.name"
    :sub="!isNew && inTls.server_name && inTls.server_name.length > 0 ? inTls.server_name : $t('ui.tlsSub')"
    :save-label="isNew ? $t('ui.create') : $t('actions.save')"
    :width="500"
    :loading="loading"
    @close="$emit('close')"
    @save="saveChanges"
  >
    <Field :label="$t('client.name')">
      <input class="input mono" v-model="tlsData.name" placeholder="reality-main" />
    </Field>

    <div style="margin-bottom: 15px;">
      <Segmented
        v-model="tlsType"
        block
        :options="[[0, 'TLS'], [1, 'Reality']]"
        @update:model-value="changeTlsType"
      />
    </div>

    <Field v-if="inTls.server_name != undefined" label="SNI">
      <input class="input mono" v-model="inTls.server_name" placeholder="www.cloudflare.com" />
    </Field>

    <!-- ===================== TLS mode ===================== -->
    <template v-if="tlsType == 0">
      <div class="grid2">
        <Field v-if="inTls.min_version" :label="$t('tls.minVer')">
          <Select v-model="inTls.min_version">
            <option v-for="v in TLS_VERSIONS" :key="v" :value="v">{{ v }}</option>
          </Select>
        </Field>
        <Field v-if="inTls.max_version" :label="$t('tls.maxVer')">
          <Select v-model="inTls.max_version">
            <option v-for="v in TLS_VERSIONS" :key="v" :value="v">{{ v }}</option>
          </Select>
        </Field>
      </div>
      <Field v-if="inTls.alpn" label="ALPN">
        <MultiPick v-model="alpnValue" :options="ALPN_OPTIONS" />
      </Field>
      <Field v-if="inTls.cipher_suites != undefined" :label="$t('tls.cs')">
        <ChipSelect v-model="cipherSuites" :options="CIPHER_SUITES" />
      </Field>

      <!-- certificate: path / text + self-signed generator. A certificate
           provider issues and renews the certificate itself, so these are
           hidden while one is selected rather than left to contradict it. -->
      <template v-if="certificateProvider === ''">
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
          <Segmented
            v-model="usePath"
            :options="[[0, $t('tls.usePath')], [1, $t('tls.useText')]]"
            @update:model-value="switchCertMode"
          />
          <div style="flex: 1;" />
          <Btn variant="subtle" sm :loading="loading" :title="$t('actions.generate')" @click="genSelfSigned">
            <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
          </Btn>
        </div>
        <template v-if="usePath == 0">
          <div class="grid2">
            <Field :label="$t('tls.certPath')">
              <input class="input mono" v-model="inTls.certificate_path" />
            </Field>
            <Field :label="$t('tls.keyPath')">
              <input class="input mono" v-model="inTls.key_path" />
            </Field>
          </div>
        </template>
        <template v-else>
          <Field :label="$t('tls.cert')">
            <textarea class="input mono" rows="4" spellcheck="false" v-model="certText"></textarea>
          </Field>
          <Field :label="$t('tls.key')">
            <textarea class="input mono" rows="4" spellcheck="false" v-model="keyText"></textarea>
          </Field>
        </template>
      </template>

      <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
        <SwitchLabel v-model="disableSni" :label="$t('tls.disableSni')" />
        <SwitchLabel v-model="insecure" :label="$t('tls.insecure')" />
      </div>
    </template>

    <!-- ===================== Reality ===================== -->
    <template v-if="outTls.reality && inTls.reality">
      <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
        <SectionLabel>Reality</SectionLabel>
        <div style="flex: 1;" />
        <Btn variant="subtle" sm :loading="loading" :title="$t('actions.generate')" @click="genRealityKey">
          <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
        </Btn>
      </div>
      <div class="grid2">
        <Field :label="$t('types.shdwTls.hs')">
          <input class="input mono" v-model="realityHandshakeServer" />
        </Field>
        <Field :label="$t('out.port')">
          <input class="input mono" type="number" min="0" v-model="server_port" />
        </Field>
      </div>
      <Field :label="$t('tls.privKey')">
        <input class="input mono" style="font-size: 12px;" v-model="realityPrivateKey" />
      </Field>
      <Field :label="$t('tls.pubKey')">
        <input class="input mono" style="font-size: 12px;" v-model="realityPublicKey" />
      </Field>
      <Field label="Short IDs">
        <KeyInput v-model="short_id" :title="$t('actions.generate')" @regenerate="randomSID" />
      </Field>
      <Field v-if="optionTime" :label="'Max Time Diference (' + $t('date.m') + ')'">
        <input class="input mono" type="number" min="1" v-model.number="max_time" />
      </Field>
    </template>

    <!-- shared extras (store / kernel TLS / uTLS fingerprint) -->
    <div v-if="optionStore || optionKtls" class="grid2">
      <Field v-if="optionStore" :label="$t('tls.store')">
        <Select v-model="storeValue">
          <option v-for="s in storeItems" :key="s.value" :value="s.value">{{ s.title }}</option>
        </Select>
      </Field>
    </div>
    <div v-if="optionKtls" style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
      <SwitchLabel v-model="kernelTx" :label="$t('tls.ktls') + ' ' + $t('tls.kernelTx')" />
      <SwitchLabel v-model="kernelRx" :label="$t('tls.ktls') + ' ' + $t('tls.kernelRx')" />
    </div>
    <div v-if="outTls.utls != undefined" class="grid2">
      <Field label="Fingerprint">
        <Select v-model="fingerprint">
          <option v-for="f in UTLS_FINGERPRINTS" :key="f.value" :value="f.value">{{ f.title }}</option>
        </Select>
      </Field>
    </div>

    <div style="display: flex; margin-bottom: 14px;">
      <div style="flex: 1;" />
      <Pop :min-width="210" direction="up">
        <template #trigger="{ toggle }">
          <Btn variant="subtle" sm @click="toggle">{{ $t('tls.options') }}</Btn>
        </template>
        <div style="display: flex; flex-direction: column; gap: 2px; padding: 4px;">
          <template v-if="tlsType == 0">
            <div class="pop-item"><SwitchLabel v-model="optionSNI" label="SNI" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionALPN" label="ALPN" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionMinV" :label="$t('tls.minVer')" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionMaxV" :label="$t('tls.maxVer')" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionCS" :label="$t('tls.cs')" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionFP" label="UTLS" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionStore" :label="$t('tls.store')" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionKtls" :label="$t('tls.ktls')" /></div>
          </template>
          <template v-else>
            <div class="pop-item"><SwitchLabel v-model="optionTime" label="Max Time Difference" /></div>
          </template>
        </div>
      </Pop>
    </div>

    <hr class="form-divider" />

    <!-- ================ certificate provider ================ -->
    <!-- Since sing-box 1.14 the issuer is a provider declared once in the
         config and referenced here by tag, so several TLS configs can share
         one. Providers are created on the TLS page itself. A provider issues
         the certificate, so the manual certificate fields above are hidden
         while one is selected. -->
    <Field :label="$t('tls.provider.title')" :hint="$t('tls.provider.hint')">
      <Select v-model="certificateProvider">
        <option value="">{{ $t('ui.none') }}</option>
        <option v-for="p in providers" :key="p" :value="p">{{ p }}</option>
      </Select>
    </Field>

    <!-- ===================== ECH ===================== -->
    <MSwitchRow v-model="echEnabled" :label="$t('ui.echTitle')" :desc="$t('ui.echDesc')" />
    <Ech :iTls="inTls" :oTls="outTls" />
  </MDrawer>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import RandomUtil from '@/plugins/randomUtil'
import { tls, iTls, defaultInTls, oTls, defaultOutTls, ALPN_OPTIONS, TLS_VERSIONS, CIPHER_SUITES, UTLS_FINGERPRINTS } from '@/types/tls'
import MDrawer from '@/components/ui/MDrawer.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Pop from '@/components/ui/Pop.vue'
import Segmented from '@/components/ui/Segmented.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import MSwitchRow from '@/components/ui/MSwitchRow.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import MultiPick from '@/components/ui/MultiPick.vue'
import ChipSelect from '@/components/ui/ChipSelect.vue'
import KeyInput from '@/components/ui/KeyInput.vue'
import Ech from '@/components/forms/out/tls/Ech.vue'

const props = defineProps<{
  visible: boolean
  id: number
  data: string
  providers: string[]
}>()
const emit = defineEmits<{ close: [] }>()

const { t } = useI18n({ useScope: 'global' })

const tlsData = ref<tls>(<tls>{ id: 0, name: '', server: <iTls>{ enabled: true }, client: <oTls>{} })
const loading = ref(false)
const tlsType = ref<string | number>(0)
const usePath = ref<string | number>(0)

const storeItems = [
  { title: 'Mozilla', value: 'mozilla' },
  { title: 'Chrome', value: 'chrome' },
]

const isNew = computed(() => props.id === 0)
const inTls = computed((): iTls => tlsData.value.server)
const outTls = computed((): oTls => tlsData.value.client)

// ---------------- lifecycle ----------------
const updateData = (id: number) => {
  if (id > 0) {
    const newData = <tls>JSON.parse(props.data)
    tlsData.value = newData
    if (tlsData.value.server == null) tlsData.value.server = { enabled: true }
    if (tlsData.value.client == null) tlsData.value.client = {}
    tlsType.value = newData.server?.reality == undefined ? 0 : 1
    usePath.value = newData.server?.key == undefined ? 0 : 1
  } else {
    tlsData.value = <tls>{ id: 0, name: '', server: { enabled: true }, client: {} }
    tlsType.value = 0
    usePath.value = 0
  }
}

const changeTlsType = () => {
  if (tlsType.value) {
    tlsData.value.server = <iTls>{
      enabled: true,
      reality: { enabled: true, handshake: { server_port: 443 }, short_id: RandomUtil.randomShortId() },
      server_name: '',
    }
    tlsData.value.client = <oTls>{ reality: { public_key: '' }, utls: defaultOutTls.utls }
  } else {
    tlsData.value.server = <iTls>{ enabled: true }
    tlsData.value.client = <oTls>{}
  }
}

const saveChanges = async () => {
  if (!props.visible) return
  loading.value = true
  const success = await Data().save('tls', tlsData.value.id > 0 ? 'edit' : 'new', tlsData.value)
  if (success) emit('close')
  loading.value = false
}

// ---------------- certificate ----------------
const switchCertMode = (v: string | number) => {
  if (v == 0) {
    inTls.value.key = undefined
    inTls.value.certificate = undefined
  } else {
    inTls.value.key_path = undefined
    inTls.value.certificate_path = undefined
  }
}

const genSelfSigned = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'tls', o: inTls.value.server_name ?? "''" })
  loading.value = false
  if (msg.success) {
    inTls.value.key_path = undefined
    inTls.value.certificate_path = undefined
    usePath.value = 1
    if (msg.obj.length > 0) {
      const privateKey = <string[]>[]
      const publicKey = <string[]>[]
      let isPrivateKey = false
      let isPublicKey = false

      msg.obj.forEach((line: string) => {
        if (line === '-----BEGIN PRIVATE KEY-----') {
          isPrivateKey = true
          isPublicKey = false
          privateKey.push(line)
        } else if (line === '-----END PRIVATE KEY-----') {
          isPrivateKey = false
          privateKey.push(line)
        } else if (line === '-----BEGIN CERTIFICATE-----') {
          isPublicKey = true
          isPrivateKey = false
          publicKey.push(line)
        } else if (line === '-----END CERTIFICATE-----') {
          isPublicKey = false
          publicKey.push(line)
        } else if (isPrivateKey) {
          privateKey.push(line)
        } else if (isPublicKey) {
          publicKey.push(line)
        }
      })
      inTls.value.key = privateKey ?? undefined
      inTls.value.certificate = publicKey ?? undefined
    } else {
      push.error({
        message: t('failed') + ': ' + msg.obj,
      })
    }
  }
}

// ---------------- reality ----------------
const genRealityKey = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'reality' })
  loading.value = false
  if (msg.success) {
    msg.obj.forEach((line: string) => {
      if (inTls.value.reality && outTls.value.reality) {
        if (line.startsWith('PrivateKey')) {
          inTls.value.reality.private_key = line.substring(12)
        }
        if (line.startsWith('PublicKey')) {
          outTls.value.reality.public_key = line.substring(11)
        }
      }
    })
  } else {
    push.error({
      message: t('failed') + ': ' + msg.obj,
    })
  }
}

const randomSID = () => {
  short_id.value = RandomUtil.randomShortId().join(',')
}

const realityHandshakeServer = computed({
  get: () => inTls.value.reality?.handshake?.server ?? '',
  set: (v: string) => { if (inTls.value.reality) inTls.value.reality.handshake.server = v },
})
const realityPrivateKey = computed({
  get: () => inTls.value.reality?.private_key ?? '',
  set: (v: string) => { if (inTls.value.reality) inTls.value.reality.private_key = v },
})
const realityPublicKey = computed({
  get: () => outTls.value.reality?.public_key ?? '',
  set: (v: string) => { if (outTls.value.reality) outTls.value.reality.public_key = v },
})
const server_port = computed({
  get: () => inTls.value.reality?.handshake?.server_port ? inTls.value.reality.handshake.server_port : 443,
  set: (v: any) => {
    if (inTls.value.reality) {
      inTls.value.reality.handshake.server_port = v.length == 0 || v == 0 ? 443 : parseInt(v)
    }
  },
})
const short_id = computed({
  get: () => inTls.value.reality?.short_id ? inTls.value.reality.short_id.join(',') : undefined,
  set: (v: string) => {
    if (inTls.value.reality) {
      inTls.value.reality.short_id = v.length > 0 ? v.split(',') : []
    }
  },
})
const max_time = computed({
  get: () => inTls.value?.reality?.max_time_difference ? parseInt(inTls.value.reality.max_time_difference.replace('m', '')) : 1,
  set: (v: number) => {
    if (inTls.value.reality) {
      inTls.value.reality.max_time_difference = v > 0 ? v + 'm' : '1m'
    }
  },
})

// ---------------- text/value bridges ----------------
const certText = computed({
  get: (): string => inTls.value.certificate ? inTls.value.certificate.join('\n') : '',
  set: (v: string) => { inTls.value.certificate = v.split('\n') },
})
const keyText = computed({
  get: (): string => inTls.value.key ? inTls.value.key.join('\n') : '',
  set: (v: string) => { inTls.value.key = v.split('\n') },
})
const disableSni = computed({
  get: () => outTls.value.disable_sni ?? false,
  set: (v: boolean) => { tlsData.value.client.disable_sni = v ? true : undefined },
})
const insecure = computed({
  get: () => outTls.value.insecure ?? false,
  set: (v: boolean) => { tlsData.value.client.insecure = v ? true : undefined },
})
const alpnValue = computed({
  get: () => inTls.value.alpn ?? [],
  set: (v: string[]) => { inTls.value.alpn = v },
})
const cipherSuites = computed({
  get: () => inTls.value.cipher_suites ?? [],
  set: (v: string[]) => { inTls.value.cipher_suites = v },
})
const storeValue = computed({
  get: (): string => inTls.value.store ?? 'mozilla',
  set: (v: string) => { inTls.value.store = <'mozilla' | 'chrome'>v },
})
const fingerprint = computed({
  get: (): string => outTls.value.utls?.fingerprint ?? 'chrome',
  set: (v: string) => { if (outTls.value.utls) outTls.value.utls.fingerprint = v },
})
const kernelTx = computed({
  get: () => inTls.value.kernel_tx ?? false,
  set: (v: boolean) => { inTls.value.kernel_tx = v },
})
const kernelRx = computed({
  get: () => inTls.value.kernel_rx ?? false,
  set: (v: boolean) => { inTls.value.kernel_rx = v },
})

// ---------------- option toggles ----------------
const optionSNI = computed({
  get: (): boolean => inTls.value.server_name != undefined,
  set: (v: boolean) => { inTls.value.server_name = v ? '' : undefined },
})
const optionALPN = computed({
  get: (): boolean => inTls.value.alpn != undefined,
  set: (v: boolean) => { inTls.value.alpn = v ? defaultInTls.alpn : undefined },
})
const optionMinV = computed({
  get: (): boolean => inTls.value.min_version != undefined,
  set: (v: boolean) => { inTls.value.min_version = v ? defaultInTls.min_version : undefined },
})
const optionMaxV = computed({
  get: (): boolean => inTls.value.max_version != undefined,
  set: (v: boolean) => { inTls.value.max_version = v ? defaultInTls.max_version : undefined },
})
const optionCS = computed({
  get: (): boolean => inTls.value.cipher_suites != undefined,
  set: (v: boolean) => { inTls.value.cipher_suites = v ? defaultInTls.cipher_suites : undefined },
})
const optionFP = computed({
  get: (): boolean => outTls.value.utls != undefined,
  set: (v: boolean) => { outTls.value.utls = v ? defaultOutTls.utls : undefined },
})
const optionStore = computed({
  get: (): boolean => inTls.value.store != undefined,
  set: (v: boolean) => { inTls.value.store = v ? 'mozilla' : undefined },
})
const optionKtls = computed({
  get: (): boolean => inTls.value.kernel_tx != undefined || inTls.value.kernel_rx != undefined,
  set: (v: boolean) => {
    if (v) {
      inTls.value.kernel_tx = false
      inTls.value.kernel_rx = false
    } else {
      delete inTls.value.kernel_tx
      delete inTls.value.kernel_rx
    }
  },
})
const optionTime = computed({
  get: (): boolean => inTls.value?.reality?.max_time_difference != undefined,
  set: (v: boolean) => { if (inTls.value.reality) inTls.value.reality.max_time_difference = v ? '1m' : undefined },
})

// ---------------- provider / ECH section toggles ----------------
const certificateProvider = computed({
  get: (): string => inTls.value.certificate_provider ?? '',
  set: (v: string) => { inTls.value.certificate_provider = v.length > 0 ? v : undefined },
})
const echEnabled = computed({
  get: (): boolean => inTls.value.ech != undefined,
  set: (v: boolean) => {
    inTls.value.ech = v ? { enabled: true } : undefined
    outTls.value.ech = v ? <any>{} : undefined
  },
})

watch(() => props.visible, (v) => {
  if (v) updateData(props.id)
})
</script>
