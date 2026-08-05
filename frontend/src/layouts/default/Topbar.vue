<template>
  <header class="topbar">
    <Btn class="hamburger" icon @click="app.sidebarOpen = true">
      <Ico name="menu" :size="19" />
    </Btn>

    <div style="min-width: 0; flex: 0 1 auto;">
      <h1>{{ $t(`ui.title.${pageId}`) }}</h1>
      <div class="page-sub">{{ $t(`ui.sub.${pageId}`) }}</div>
    </div>

    <div class="grow" />

    <!-- Every live number on screen arrives over the push socket. When it is
         down they simply stop moving, which is indistinguishable from an idle
         panel — say so instead. -->
    <Chip v-if="wsDown" color="amber" class="ws-down" :title="$t('ws.downHint')">
      <Ico name="link" :size="14" />
      <span class="ws-down-text">{{ $t('ws.down') }}</span>
    </Chip>

    <LangMenu />

    <Pop :width="184">
      <template #trigger="{ toggle }">
        <Btn icon :title="$t('ui.theme')" @click="toggle">
          <Ico :name="themeIcon" :size="17" />
        </Btn>
      </template>
      <template #default="{ close }">
        <button
          v-for="t in THEMES"
          :key="t.value"
          type="button"
          class="pop-item"
          :class="{ active: app.theme === t.value }"
          @click="app.setTheme(t.value); close()"
        >
          <Ico :name="t.icon" :size="16" />
          <span style="flex: 1;">{{ $t(t.label) }}</span>
          <Ico v-if="app.theme === t.value" name="check" :size="15" />
        </button>
      </template>
    </Pop>

    <div class="topbar-divider" />

    <Pop :width="168">
      <template #trigger="{ toggle }">
        <button type="button" class="btn btn-ghost user-chip" @click="toggle">
          <span class="user-avatar">{{ initials }}</span>
          <span class="user-meta">
            <span class="user-name">{{ username }}</span>
          </span>
          <Ico class="user-caret" name="chevronDown" :size="14" />
        </button>
      </template>
      <template #default="{ close }">
        <button type="button" class="pop-item danger" @click="close(); doLogout()">
          <Ico name="logout" :size="15" /> {{ $t('menu.logout') }}
        </button>
      </template>
    </Pop>
  </header>
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import Btn from '@/components/ui/Btn.vue'
import Chip from '@/components/ui/Chip.vue'
import Ico from '@/components/ui/Ico.vue'
import Pop from '@/components/ui/Pop.vue'
import LangMenu from './LangMenu.vue'
import AppStore, { type Theme } from '@/store/modules/app'
import { logout } from '@/plugins/httputil'
import { wsDown } from '@/plugins/ws'

const app = AppStore()
const route = useRoute()

const pageId = computed(() => String(route.name).replace('pages.', '') || 'home')

const THEMES: { value: Theme; label: string; icon: string }[] = [
  { value: 'light', label: 'ui.themeLight', icon: 'sun' },
  { value: 'dark', label: 'ui.themeDark', icon: 'moon' },
  { value: 'system', label: 'ui.themeSystem', icon: 'monitor' },
]
const themeIcon = computed(() => THEMES.find((t) => t.value === app.theme)?.icon ?? 'moon')

const username = computed(() => localStorage.getItem('2sui-user') || 'admin')
const initials = computed(() => username.value.slice(0, 2).toUpperCase())

const doLogout = () => logout()
</script>

<style scoped>
.ws-down { gap: 6px; flex: none; }
.ws-down-text { white-space: nowrap; }
@media (max-width: 820px) {
  /* The icon alone still reads as a warning; the label is what costs room. */
  .ws-down-text { display: none; }
}
.hamburger { display: none; flex: none; }
@media (max-width: 820px) {
  .hamburger { display: inline-flex; }
}
.user-chip { gap: 9px; height: 42px; padding: 0 10px 0 6px; }
[dir='rtl'] .user-chip { padding: 0 6px 0 10px; }
.user-avatar {
  width: 28px; height: 28px; border-radius: 9px; flex: none;
  background: linear-gradient(135deg, var(--brand), var(--violet));
  display: flex; align-items: center; justify-content: center;
  color: #fff; font-weight: 700; font-size: 11.5px;
}
.user-meta { text-align: start; line-height: 1.15; }
.user-name { display: block; font-size: 13px; font-weight: 700; color: var(--text); }
@media (max-width: 820px) {
  .user-meta, .user-caret { display: none; }
  .user-chip { height: 38px; padding: 0 5px; }
}
</style>
