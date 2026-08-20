<template>
  <Modal :open="open" :title="$t('admin.twoFa.title')" :width="480" @close="$emit('close')">
    <div style="padding: 18px 20px;">
      <!-- already on: the only thing offered is turning it off -->
      <div v-if="enabled">
        <div class="tfa-note">
          <Ico name="check" :size="15" />
          <span>{{ $t('admin.twoFa.enabledMsg') }}</span>
        </div>
        <Field :label="$t('admin.oldPass')" :hint="$t('admin.twoFa.disableHint')" :mb="0">
          <input class="input" v-model="password" type="password" autocomplete="current-password" />
        </Field>
      </div>

      <!-- enrolment -->
      <div v-else-if="secret">
        <div style="display: flex; justify-content: center; margin-bottom: 14px;">
          <!-- Rendered locally from the URI; the secret never goes to a QR service. -->
          <QrcodeVue :value="uri" :size="180" :margin="1" style="border-radius: .5rem;" />
        </div>
        <div class="tfa-note">{{ $t('admin.twoFa.scanMsg') }}</div>
        <Field :label="$t('admin.twoFa.secret')" :mb="14">
          <div style="display: flex; gap: 8px; align-items: center;">
            <input
              class="input mono"
              style="flex: 1;"
              readonly
              :value="secret"
              @focus="($event.target as HTMLInputElement).select()"
            />
            <Btn variant="subtle" icon :title="$t('copyToClipboard')" @click="copyToClipboard(secret)">
              <Ico name="copy" :size="16" />
            </Btn>
          </div>
        </Field>
        <Field :label="$t('ui.twoFaCode')" :hint="$t('admin.twoFa.confirmHint')" :mb="14">
          <input
            class="input mono"
            v-model="code"
            inputmode="numeric"
            maxlength="6"
            placeholder="000000"
            @keyup.enter="enable"
          />
        </Field>
        <Field :label="$t('admin.oldPass')" :hint="$t('admin.twoFa.enableHint')" :mb="0">
          <input class="input" v-model="password" type="password" autocomplete="current-password" @keyup.enter="enable" />
        </Field>
      </div>

      <div v-else style="padding: 20px 0;">
        <EmptyState icon="globe" :title="$t('loading')" />
      </div>
    </div>

    <template #footer>
      <Btn sm @click="$emit('close')">{{ $t('actions.close') }}</Btn>
      <Btn v-if="enabled" variant="primary" sm :loading="loading" :disabled="!password" @click="disable">
        {{ $t('admin.twoFa.disable') }}
      </Btn>
      <Btn v-else variant="primary" sm :loading="loading" :disabled="code.length < 6 || !password" @click="enable">
        {{ $t('admin.twoFa.enable') }}
      </Btn>
    </template>
  </Modal>
</template>

<script lang="ts" setup>
import { ref, watch } from 'vue'
import QrcodeVue from 'qrcode.vue'
import HttpUtils from '@/plugins/httputil'
import { copyToClipboard } from '@/plugins/clipboard'
import Modal from '@/components/ui/Modal.vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import EmptyState from '@/components/ui/EmptyState.vue'

const props = defineProps<{
  open: boolean
  enabled: boolean
}>()
const emit = defineEmits<{ close: []; saved: [] }>()

const secret = ref('')
const uri = ref('')
const code = ref('')
const password = ref('')
const loading = ref(false)

// Minted per opening, and never stored server-side until a code from it works,
// so closing the modal half way through simply discards it.
const loadSetup = async () => {
  secret.value = ''
  uri.value = ''
  const msg = await HttpUtils.post('api/twoFaSetup', {})
  if (msg.success) {
    secret.value = msg.obj?.secret ?? ''
    uri.value = msg.obj?.uri ?? ''
  }
}

const enable = async () => {
  if (code.value.length < 6 || !password.value) return
  loading.value = true
  const msg = await HttpUtils.post('api/twoFaEnable', {
    pass: password.value,
    secret: secret.value,
    code: code.value,
  })
  loading.value = false
  if (msg.success) {
    emit('saved')
    emit('close')
  }
}

const disable = async () => {
  if (!password.value) return
  loading.value = true
  const msg = await HttpUtils.post('api/twoFaDisable', { pass: password.value })
  loading.value = false
  if (msg.success) {
    emit('saved')
    emit('close')
  }
}

watch(() => props.open, (open) => {
  code.value = ''
  password.value = ''
  if (!open) {
    secret.value = ''
    uri.value = ''
    return
  }
  if (!props.enabled) loadSetup()
})
</script>

<style scoped>
.tfa-note {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12.5px;
  color: var(--text-3);
  line-height: 1.55;
  margin-bottom: 14px;
}
</style>
