<template>
  <TlsDrawer
    :visible="drawer.visible"
    :id="drawer.id"
    :data="drawer.data"
    :providers="providerTags"
    @close="drawer.visible = false"
  />

  <CertProviderDrawer
    :open="providerDrawer.open"
    :index="providerDrawer.index"
    :data="providerDrawer.data"
    :tags="providerTags"
    @close="providerDrawer.open = false"
    @save="saveProvider"
  />

  <!-- delete confirmation -->
  <DeleteConfirm :open="del.visible" :loading="deleting" @close="del.visible = false" @confirm="confirmDelete" />

  <div class="page-stack fade-up">
    <div class="toolbar" style="justify-content: center;">
      <Btn variant="primary" sm @click="openDrawer(0)">
        <Ico name="plus" :size="15" /> {{ $t('actions.add') }}
      </Btn>
    </div>

    <div class="entity-grid">
      <EntityCard
        v-for="item in tlsConfigs"
        :key="item.id"
        :title="item.name"
        :type="item.server?.server_name?.length > 0 ? item.server.server_name : $t('ui.none')"
        color="var(--brand)"
        icon="tls"
        :rows="cardRows(item)"
      >
        <template #actions>
          <CardBtn icon="edit" :title="$t('actions.edit')" @click="openDrawer(item.id)" />
          <CardBtn
            v-if="tlsInbounds(item.id).length == 0"
            icon="trash"
            border
            danger
            :title="$t('actions.del')"
            @click="askDelete(item.id)"
          />
          <CardBtn icon="clone" border :title="$t('actions.clone')" @click="clone(item)" />
        </template>
      </EntityCard>
    </div>

    <!-- Certificate providers live in the base config rather than the TLS
         table, but they exist only to serve the configs above, so they are
         managed here. A TLS config points at one by tag. -->
    <div class="toolbar" style="justify-content: center; margin-top: 8px;">
      <span style="font-size: 13px; color: var(--text-2); margin-inline-end: 10px;">
        {{ $t('tls.provider.title') }}
      </span>
      <Btn variant="primary" sm :loading="providerSaving" @click="openProviderDrawer(-1)">
        <Ico name="plus" :size="15" /> {{ $t('actions.add') }}
      </Btn>
    </div>

    <div class="entity-grid">
      <EntityCard
        v-for="(item, index) in providers"
        :key="item.tag"
        :title="item.tag"
        :type="providerTypeName(item.type)"
        color="var(--amber)"
        icon="tls"
        :rows="providerRows(item)"
      >
        <template #actions>
          <CardBtn icon="edit" :title="$t('actions.edit')" @click="openProviderDrawer(index)" />
          <!-- Deleting a provider a TLS config still references would leave a
               dangling tag, which stops the core from starting. -->
          <CardBtn
            v-if="providerUsers(item.tag).length == 0"
            icon="trash"
            border
            danger
            :title="$t('actions.del')"
            @click="askDeleteProvider(index)"
          />
          <CardBtn icon="clone" border :title="$t('actions.clone')" @click="cloneProvider(index)" />
        </template>
      </EntityCard>
    </div>
  </div>
</template>

<script lang="ts" setup>
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Data from '@/store/modules/data'
import { Inbound } from '@/types/inbounds'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import DeleteConfirm from '@/components/ui/DeleteConfirm.vue'
import CardBtn from '@/components/ui/CardBtn.vue'
import EntityCard, { EntityRow } from '@/components/ui/EntityCard.vue'
import TlsDrawer from '@/layouts/drawers/tls/TlsDrawer.vue'
import CertProviderDrawer from '@/layouts/drawers/tls/CertProviderDrawer.vue'
import { certProvider } from '@/types/tls'

const { t } = useI18n({ useScope: 'global' })
const dataStore = Data()

// ---------------- store data ----------------
const tlsConfigs = computed((): any[] => dataStore.tlsConfigs)

const inbounds = computed((): Inbound[] => dataStore.inbounds)

const tlsInbounds = (id: number): string[] =>
  inbounds.value.filter((i) => i.tls_id == id).map((i) => i.tag)

