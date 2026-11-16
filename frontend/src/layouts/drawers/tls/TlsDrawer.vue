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
      <input ref="sniInput" class="input mono" v-model="inTls.server_name" placeholder="www.cloudflare.com" />
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

      <!-- client authentication: the server side asking for a client
           certificate. Unlike the endpoint block below it is real sing-box
           inbound TLS, so an inbound on this config enforces it too -- and the
           panel hands its subscribers no certificate, which the hint says. -->
      <template v-if="optionClientAuth">
        <SectionLabel style="margin-bottom: 8px;">{{ $t('tls.clientAuth') }}</SectionLabel>
        <MHint style="margin-bottom: 12px;">{{ $t('tls.clientAuthHint') }}</MHint>
        <Field :label="$t('tls.clientAuthMode')">
          <Select v-model="clientAuthMode">
            <option value="">{{ $t('ui.none') }}</option>
            <option v-for="m in CLIENT_AUTH_MODES" :key="m" :value="m">{{ m }}</option>
          </Select>
        </Field>
        <Field :label="$t('tls.clientCa')" :hint="$t('tls.clientCaHint')">
          <textarea class="input mono" rows="4" spellcheck="false" v-model="trustedClientCertText"></textarea>
        </Field>
      </template>

      <div style="display: flex; gap: 20px; flex-wrap: wrap; margin-bottom: 14px;">
        <SwitchLabel v-model="disableSni" :label="$t('tls.disableSni')" />
        <SwitchLabel v-model="insecure" :label="$t('tls.insecure')" />
      </div>

      <!-- VPN endpoint: how an OpenConnect / OpenVPN endpoint using this config
           checks its server and proves itself to it. Kept out of every
           subscription (util.addTls), so it says so rather than look like part
           of what inbound users get. The CA is text only: certificate_path on
           the client side is stripped as a server-only field. -->
      <template v-if="optionEndpointTls">
        <SectionLabel style="margin-bottom: 8px;">{{ $t('tls.endpoint') }}</SectionLabel>
        <MHint style="margin-bottom: 12px;">{{ $t('tls.endpointHint') }}</MHint>
        <Field :label="$t('tls.serverCa')" :hint="$t('tls.serverCaHint')">
          <textarea class="input mono" rows="4" spellcheck="false" v-model="serverCaText"></textarea>
        </Field>
        <Field :label="$t('tls.peerFingerprint')" :hint="$t('tls.peerFingerprintHint')">
          <input class="input mono" v-model.lazy="peerFingerprints" />
        </Field>
        <div style="font-size: 12.5px; font-weight: 600; color: var(--text-2); margin-bottom: 8px;">{{ $t('tls.clientCert') }}</div>
        <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
          <Segmented
            v-model="useClientPath"
            :options="[[0, $t('tls.usePath')], [1, $t('tls.useText')]]"
            @update:model-value="switchClientCertMode"
          />
          <div style="flex: 1;" />
          <Btn variant="subtle" sm :loading="loading" :title="$t('actions.generate')" @click="genClientCert">
            <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
          </Btn>
        </div>
        <div v-if="useClientPath == 0" class="grid2">
          <Field :label="$t('tls.certPath')">
            <input class="input mono" v-model="clientCertPath" />
          </Field>
          <Field :label="$t('tls.keyPath')">
            <input class="input mono" v-model="clientKeyPath" />
          </Field>
        </div>
        <template v-else>
          <Field :label="$t('tls.cert')">
            <textarea class="input mono" rows="4" spellcheck="false" v-model="clientCertText"></textarea>
          </Field>
          <Field :label="$t('tls.key')">
            <textarea class="input mono" rows="4" spellcheck="false" v-model="clientKeyText"></textarea>
          </Field>
        </template>
      </template>
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
    <div class="grid2">
      <Field :label="$t('tls.handshakeTimeout')">
        <input class="input mono" placeholder="10s" v-model="inHandshakeTimeout" />
      </Field>
      <Field :label="$t('tls.handshakeTimeoutClient')">
        <input class="input mono" placeholder="10s" v-model="outHandshakeTimeout" />
      </Field>
    </div>
    <!-- The forged ClientHello needs elevated privileges and only works on
         Linux, macOS and Windows; the method decides how the real server
         rejects the forged segment, so it says nothing without a spoof. -->
    <div class="grid2">
      <Field :label="$t('tls.spoof')" :hint="$t('tls.spoofHint')">
        <input class="input mono" placeholder="example.com" v-model="spoof" />
      </Field>
      <Field v-if="spoof.length > 0" :label="$t('tls.spoofMethod')">
        <Select v-model="spoofMethod">
          <option value="">{{ $t('ui.none') }}</option>
          <option v-for="m in TLS_SPOOF_METHODS" :key="m" :value="m">{{ m }}</option>
        </Select>
      </Field>
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
            <div class="pop-item"><SwitchLabel v-model="optionClientAuth" :label="$t('tls.clientAuth')" /></div>
            <div class="pop-item"><SwitchLabel v-model="optionEndpointTls" :label="$t('tls.endpoint')" /></div>
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
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { push } from 'notivue'
import Data from '@/store/modules/data'
import HttpUtils from '@/plugins/httputil'
import RandomUtil from '@/plugins/randomUtil'
import { tls, iTls, defaultInTls, oTls, defaultOutTls, ALPN_OPTIONS, TLS_VERSIONS, CIPHER_SUITES, TLS_SPOOF_METHODS, UTLS_FINGERPRINTS } from '@/types/tls'
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
import MHint from '@/components/ui/MHint.vue'
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
const useClientPath = ref<string | number>(0)
// Flags of their own rather than read off the fields: switching a section on
// must not seed empty values -- an empty path the endpoint would try to open,
// an empty client CA sing-box would refuse.
const showEndpointTls = ref(false)
const showClientAuth = ref(false)

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
    const client = tlsData.value.client
    const server = tlsData.value.server
    // Text if either half is stored as text: opening in path mode would hide it,
    // and switching over from there deletes the paths.
    useClientPath.value = client.client_certificate == undefined && client.client_key == undefined ? 0 : 1
    showEndpointTls.value = ENDPOINT_TLS_FIELDS.some((f) => client[f] != undefined)
    showClientAuth.value = server.client_authentication != undefined || server.client_certificate != undefined
  } else {
    tlsData.value = <tls>{ id: 0, name: '', server: { enabled: true }, client: {} }
    tlsType.value = 0
    usePath.value = 0
    useClientPath.value = 0
    showEndpointTls.value = false
    showClientAuth.value = false
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
  showEndpointTls.value = false
  showClientAuth.value = false
}

