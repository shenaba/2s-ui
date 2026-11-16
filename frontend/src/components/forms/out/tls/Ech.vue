<template>
  <div v-if="ech" class="fade-up">
    <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
      <Segmented
        v-model="useEchPath"
        :options="[[0, $t('tls.usePath')], [1, $t('tls.useText')]]"
        @update:model-value="switchEchMode"
      />
      <div style="flex: 1;" />
      <Btn variant="subtle" sm :loading="loading" :title="$t('actions.generate')" @click="genOpen = true">
        <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
      </Btn>
    </div>

    <Field v-if="useEchPath == 0" :label="$t('tls.keyPath')">
      <input class="input mono" v-model="ech.key_path" />
    </Field>
    <Field v-else :label="$t('tls.key')">
      <textarea class="input mono" rows="4" spellcheck="false" v-model="echKeyText"></textarea>
    </Field>

    <Field v-if="oTls.ech" :label="$t('tls.queryServerName')" :hint="hasConfig ? $t('tls.echQueryDisabled') : ''">
      <input class="input mono" v-model="oTls.ech.query_server_name" :disabled="queryLocked" placeholder="ech.example.com" />
    </Field>

    <Field :label="$t('tls.cert')">
      <textarea class="input mono" rows="4" spellcheck="false" v-model="echConfigText"></textarea>
    </Field>

    <Modal :open="genOpen" :title="$t('tls.echPublicName')" :width="520" @close="genOpen = false">
      <div style="padding: 18px; font-size: 13px; line-height: 1.7;">
        <div style="margin-bottom: 10px;">{{ $t('tls.echPublicNameAbout') }}</div>
        <div style="margin-bottom: 14px; color: var(--text-3);">{{ $t('tls.echQueryAbout') }}</div>
        <Field :label="$t('tls.echPublicName')" :mb="0">
          <input
            ref="publicNameInput"
            class="input mono"
            dir="ltr"
            v-model="publicName"
            placeholder="public.example.com"
            @keydown.enter="onPublicNameEnter"
          />
        </Field>
      </div>
      <template #footer>
        <Btn @click="genOpen = false">{{ $t('actions.close') }}</Btn>
        <Btn variant="primary" :loading="loading" :disabled="publicName.trim() == ''" @click="genECH">
          <Ico name="key" :size="14" /> {{ $t('actions.generate') }}
        </Btn>
      </template>
    </Modal>
  </div>
</template>

<script lang="ts" setup>
import { computed, nextTick, ref, watch } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { ech as echType } from '@/types/tls'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Segmented from '@/components/ui/Segmented.vue'
import Modal from '@/components/ui/Modal.vue'

const props = defineProps<{ iTls: any; oTls: any }>()

const useEchPath = ref<string | number>(props.iTls?.ech?.key ? 1 : 0)
const loading = ref(false)

const ech = computed((): echType => props.iTls.ech)

// The public name is the cleartext outer SNI baked into the generated config.
// It used to be the inbound's own server_name -- the very name ECH is there to
// hide, sent in the clear on every handshake. So it is asked for on its own,
// when generating; sing-box keeps no field for it.
const genOpen = ref(false)
const publicName = ref('')
const publicNameInput = ref<HTMLInputElement>()
watch(genOpen, (open) => { if (open) nextTick(() => publicNameInput.value?.focus()) })

// key and config are Listable[string] in sing-box: a row written by sing-box
// for a single entry, imported, or edited by hand can hold a bare string, on
// which .join/.some threw and took the whole drawer down.
const lines = (v: unknown): string[] =>
  Array.isArray(v) ? v.filter((l): l is string => typeof l == 'string')
    : typeof v == 'string' ? v.split('\n') : []

// A client with a config uses it and never looks one up, so the query name
// only matters while the config is empty.
const hasConfig = computed((): boolean => lines(props.oTls.ech?.config).some((l) => l.trim() != ''))
// Disabled only while it is empty: a row saved with both keeps a name that
// does nothing, and the operator has to be able to clear it.
const queryLocked = computed((): boolean => hasConfig.value && !props.oTls.ech?.query_server_name)

const switchEchMode = (v: string | number) => {
  if (v == 0) delete props.iTls.ech.key
  else delete props.iTls.ech.key_path
}

const echKeyText = computed({
  get: (): string => lines(ech.value?.key).join('\n'),
  set: (v: string) => { ech.value.key = v.split('\n') },
})
const echConfigText = computed({
  get: (): string => lines(props.oTls.ech?.config).join('\n'),
  set: (v: string) => { props.oTls.ech.config = v.split('\n') },
})

// keydown, not keyup, and not while an IME is composing: the Enter that picks
// a candidate would otherwise send a half-typed name.
const onPublicNameEnter = (e: KeyboardEvent) => {
  if (e.isComposing || e.keyCode == 229) return
  genECH()
}

const genECH = async () => {
  const name = publicName.value.trim()
  if (name == '' || loading.value) return
  loading.value = true
  const msg = await HttpUtils.get('api/keypairs', {
    k: 'ech',
    // The panel refuses a public name that is not a domain.
    o: name,
  })
  loading.value = false
  // A failure is success:false, already shown by HttpUtils; success has lines.
  if (msg.success && props.iTls.ech && props.oTls.ech) {
    props.iTls.ech.key_path = undefined
    useEchPath.value = 1
    const config = <string[]>[]
    const key = <string[]>[]
    let isConfig = false
    let isKey = false

    msg.obj.forEach((line: string) => {
      if (line === '-----BEGIN ECH CONFIGS-----') {
        isConfig = true
        isKey = false
        config.push(line)
      } else if (line === '-----END ECH CONFIGS-----') {
        isConfig = false
        config.push(line)
      } else if (line === '-----BEGIN ECH KEYS-----') {
        isKey = true
        isConfig = false
        key.push(line)
      } else if (line === '-----END ECH KEYS-----') {
        isKey = false
        key.push(line)
      } else if (isConfig) {
        config.push(line)
      } else if (isKey) {
        key.push(line)
      }
    })
    props.iTls.ech.key = key
    props.oTls.ech.config = config
    // Has no effect next to the config just written.
    delete props.oTls.ech.query_server_name
    genOpen.value = false
  }
}
</script>
