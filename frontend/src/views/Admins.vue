<template>
  <AdminModal
    :open="editModal.visible"
    :user="editModal.user"
    :loading="loading"
    @close="closeEditModal"
    @save="saveEditModal"
  />
  <ChangesModal
    :open="changesModal.visible"
    :admins="users.map((u: any) => u.username)"
    :actor="changesModal.actor"
    @close="closeChangesModal"
  />
  <TokenModal :open="tokenModal.visible" @close="closeTokenModal" />
  <TwoFaModal
    :open="twoFaModal.visible"
    :enabled="twoFaModal.enabled"
    @close="twoFaModal.visible = false"
    @saved="loadData"
  />

  <div class="page-stack-lg fade-up">
    <!-- toolbar -->
    <div class="toolbar" style="justify-content: center;">
      <Btn variant="primary" sm @click="showChangesModal('')">{{ $t('ui.changes') }}</Btn>
      <Btn variant="primary" sm @click="showTokenModal()">{{ $t('ui.apiToken') }}</Btn>
    </div>

    <!-- admin cards -->
    <div class="entity-grid">
      <div v-for="item in users" :key="item.id" class="card admin-card">
        <div class="admin-head">
          <Avatar :name="item.username" :size="36" />
          <div style="flex: 1; min-width: 0;">
            <div style="font-weight: 700; font-size: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;">{{ item.username }}</div>
            <div style="font-size: 11.5px; color: var(--text-3);">{{ $t('ui.lastLogin') }}</div>
          </div>
          <Chip v-if="item.twoFa" color="emerald">{{ $t('admin.twoFa.short') }}</Chip>
        </div>
        <div class="admin-rows">
          <div class="kv-row">
            <span class="k">{{ $t('admin.date') }}</span>
            <span class="v mono">{{ item.loginDate }}</span>
          </div>
          <div class="kv-row">
            <span class="k">{{ $t('admin.time') }}</span>
            <span class="v mono">{{ item.loginTime }}</span>
          </div>
          <div class="kv-row">
            <span class="k">IP</span>
            <span class="v mono">{{ item.ip }}</span>
          </div>
        </div>
        <div class="admin-actions">
          <CardBtn icon="edit" :title="$t('actions.edit')" @click="showEditModal(item)" />
          <CardBtn
            icon="shield"
            border
            :disabled="currentUser !== '' && item.username !== currentUser"
            :title="$t('admin.twoFa.title')"
            @click="showTwoFaModal(item)"
          />
          <CardBtn icon="list" border :title="$t('ui.changes')" @click="showChangesModal(item.username)" />
        </div>
      </div>
    </div>
  </div>
</template>

<script lang="ts" setup>
import AdminModal from '@/layouts/drawers/admin/AdminModal.vue'
import ChangesModal from '@/layouts/drawers/admin/ChangesModal.vue'
import TokenModal from '@/layouts/drawers/admin/TokenModal.vue'
import TwoFaModal from '@/layouts/drawers/admin/TwoFaModal.vue'
import { intlLocale } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { ref, onMounted } from 'vue'
import Btn from '@/components/ui/Btn.vue'
import Avatar from '@/components/ui/Avatar.vue'
import CardBtn from '@/components/ui/CardBtn.vue'
import Chip from '@/components/ui/Chip.vue'

const loading = ref(false)

const users = ref(<any[]>[])

onMounted(async () => {
  loading.value = true
  await loadData()
  loading.value = false
})

const loadData = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/users')
  loading.value = false
  if (msg.success) {
    users.value = []
    msg.obj.forEach((u: any) => {
      const lastLogin = u.lastLogin.split(" ")
      const localLastLogin = lastLogin.length > 2 ? dateFormatted(Date.parse(lastLogin[0] + " " + lastLogin[1])) : "- -"
      const loginDateTime = localLastLogin.split(" ")
      users.value.push({
        id: u.id,
        username: u.username,
        twoFa: u.twoFa === true,
        loginDate: loginDateTime[0],
        loginTime: loginDateTime[1],
        ip: lastLogin[2] ?? "-",
      })
    })
  }
}

const dateFormatted = (dt: number): string => {
  const date = new Date(dt)
  return date.toLocaleString(intlLocale())
}

const editModal = ref({
  visible: false,
  user: <any>{},
})

const showEditModal = (user: any) => {
  editModal.value.user = user
  editModal.value.visible = true
}
const closeEditModal = () => {
  editModal.value.visible = false
  editModal.value.user = {}
}
const saveEditModal = async (data: any) => {
  loading.value = true
  const response = await HttpUtils.post('api/changePass', data)
  if (response.success) {
    setTimeout(() => {
      loading.value = false
      editModal.value.visible = false
    }, 500)
  } else {
    loading.value = false
  }
}

const changesModal = ref({
  visible: false,
  actor: '',
})
const showChangesModal = (actor: string) => {
  changesModal.value.actor = actor
  changesModal.value.visible = true
}
const closeChangesModal = () => {
  changesModal.value.visible = false
  changesModal.value.actor = ''
}

// Enrolment always applies to the logged-in account — the backend reads the
// session rather than a user id — so opening this for a different row would
// write the secret to the wrong account with nothing on screen saying so.
// There is only ever one admin today, but the page renders a list, hence the
// button being disabled on every row but your own.
//
// This key is written at login and can go missing on its own — a cleared
// localStorage leaves the session cookie intact — so an empty value means "no
// idea who you are" and disables nothing. Locking every row would be worse:
// with one admin it makes 2FA unreachable, and unlike the mismatch case there
// is nothing on screen to explain it.
const currentUser = localStorage.getItem('2sui-user') ?? ''
const twoFaModal = ref({
  visible: false,
  enabled: false,
})
const showTwoFaModal = (user: any) => {
  twoFaModal.value.enabled = user.twoFa === true
  twoFaModal.value.visible = true
}

const tokenModal = ref({
  visible: false,
})
const showTokenModal = () => {
  tokenModal.value.visible = true
}
const closeTokenModal = () => {
  tokenModal.value.visible = false
}
</script>

<style scoped>
.admin-card { padding: 0; overflow: hidden; display: flex; flex-direction: column; }
.admin-head { padding: 14px 15px 4px; display: flex; align-items: center; gap: 11px; }
.admin-rows { padding: 8px 15px 12px; display: flex; flex-direction: column; }
.admin-actions { display: flex; border-top: 1px solid var(--line); margin-top: auto; }
</style>