const saveChanges = async () => {
  if (!props.visible) return
  // The SNI goes into every share link and subscription. A pasted name keeps
  // its stray spaces, which the generated certificate does not have (the panel
  // trims before issuing), and a blank one -- the field switched on and left
  // empty, as requireSni does -- became sni= / servername: "". Send it trimmed,
  // or not at all. A copy, so a refused save leaves the form as the operator
  // left it: deleting the key would take the SNI field off screen. The panel
  // does the same on its side for every other writer.
  const data = JSON.parse(JSON.stringify(tlsData.value))
  const sni = data.server?.server_name?.trim()
  if (sni) data.server.server_name = sni
  else if (data.server) delete data.server.server_name
  loading.value = true
  const success = await Data().save('tls', tlsData.value.id > 0 ? 'edit' : 'new', data)
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

// Both generators answer with the key's PEM lines, then the certificate's.
const splitPem = (lines: string[]): { key: string[]; cert: string[] } => {
  const key = <string[]>[]
  const cert = <string[]>[]
  let inKey = false
  let inCert = false
  lines.forEach((line: string) => {
    if (line === '-----BEGIN PRIVATE KEY-----') {
      inKey = true
      inCert = false
      key.push(line)
    } else if (line === '-----END PRIVATE KEY-----') {
      inKey = false
      key.push(line)
    } else if (line === '-----BEGIN CERTIFICATE-----') {
      inCert = true
      inKey = false
      cert.push(line)
    } else if (line === '-----END CERTIFICATE-----') {
      inCert = false
      cert.push(line)
    } else if (inKey) {
      key.push(line)
    } else if (inCert) {
      cert.push(line)
    }
  })
  return { key, cert }
}

// A self-signed certificate is generated for the SNI, and the panel refuses an
// empty one. (ECH keys are not: Ech.vue asks for their public name on its own,
// since using the SNI there would send the hidden name in the clear.) The field is off by default, tucked into the
// TLS Options menu, so the refusal alone named nothing on screen: switch it on
// and put the cursor there instead of sending a request bound to fail.
const sniInput = ref<HTMLInputElement>()
const requireSni = (): boolean => {
  if ((inTls.value.server_name ?? '').trim() != '') return true
  inTls.value.server_name = inTls.value.server_name ?? ''
  nextTick(() => sniInput.value?.focus())
  push.error({ message: t('tls.sniRequired') })
  return false
}

const genSelfSigned = async () => {
  if (!requireSni()) return
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'tls', o: inTls.value.server_name })
  loading.value = false
  // A failure is success:false, already shown by HttpUtils; success has lines.
  if (msg.success) {
    inTls.value.key_path = undefined
    inTls.value.certificate_path = undefined
    usePath.value = 1
    const { key, cert } = splitPem(msg.obj)
    inTls.value.key = key
    inTls.value.certificate = cert
  }
}

// ---------------- VPN endpoint (client side) ----------------
const ENDPOINT_TLS_FIELDS = [
  'certificate', 'peer_fingerprint',
  'client_certificate', 'client_certificate_path', 'client_key', 'client_key_path',
] as const

const optionEndpointTls = computed({
  get: (): boolean => showEndpointTls.value,
  set: (v: boolean) => {
    showEndpointTls.value = v
    if (!v) ENDPOINT_TLS_FIELDS.forEach((f) => delete outTls.value[f])
  },
})

