<template>
  <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px;">
    <SectionLabel>{{ $t('types.hysteriaRealm.users') }}</SectionLabel>
    <Btn variant="subtle" sm @click="addUser"><Ico name="plus" :size="14" /> {{ $t('actions.add') }}</Btn>
  </div>
  <MHint v-if="usersNeedAttention">{{ $t('types.hysteriaRealm.usersHint') }}</MHint>
  <div
    v-for="(user, index) in (data.users || [])"
    :key="index"
    class="card"
    style="padding: 14px; margin-bottom: 12px; background: var(--surface-2);"
  >
    <div style="display: flex; align-items: center; margin-bottom: 10px;">
      <Chip>{{ Number(index) + 1 }}</Chip>
      <div style="flex: 1;" />
      <Btn variant="subtle" icon sm :title="$t('actions.del')" @click="delUser(index)">
        <Ico name="trash" :size="14" />
      </Btn>
    </div>
    <div class="grid2">
      <!-- Chrome pairs the text field beside a password input with the password
           itself and fills both from the saved panel login, so the name would
           arrive as the operator's username and the token as their password. -->
      <Field :label="$t('types.hysteriaRealm.userName')" :mb="0">
        <input class="input mono" autocomplete="off" v-model="user.name" />
      </Field>
      <Field :label="$t('types.hysteriaRealm.userToken')" :mb="0">
        <input class="input mono" type="password" autocomplete="new-password" v-model="user.token" />
      </Field>
      <Field :label="$t('types.hysteriaRealm.maxRealms')" :mb="0">
        <input class="input mono" type="number" min="0" :value="user.max_realms ?? 0" @input="setMaxRealms(user, $event)" />
      </Field>
    </div>
  </div>

  <QuicFields :data="data" />
</template>

<script lang="ts" setup>
import { computed } from 'vue'
import Field from '@/components/ui/Field.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Chip from '@/components/ui/Chip.vue'
import MHint from '@/components/ui/MHint.vue'
import SectionLabel from '@/components/ui/SectionLabel.vue'
import QuicFields from '@/components/forms/out/QuicFields.vue'

const props = defineProps<{ data: any }>()

// sing-box rejects a blank name or token by index, as hard as it rejects an
// empty list, and saveChanges drops the rows that carry one. The hint covers
// both halves: no usable user at all, and any row still incomplete -- gating it
// on the first alone let a name typed beside an empty token be dropped on save
// with nothing on screen having said so.
const usersNeedAttention = computed(() => {
  const users = props.data.users ?? []
  return !users.some((u: any) => u?.name && u?.token) ||
    users.some((u: any) => !u?.name || !u?.token)
})

const addUser = () => {
  if (!props.data.users) props.data.users = []
  props.data.users.push({ name: '', token: '' })
}
const delUser = (i: number | string) => {
  props.data.users?.splice(Number(i), 1)
}
// 0 already means "no limit" to sing-box, so an untouched field leaves no key
// rather than storing the number that says nothing. A fraction leaves none
// either: max_realms is an int there, and json.Unmarshal refuses 2.5 outright,
// which would take the whole config down rather than this one field.
const setMaxRealms = (user: any, event: Event) => {
  const value = Number((event.target as HTMLInputElement).value)
  if (Number.isInteger(value) && value > 0) user.max_realms = value
  else delete user.max_realms
}
</script>
