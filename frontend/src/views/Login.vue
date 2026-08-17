<template>
  <div class="login-shell">
    <!-- brand side (desktop only) -->
    <div class="login-brand">
      <div class="login-brand-glow" />
      <div style="position: relative; display: flex; align-items: center; gap: 12px;">
        <Logo :size="40" />
        <div style="font-weight: 800; font-size: 20px; letter-spacing: -.02em;">2S<span style="opacity: .6;">-UI</span></div>
      </div>
      <div style="position: relative;">
        <div style="font-size: 34px; font-weight: 800; letter-spacing: -.03em; line-height: 1.12; max-width: 420px;">{{ $t('ui.heroTitle') }}</div>
        <div style="font-size: 15px; color: rgba(255,255,255,.65); margin-top: 16px; max-width: 400px; line-height: 1.6;">{{ $t('ui.heroDesc') }}</div>
      </div>
      <div style="position: relative; font-size: 12.5px; color: rgba(255,255,255,.4);">sing-box · © {{ new Date().getFullYear() }} 2S-UI</div>
    </div>

    <!-- form side -->
    <div class="login-form-side hide-scroll">
      <div class="login-corner">
        <LangMenu />
        <Btn icon @click="app.toggleTheme()">
          <Ico :name="app.theme === 'dark' ? 'sun' : 'moon'" :size="17" />
        </Btn>
      </div>

      <div class="fade-up" style="width: 100%; max-width: 360px;">
        <div class="login-mobile-logo">
          <Logo :size="38" />
          <div style="font-weight: 800; font-size: 19px; letter-spacing: -.02em; white-space: nowrap;">2S<span style="color: var(--text-3);">-UI</span></div>
        </div>

        <div style="font-size: 24px; font-weight: 800; letter-spacing: -.02em;">{{ $t('ui.welcomeBack') }}</div>
        <div style="font-size: 14px; color: var(--text-3); margin-top: 6px; margin-bottom: 28px;">{{ $t('ui.signInPanel') }}</div>

        <form @submit.prevent="login">
          <Field :label="$t('ui.username')" :mb="16">
            <input class="input" v-model="username" name="username" autocomplete="username" autofocus />
          </Field>
          <Field :label="$t('ui.password')" :mb="twoFa ? 16 : 22">
            <input class="input" v-model="password" name="password" type="password" autocomplete="current-password" />
          </Field>
          <!-- Only after the backend says this account has a second factor, so
               the field never appears for panels that do not use one. -->
          <Field v-if="twoFa" :label="$t('ui.twoFaCode')" :mb="22">
            <input
              ref="codeInput"
              class="input mono"
              v-model="code"
              name="one-time-code"
              inputmode="numeric"
              autocomplete="one-time-code"
              maxlength="6"
              placeholder="000000"
            />
          </Field>
          <Btn variant="primary" type="submit" :loading="loading" style="width: 100%; height: 44px;">
            {{ $t('ui.signIn') }} <Ico name="chevron" :size="16" />
          </Btn>
        </form>

        <div style="font-size: 12px; color: var(--text-3); text-align: center; margin-top: 22px;">{{ $t('ui.protectedArea') }}</div>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { ref, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import HttpUtil from '@/plugins/httputil'
import Logo from '@/components/ui/Logo.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Field from '@/components/ui/Field.vue'
import LangMenu from '@/layouts/default/LangMenu.vue'
import AppStore from '@/store/modules/app'

const app = AppStore()
const router = useRouter()

const username = ref('')
const password = ref('')
const code = ref('')
const twoFa = ref(false)
const codeInput = ref<HTMLInputElement | null>(null)
const loading = ref(false)

const login = async () => {
  // The submit button is disabled while a request is in flight, but pressing
  // Enter in a field submits the form anyway. Resending the same code is not
  // harmless: the first request burns it, so the second is refused as a replay
  // and counts against the login rate limit.
  if (loading.value) return
  if (username.value == '' || password.value == '') return
  loading.value = true
  const response = await HttpUtil.post('api/login', {
    user: username.value,
    pass: password.value,
    code: code.value,
  })
  if (response.success) {
    localStorage.setItem('2sui-user', username.value)
    setTimeout(() => {
      loading.value = false
      router.push('/')
    }, 500)
    return
  }

  loading.value = false
  // The password was right and a code is what is missing. The backend only
  // ever answers this once the password has checked out, so revealing the
  // field here does not tell an anonymous caller anything.
  if (response.obj?.twoFa) {
    twoFa.value = true
    code.value = ''
    nextTick(() => codeInput.value?.focus())
  }
}
</script>

<style scoped>
.login-shell {
  height: 100vh;
  min-height: 100dvh;
  display: grid;
  grid-template-columns: 1fr 1fr;
  background: var(--bg);
}
.login-brand {
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  padding: 48px;
  background: linear-gradient(150deg, #1a1640, #0b0d14 55%);
  color: #fff;
}
.login-brand-glow {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(700px 500px at 20% 10%, rgba(99, 91, 255, .4), transparent 60%),
    radial-gradient(600px 500px at 90% 90%, rgba(27, 199, 216, .28), transparent 60%);
}
.login-form-side {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px;
  position: relative;
  overflow-y: auto;
}
.login-corner {
  position: absolute;
  top: 26px;
  inset-inline-end: 26px;
  display: flex;
  gap: 8px;
  align-items: center;
}
.login-mobile-logo { display: none; }
@media (max-width: 820px) {
  .login-shell { grid-template-columns: 1fr; }
  .login-brand { display: none; }
  .login-form-side { padding: 84px 24px 40px; }
  .login-corner { inset-inline-end: 18px; }
  .login-mobile-logo {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 30px;
  }
}
</style>