// The CA an endpoint checks its server against. client.certificate is a single
// PEM string in the panel's types (OutTLS edits it that way too); sing-box
// takes either shape.
const serverCaText = computed({
  get: (): string => {
    const c: unknown = outTls.value.certificate
    return Array.isArray(c) ? c.join('\n') : (c as string | undefined) ?? ''
  },
  set: (v: string) => {
    if (v.trim().length > 0) outTls.value.certificate = v
    else delete outTls.value.certificate
  },
})

// Comma- or space-separated, committed on blur/Enter (v-model.lazy): parsing
// per keystroke would eat the separator before the next value is typed, and
// holding a trailing one back dropped a paste ending in a space altogether.
const peerFingerprints = computed({
  get: (): string => outTls.value.peer_fingerprint?.join(', ') ?? '',
  set: (v: string) => {
    const list = v.split(/[\s,]+/).filter((s) => s.length > 0)
    if (list.length > 0) outTls.value.peer_fingerprint = list
    else delete outTls.value.peer_fingerprint
  },
})

// ---------------- client authentication (server side) ----------------
// 'no' is sing-box's default, so it is the blank option rather than a second
// entry that means the same; a stored 'no' shows as that blank too.
const CLIENT_AUTH_MODES = ['request', 'require-any', 'verify-if-given', 'require-and-verify']

const optionClientAuth = computed({
  get: (): boolean => showClientAuth.value,
  set: (v: boolean) => {
    showClientAuth.value = v
    if (!v) {
      delete inTls.value.client_authentication
      delete inTls.value.client_certificate
    }
  },
})

const clientAuthMode = computed({
  get: (): string => (inTls.value.client_authentication === 'no' ? '' : inTls.value.client_authentication ?? ''),
  set: (v: string) => {
    if (v) inTls.value.client_authentication = v
    else delete inTls.value.client_authentication
  },
})

const switchClientCertMode = (v: string | number) => {
  if (v == 0) {
    delete outTls.value.client_certificate
    delete outTls.value.client_key
  } else {
    delete outTls.value.client_certificate_path
    delete outTls.value.client_key_path
  }
}

// The name of the template is the closest thing to an identity there is; the
// server only checks that it trusts the certificate.
const genClientCert = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', { k: 'tls-client', o: tlsData.value.name || 'client' })
  loading.value = false
  if (!msg.success) return
  const { key, cert } = splitPem(msg.obj ?? [])
  if (key.length === 0 || cert.length === 0) {
    push.error({ message: t('failed') + ': ' + msg.obj })
    return
  }
  delete outTls.value.client_certificate_path
  delete outTls.value.client_key_path
  outTls.value.client_certificate = cert
  outTls.value.client_key = key
  useClientPath.value = 1
}

// A PEM field stored as its lines, edited as one text. Emptied, it leaves no
// key rather than [''], which sing-box reads as a certificate and fails on.
const pemLines = (target: { value: any }, key: string) =>
  computed({
    get: (): string => (target.value[key] as string[] | undefined)?.join('\n') ?? '',
    set: (v: string) => {
      if (v.trim().length > 0) target.value[key] = v.split('\n')
      else delete target.value[key]
    },
  })
const clientCertText = pemLines(outTls, 'client_certificate')
const clientKeyText = pemLines(outTls, 'client_key')
const trustedClientCertText = pemLines(inTls, 'client_certificate')

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
const certText = pemLines(inTls, 'certificate')
const keyText = pemLines(inTls, 'key')
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
// Emptied fields leave no key: sing-box rejects "" for a duration, and a spoof
// method describes how a spoof is rejected, so it says nothing on its own.
const optionalText = (target: any, key: string) =>
  computed({
    get: (): string => target.value[key] ?? '',
    set: (v: string) => {
      const trimmed = (v ?? '').trim()
      if (trimmed) target.value[key] = trimmed
      else delete target.value[key]
    },
  })
const inHandshakeTimeout = optionalText(inTls, 'handshake_timeout')
const outHandshakeTimeout = optionalText(outTls, 'handshake_timeout')
// An emptied path leaves no key too: the endpoint would try to open "".
const clientCertPath = optionalText(outTls, 'client_certificate_path')
const clientKeyPath = optionalText(outTls, 'client_key_path')
const spoofMethodRaw = optionalText(outTls, 'spoof_method')
const spoofRaw = optionalText(outTls, 'spoof')
// tlsspoof.ParseOptions refuses "spoof_method requires spoof", and this drawer
// saves the object as it stands, so the method goes when the SNI does.
const spoof = computed({
  get: (): string => spoofRaw.value,
  set: (v: string) => {
    spoofRaw.value = v
    if (spoofRaw.value.length === 0) delete outTls.value.spoof_method
  },
})
const spoofMethod = computed({
  get: (): string => (spoof.value.length > 0 ? spoofMethodRaw.value : ''),
  set: (v: string) => { spoofMethodRaw.value = v },
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