// ---------------- card rows ----------------
const cardRows = (item: any): EntityRow[] => [
  {
    k: t('pages.inbounds'),
    v: tlsInbounds(item.id).length > 0 ? tlsInbounds(item.id).length : t('ui.none'),
  },
  {
    k: t('tls.provider.title'),
    v: item.server?.certificate_provider ?? t('ui.none'),
    color: item.server?.certificate_provider ? 'var(--emerald)' : undefined,
  },
  { k: 'ECH', v: t(item.server?.ech == undefined ? 'no' : 'yes') },
  {
    k: 'Reality',
    v: t(item.server?.reality == undefined ? 'no' : 'yes'),
    color: item.server?.reality != undefined ? 'var(--brand)' : undefined,
  },
]

// ---------------- drawer / clone ----------------
const drawer = ref({ visible: false, id: 0, data: '' })
const openDrawer = (id: number) => {
  drawer.value.id = id
  drawer.value.data = id == 0 ? '{}' : JSON.stringify(tlsConfigs.value.findLast((c) => c.id == id))
  drawer.value.visible = true
}

const clone = async (obj: any) => {
  const data = JSON.parse(JSON.stringify(obj))
  data.id = 0
  while (tlsConfigs.value.findIndex((c) => c.name == data.name) != -1) {
    data.name += '-copy'
  }
  await Data().save('tls', 'new', data)
}

// ---------------- delete (with confirm) ----------------
const del = ref({ visible: false, id: 0 })
const deleting = ref(false)
const askDelete = (id: number) => {
  del.value = { visible: true, id }
}
const confirmDelete = async () => {
  // A provider delete borrows the same confirmation dialog; -1 is the id no TLS
  // config can have, so it is what marks the pending delete as a provider's.
  if (del.value.id === -1) {
    deleting.value = true
    const ok = await removeProvider(pendingProvider.value)
    if (ok) del.value.visible = false
    deleting.value = false
    return
  }
  if (del.value.id === 0) return
  deleting.value = true
  const success = await Data().save('tls', 'del', del.value.id)
  if (success) del.value.visible = false
  deleting.value = false
}

// ---------------- certificate providers ----------------
// They live in the base config, so every change is written back through the
// config object as a whole, the same way the rules page does it.
const appConfig = computed((): any => dataStore.config)

const providers = computed((): certProvider[] => {
  const config = appConfig.value
  if (!Array.isArray(config.certificate_providers)) config.certificate_providers = []
  return config.certificate_providers
})

const providerTags = computed((): string[] => providers.value.map((p) => p.tag))

const providerUsers = (tag: string): string[] =>
  tlsConfigs.value.filter((c) => c.server?.certificate_provider == tag).map((c) => c.name)

const providerTypeNames: Record<string, string> = {
  acme: 'ACME',
  tailscale: 'Tailscale',
  'cloudflare-origin-ca': 'Cloudflare Origin CA',
}
const providerTypeName = (type: string): string => providerTypeNames[type] ?? type

const providerRows = (item: any): EntityRow[] => [
  { k: t('rule.domain'), v: item.domain?.length > 0 ? item.domain.length : t('ui.none') },
  {
    k: t('objects.tls'),
    v: providerUsers(item.tag).length > 0 ? providerUsers(item.tag).length : t('ui.none'),
  },
]

const providerSaving = ref(false)
const providerDrawer = ref({ open: false, index: -1, data: '' })
const pendingProvider = ref(-1)

const openProviderDrawer = (index: number) => {
  providerDrawer.value.index = index
  providerDrawer.value.data = index == -1 ? '' : JSON.stringify(providers.value[index])
  providerDrawer.value.open = true
}

const saveProviders = async (): Promise<boolean> => {
  providerSaving.value = true
  const success = await Data().save('config', 'set', appConfig.value)
  providerSaving.value = false
  return success
}

const saveProvider = async (data: certProvider) => {
  const index = providerDrawer.value.index
  if (index == -1) providers.value.push(data)
  else providers.value[index] = data
  const success = await saveProviders()
  if (success) providerDrawer.value.open = false
}

const cloneProvider = async (index: number) => {
  const copy = <certProvider>JSON.parse(JSON.stringify(providers.value[index]))
  while (providerTags.value.includes(copy.tag)) copy.tag += '-copy'
  providers.value.push(copy)
  await saveProviders()
}

const askDeleteProvider = (index: number) => {
  pendingProvider.value = index
  del.value = { visible: true, id: -1 }
}

const removeProvider = async (index: number): Promise<boolean> => {
  if (index < 0) return false
  providers.value.splice(index, 1)
  return await saveProviders()
}
</script>
