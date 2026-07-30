<template>
  <EditorModal
    :open="jsonEditor"
    :title="$t('editor') + ' - ' + $t('setting.jsonSub')"
    :content="settings.subJsonExt"
    @save="saveJsonEditor"
    @close="jsonEditor = false"
  />
  <EditorModal
    :open="clashEditor"
    :title="$t('editor') + ' - ' + $t('setting.clashSub')"
    :content="settings.subClashExt"
    @save="saveClashEditor"
    @close="clashEditor = false"
  />

  <div class="page-stack-lg fade-up" style="max-width: 1040px;">
    <!-- page tabs + actions -->
    <div class="head-row">
      <Tabs v-model="tab" page :mb="0" :tabs="tabItems" style="border-bottom: none; flex: 1 1 auto; min-width: 0;" />
      <!-- 证书页的每个操作都立即生效，没有待保存的表单，摆着保存/重启只会让人以为
           申请完证书还得再点一下 -->
      <div v-if="tab !== 'certs'" class="head-actions">
        <Btn variant="primary" sm :loading="loading" :disabled="!stateChange || proxyBlocked" @click="save">
          <Ico name="check" :size="15" /> {{ $t('actions.save') }}
        </Btn>
        <Pop :min-width="210">
          <template #trigger="{ toggle }">
            <Btn sm style="color: var(--amber);" :loading="loading" :disabled="stateChange" @click="toggle">
              <Ico name="refresh" :size="15" /> {{ $t('actions.restartApp') }}
            </Btn>
          </template>
          <template #default="{ close }">
            <div style="padding: 8px 10px 4px; font-size: 13px; font-weight: 700;">{{ $t('actions.restartApp') }}</div>
            <div style="padding: 0 10px 8px; font-size: 12.5px; color: var(--text-3);">{{ $t('confirm') }}</div>
            <div style="display: flex; gap: 6px; padding: 2px;">
              <Btn sm style="flex: 1; color: var(--amber);" @click="close(); restartApp();">{{ $t('yes') }}</Btn>
              <Btn variant="subtle" sm style="flex: 1;" @click="close()">{{ $t('no') }}</Btn>
            </div>
          </template>
        </Pop>
      </div>
    </div>

    <!-- ===================== Domains & certificates ===================== -->
    <CertsPanel v-if="tab === 'certs'" :initial-domain="issueDomain" :proxied-domains="proxiedDomains" />

    <!-- ===================== Interface ===================== -->
    <SettingsGroup v-else-if="tab === 'interface'" grid>
      <!-- 域名决定用哪份证书:选中的域名有证书就自动用它,证书路径不再手填 -->
      <SRow :label="$t('setting.domain')" :hint="$t('setting.domainHint')">
        <DomainInput v-model="settings.webDomain" placeholder="panel.example.com" @issue="goIssue" />
        <div v-if="webCertNote" class="fieldnote" :class="webCertNote.kind">
          <span>{{ webCertNote.text }}</span>
          <a v-if="webCertNote.offerIssue" role="button" tabindex="0"
             @click="goIssue(settings.webDomain)" @keyup.enter="goIssue(settings.webDomain)">{{ $t('setting.certGoIssue') }}</a>
        </div>
      </SRow>
      <SRow :label="$t('setting.webPath')">
        <input class="input mono" v-model="settings.webPath" />
      </SRow>
      <SRow :label="$t('setting.addr')">
        <input class="input mono" v-model="settings.webListen" placeholder="0.0.0.0" />
      </SRow>
      <SRow :label="$t('setting.port')">
        <input class="input mono" type="number" min="1" v-model.number="webPort" />
      </SRow>
      <ToggleRow v-model="webBehindProxy" :label="$t('setting.behindProxy')" :desc="behindProxyDesc" />
      <SRow :label="$t('setting.webUri')" :hint="webUriHint">
        <input class="input mono" v-model="settings.webURI" placeholder="https://panel.example.com/app/" />
      </SRow>
      <SRow :label="$t('setting.sessionAge')" :hint="$t('date.m')">
        <input class="input mono" type="number" min="0" v-model.number="sessionMaxAge" />
      </SRow>
      <SRow :label="$t('setting.trafficAge')" :hint="$t('date.d')">
        <input class="input mono" type="number" min="0" v-model.number="trafficAge" />
      </SRow>
      <SRow :label="$t('setting.timeLoc')">
        <input class="input mono" v-model="settings.timeLocation" />
      </SRow>
    </SettingsGroup>

    <!-- ===================== Subscription ===================== -->
    <SettingsGroup v-else-if="tab === 'sub'" grid>
      <!-- 订阅链接会发给客户端,很多人不想让它暴露面板域名——这里可以填另一个域名,
           后端会为它单独生成一份 vhost;与面板同域名时则合并成一份、两个 location -->
      <SRow :label="$t('setting.domain')" :hint="$t('setting.subDomainHint')">
        <DomainInput v-model="settings.subDomain" placeholder="sub.example.com" @issue="goIssue" />
        <div v-if="subCertNote" class="fieldnote" :class="subCertNote.kind">
          <span>{{ subCertNote.text }}</span>
          <a v-if="subCertNote.offerIssue" role="button" tabindex="0"
             @click="goIssue(settings.subDomain)" @keyup.enter="goIssue(settings.subDomain)">{{ $t('setting.certGoIssue') }}</a>
        </div>
      </SRow>
      <SRow :label="$t('setting.path')">
        <input class="input mono" v-model="settings.subPath" />
      </SRow>
      <SRow :label="$t('setting.addr')">
        <input class="input mono" v-model="settings.subListen" placeholder="0.0.0.0" />
      </SRow>
      <SRow :label="$t('setting.port')">
        <input class="input mono" type="number" min="1" v-model.number="subPort" />
      </SRow>
      <ToggleRow v-model="subBehindProxy" :label="$t('setting.behindProxy')" :desc="subBehindProxyDesc" />
      <SRow :label="$t('setting.subUri')">
        <input class="input mono" v-model="settings.subURI" placeholder="https://sub.example.com/sub/" />
      </SRow>
      <ToggleRow v-model="subEncode" :label="$t('setting.subEncode')" />
      <ToggleRow v-model="subShowInfo" :label="$t('setting.subInfo')" />
      <SRow :label="$t('setting.update')" :hint="$t('date.h')">
        <input class="input mono" type="number" min="0" v-model.number="subUpdates" />
      </SRow>
    </SettingsGroup>

    <!-- ===================== JSON sub ===================== -->
    <SettingsGroup v-else-if="tab === 'jsonSub'">
      <div style="font-size: 12.5px; color: var(--text-3); padding: 12px 0 14px;">{{ $t('ui.jsonExtDesc') }}</div>

      <div class="field-grid">
        <ChipSelect v-model="ruleToDirect" :options="geoList" :label="$t('setting.toDirect')" :placeholder="$t('ui.selectHint')" />
        <ChipSelect v-model="ruleToBlock" :options="geoList" :label="$t('setting.toBlock')" :placeholder="$t('ui.selectHint')" />
      </div>

      <template v-if="enableLog">
        <div class="sub-label">{{ $t('basic.log.title') }}</div>
        <div class="field-grid">
          <Field :label="$t('basic.log.level')" :mb="0">
            <Select v-model="subJsonExt.log.level">
              <option v-for="l in levels" :key="l" :value="l">{{ l }}</option>
            </Select>
          </Field>
          <div style="display: flex; align-items: flex-end; padding-bottom: 6px;">
            <SwitchLabel v-model="subJsonExt.log.timestamp" :label="$t('setting.timestamp')" />
          </div>
        </div>
      </template>

      <template v-if="enableDns">
        <div class="sub-label">{{ $t('ui.dnsOpt') }}</div>
        <div class="field-grid">
          <Field :label="$t('dns.final')" :mb="0">
            <Select v-model="subJsonExt.dns.final">
              <option v-for="tg in dnsTags" :key="tg" :value="tg">{{ tg }}</option>
            </Select>
          </Field>
          <Field :label="$t('basic.routing.defaultDns')" :mb="0">
            <Select v-model="defaultResolver">
              <option value="">{{ $t('ui.none') }}</option>
              <option v-for="tg in dnsTags" :key="tg" :value="tg">{{ tg }}</option>
            </Select>
          </Field>
        </div>
        <div class="grid2" style="margin-top: 14px;">
          <Field :label="$t('setting.globalDns')" :mb="0">
            <div style="display: flex; gap: 8px;">
              <Select style="flex: 1; min-width: 0;" :model-value="proxyDns.type" @change="setDnsType(proxyDns, $event)">
                <option v-for="dt in dnsTypes" :key="dt" :value="dt">{{ dt }}</option>
              </Select>
              <template v-if="proxyDns.type !== 'local'">
                <input class="input mono" style="flex: 1.5; min-width: 0;" v-model="proxyDns.server" :placeholder="$t('in.addr')" />
                <input class="input mono" style="width: 76px; flex: none;" type="number" min="1" v-model.number="proxyDns.server_port" :placeholder="$t('in.port')" />
              </template>
            </div>
          </Field>
          <Field :label="$t('setting.directDns')" :mb="0">
            <div style="display: flex; gap: 8px;">
              <Select style="flex: 1; min-width: 0;" :model-value="directDns.type" @change="setDnsType(directDns, $event)">
                <option v-for="dt in dnsTypes" :key="dt" :value="dt">{{ dt }}</option>
              </Select>
              <template v-if="directDns.type !== 'local'">
                <input class="input mono" style="flex: 1.5; min-width: 0;" v-model="directDns.server" :placeholder="$t('in.addr')" />
                <input class="input mono" style="width: 76px; flex: none;" type="number" min="1" v-model.number="directDns.server_port" :placeholder="$t('in.port')" />
              </template>
            </div>
          </Field>
        </div>
        <div style="margin-top: 14px;">
          <ChipSelect v-model="dnsToDirect" :options="geositeList" :label="$t('setting.toDirectDns')" :placeholder="$t('ui.selectHint')" />
        </div>
      </template>

      <template v-if="enableInb">
        <div class="sub-label">{{ $t('objects.inbound') }}</div>
        <div class="field-grid">
          <Field :label="$t('in.addr') + ' ' + $t('commaSeparated')" :mb="0">
            <input class="input mono" v-model="tunAddress" />
          </Field>
          <Field :label="$t('ui.mtu')" :mb="0">
            <input class="input mono" type="number" min="0" v-model.number="subJsonExt.inbounds[0].mtu" />
          </Field>
          <Field :label="$t('setting.excludePkg') + ' ' + $t('commaSeparated')" :mb="0">
            <input class="input mono" v-model="tunExcludePkg" />
          </Field>
          <div style="display: flex; align-items: flex-end; padding-bottom: 6px;">
            <SwitchLabel v-model="platformProxy" :label="$t('ui.platformProxy')" />
          </div>
        </div>
      </template>

      <div class="builder-foot">
        <div style="flex: 1;" />
        <Btn sm @click="jsonEditor = true"><Ico name="edit" :size="14" /> {{ $t('editor') }}</Btn>
        <Pop :min-width="220" direction="up">
          <template #trigger="{ toggle }">
            <Btn variant="subtle" sm @click="toggle">{{ $t('setting.jsonSubOptions') }} <Ico name="chevronDown" :size="14" /></Btn>
          </template>
          <label class="pop-item" @click.prevent="enableLog = !enableLog"><Toggle :model-value="enableLog" style="pointer-events: none;" /> {{ $t('basic.log.title') }}</label>
          <label class="pop-item" @click.prevent="enableDns = !enableDns"><Toggle :model-value="enableDns" style="pointer-events: none;" /> {{ $t('ui.dnsOpt') }}</label>
          <label class="pop-item" @click.prevent="enableInb = !enableInb"><Toggle :model-value="enableInb" style="pointer-events: none;" /> {{ $t('objects.inbound') }}</label>
          <label class="pop-item" @click.prevent="enableExp = !enableExp"><Toggle :model-value="enableExp" style="pointer-events: none;" /> {{ $t('ui.experimental') }}</label>
        </Pop>
      </div>
    </SettingsGroup>

    <!-- ===================== Clash sub ===================== -->
    <SettingsGroup v-else>
      <div style="font-size: 12.5px; color: var(--text-3); padding: 12px 0 14px;">{{ $t('ui.clashExtDesc') }}</div>

      <div style="display: flex; gap: 26px; padding-bottom: 16px; flex-wrap: wrap;">
        <SwitchLabel v-model="clashNoDefGrp" :label="$t('setting.clashNoDefGrp')" />
        <SwitchLabel v-model="clashSprtAll" :label="$t('setting.clashSprtAll')" />
      </div>

      <div class="field-grid">
        <template v-if="optionMixed">
          <Field :label="$t('setting.mixedPort')" :mb="0">
            <input class="input mono" type="number" min="1" max="65535" v-model.number="mixedPort" />
          </Field>
          <div style="display: flex; align-items: flex-end; padding-bottom: 6px;">
            <SwitchLabel v-model="allowLan" :label="$t('types.ts.allowLanAccess')" />
          </div>
        </template>
        <Field v-if="optionExt" :label="$t('basic.exp.extController')" :mb="0">
          <input class="input mono" v-model="externalController" />
        </Field>
        <Field v-if="optionLog" :label="$t('basic.log.title') + ' - ' + $t('basic.log.level')" :mb="0">
          <Select v-model="clashLogLevel">
            <option v-for="l in clashLevels" :key="l" :value="l">{{ l }}</option>
          </Select>
        </Field>
      </div>

      <div v-if="optionTun || optionDns" style="display: flex; gap: 26px; margin-top: 16px; flex-wrap: wrap;">
        <SwitchLabel v-if="optionTun" v-model="clashTun" :label="$t('setting.tun')" />
        <SwitchLabel v-if="optionDns" v-model="clashDns" :label="$t('pages.dns')" />
      </div>

      <div v-if="optionRules" style="margin-top: 16px;">
        <ChipSelect v-model="clashRules" :options="rulesIP" :label="$t('pages.rules')" :placeholder="$t('ui.selectHint')" />
      </div>

      <div class="builder-foot">
        <div style="flex: 1;" />
        <Btn sm @click="clashEditor = true"><Ico name="edit" :size="14" /> {{ $t('editor') }}</Btn>
        <Pop :min-width="220" direction="up">
          <template #trigger="{ toggle }">
            <Btn variant="subtle" sm @click="toggle">{{ $t('setting.jsonSubOptions') }} <Ico name="chevronDown" :size="14" /></Btn>
          </template>
          <label class="pop-item" @click.prevent="optionMixed = !optionMixed"><Toggle :model-value="optionMixed" style="pointer-events: none;" /> {{ $t('setting.mixedPort') }}</label>
          <label class="pop-item" @click.prevent="optionTun = !optionTun"><Toggle :model-value="optionTun" style="pointer-events: none;" /> {{ $t('setting.tun') }}</label>
          <label class="pop-item" @click.prevent="optionExt = !optionExt"><Toggle :model-value="optionExt" style="pointer-events: none;" /> {{ $t('basic.exp.extController') }}</label>
          <label class="pop-item" @click.prevent="optionLog = !optionLog"><Toggle :model-value="optionLog" style="pointer-events: none;" /> {{ $t('basic.log.title') }}</label>
          <label class="pop-item" @click.prevent="optionDns = !optionDns"><Toggle :model-value="optionDns" style="pointer-events: none;" /> {{ $t('pages.dns') }}</label>
          <label class="pop-item" @click.prevent="optionRules = !optionRules"><Toggle :model-value="optionRules" style="pointer-events: none;" /> {{ $t('pages.rules') }}</label>
        </Pop>
      </div>
    </SettingsGroup>
  </div>
</template>

<script lang="ts" setup>
import Select from '@/components/ui/Select.vue'
import { computed, onMounted, ref, watch } from 'vue'
import yaml from 'yaml'
import { push } from 'notivue'
import { i18n } from '@/locales'
import HttpUtils from '@/plugins/httputil'
import { FindDiff } from '@/plugins/utils'
import EditorModal from '@/layouts/drawers/EditorModal.vue'
import Tabs from '@/components/ui/Tabs.vue'
import Btn from '@/components/ui/Btn.vue'
import Ico from '@/components/ui/Ico.vue'
import Pop from '@/components/ui/Pop.vue'
import Toggle from '@/components/ui/Toggle.vue'
import SwitchLabel from '@/components/ui/SwitchLabel.vue'
import Field from '@/components/ui/Field.vue'
import ChipSelect from '@/components/ui/ChipSelect.vue'
import SettingsGroup from '@/components/ui/SettingsGroup.vue'
import SRow from '@/components/ui/SRow.vue'
import ToggleRow from '@/components/ui/ToggleRow.vue'
import DomainInput from '@/components/ui/DomainInput.vue'
import CertsPanel from '@/components/settings/CertsPanel.vue'
import { loadCerts, certsLoaded, findCert, daysLeft } from '@/plugins/certs'

const tab = ref('interface')
const loading = ref(false)
// 保存时要跟当前值逐字段比对,判断反代配置需不需要重新生成(见 save)
const oldSettings = ref<Record<string, any>>({})
const jsonEditor = ref(false)
const clashEditor = ref(false)

const tabItems = computed((): [string, string][] => [
  ['certs', i18n.global.t('setting.certs')],
  ['interface', i18n.global.t('setting.interface')],
  ['sub', i18n.global.t('setting.sub')],
  ['jsonSub', i18n.global.t('setting.jsonSub')],
  ['clashSub', i18n.global.t('setting.clashSub')],
])

const settings = ref({
  webListen: "",
  webDomain: "",
  webPort: "2095",
  webCertFile: "",
  webKeyFile: "",
  webCertMode: "",
  webNginx: "",
  webAcmeMethod: "auto",
  webAcmeEmail: "",
  webPath: "/app/",
  webURI: "",
  sessionMaxAge: "0",
  trafficAge: "30",
  timeLocation: "Asia/Tehran",
  subListen: "",
  subPort: "2096",
  subPath: "/sub/",
  subDomain: "",
  subCertFile: "",
  subKeyFile: "",
  subCertMode: "",
  subNginx: "",
  subAcmeEmail: "",
  subUpdates: "12",
  subEncode: "true",
  subShowInfo: "false",
  subURI: "",
  subJsonExt: "",
  subClashExt: "",
  subClashNoDefGrp: "false",
  subClashSprtAll: "false",
})

onMounted(async () => {
  loading.value = true
  // 证书清单在前:setData 要拿它把 webCertFile/webKeyFile 派生成与域名一致的值,
  // 且必须发生在 oldSettings 快照之前——反过来的话,派生落在快照之后,刚打开设置页
  // 就凭空多出「未保存改动」,而 webCertFile 在 proxyInputs 里,随手保存个时区都会
  // 触发整个面板重启。
  await loadCerts()
  await loadData()
  loading.value = false
})

const loadData = async () => {
  loading.value = true
  const msg = await HttpUtils.get('api/settings')
  loading.value = false
  if (msg.success) {
    setData(msg.obj)
  }
}

const setData = (data: any) => {
  settings.value = data
  // 已移除面板内置「自动 ACME」模式：旧数据残留的 'acme' 规整为手动文件模式，并提示用户去确认续期
  const hadAcme = settings.value.webCertMode === 'acme' || settings.value.subCertMode === 'acme'
  if (settings.value.webCertMode === 'acme') settings.value.webCertMode = ''
  if (settings.value.subCertMode === 'acme') settings.value.subCertMode = ''
  if (hadAcme) {
    push.warning({ message: i18n.global.t('setting.acmeMigrated'), duration: 8000 })
  }
  loadSubJsonExt()
  // 证书路径是域名的派生量,在快照【之前】规整成一致状态:证书页「编辑路径」之后,
  // 这里跟着换,面板下次重启读的才是证书页展示的那份文件
  syncCertsFromList()
  oldSettings.value = { ...settings.value }
}

// nginx 那份 vhost 里写死了这些值,任何一个变了都得重新生成并重启对应的服务
const proxyInputs = [
  'webNginx', 'webDomain', 'webPort', 'webPath', 'webListen', 'webCertFile', 'webKeyFile',
  'subNginx', 'subDomain', 'subPort', 'subPath', 'subListen', 'subCertFile', 'subKeyFile',
]

const save = async () => {
  // 保存前按当前域名把证书路径重新派生一次:域名 watch 只盯域名变化,证书清单的
  // 变化(刚在证书页申请/登记/改过路径)不在它眼里,不补这一步会把旧路径存进库
  syncCertsFromList()
  const now = settings.value as Record<string, any>
  const before = oldSettings.value
  const webOn = now.webNginx === 'true'
  const subOn = now.subNginx === 'true'
  const webWasOn = before.webNginx === 'true'
  const subWasOn = before.subNginx === 'true'

  // Configuring nginx before the save is safe in exactly one case: the panel-side
  // proxy going from OFF to ON. Only then is this page still served by the panel's
  // own TLS port rather than through nginx, so rewriting nginx cannot cut the road
  // out from under the requests that follow — and it has to happen first, because
  // the panel is about to drop to plaintext HTTP and something must already be
  // answering on 443.
  //
  // Once the proxy is on, this page IS the nginx location, and touching nginx here
  // strands the save. Changing the path or domain moves the location (/app/ ->
  // /app1/), so the very next request, api/save at /app/api/save, is no longer
  // proxied; switching the proxy off deletes the location outright. Either way the
  // browser cannot deliver api/save or api/restartApp, and the install is left with
  // nginx pointing at the new config while the DB still holds the old one — the two
  // disagree and the panel is unreachable from the internet. Deleting/rewriting the
  // vhost and restarting the panel cannot be ordered safely against each other:
  // whichever goes first, the step in between dies on the road it just cut.
  //
  // So every change made while the proxy is on lands later instead: save, restart,
  // and the startup reconciliation rewrites nginx from the persisted settings
  // (syncNginxProxy in app.Start), by which point the panel is already serving the
  // new path. waitReachable then holds the redirect until the new address answers.
  // The whole test reduces to "was the panel behind nginx when this page loaded":
  // if it was not, this page does not go through nginx and configuring it now is
  // safe (and required, for whichever side is being switched on).
  const panelWasBehindProxy = webWasOn

  // 一次调用把两侧都交给后端:它按域名聚合(同域名合并成一份 vhost 的两个 location),
  // 并删掉不再需要的旧配置。
  // 内容没变且已生效时后端直接返回,不会白 reload 一次 nginx。
  if (!panelWasBehindProxy && (webOn || subOn)) {
    loading.value = true
    // CertSet 告诉后端「证书路径这两项表单确实带了,空串就是空」:域名没有证书时
    // 必须让 vhost 生成失败、把用户拦在这里,不能回退读库拿上一个域名的证书凑数
    const r = await HttpUtils.post('api/syncNginxProxy', {
      webNginx: now.webNginx, webDomain: now.webDomain, webPort: now.webPort,
      webPath: now.webPath, webListen: now.webListen, webListenSet: 'true',
      webCertFile: now.webCertFile, webKeyFile: now.webKeyFile, webCertSet: 'true',
      subNginx: now.subNginx, subDomain: now.subDomain, subPort: now.subPort,
      subPath: now.subPath, subListen: now.subListen, subListenSet: 'true',
      subCertFile: now.subCertFile, subKeyFile: now.subKeyFile, subCertSet: 'true',
    })
    loading.value = false
    // 生成失败就【不保存】:服务继续自己终结 TLS,访问方式不变。
    // 反过来先存后配的话,服务已经改跑明文 HTTP 而 nginx 没接住,人就进不来了。
    if (!r.success) {
      // The switch was written into settings by webBehindProxy/subBehindProxy's setter
      // BEFORE this step, and this save never reached the DB. Without reverting it the
      // UI shows "on" while the DB still says "off", and from then on changing anything
      // at all (panel URI, timezone, session age...) carries that switched-on flag into
      // another sync and another failure — the page is wedged, and only a reload escapes.
      // Revert just these two switches, leaving domain/port and the rest of what the user
      // just typed alone: the switch is the one safety-critical field that moves a service
      // to plaintext HTTP and must match the DB, everything else is an unsaved draft.
      // Restore before's ORIGINAL value rather than a hardcoded "false": the DB default is
      // an empty string, so writing "false" conjures up a phantom change in proxyInputs and
      // the next save would restart the panel for nothing.
      // reverted must be computed before assigning — now is the same object as settings.value.
      const reverted = now.webNginx !== before.webNginx || now.subNginx !== before.subNginx
      settings.value.webNginx = before.webNginx ?? ''
      settings.value.subNginx = before.subNginx ?? ''
      if (reverted) {
        push.warning({
          title: i18n.global.t('setting.proxyReverted'),
          duration: 9000,
          message: i18n.global.t('setting.proxyRevertedHint'),
        })
      }
      return
    }
    const vhosts: any[] = Array.isArray(r.obj) ? r.obj : []
    if (vhosts.length) {
      push.success({
        title: i18n.global.t('setting.proxyGenerated'),
        duration: 9000,
        message: vhosts.map(v => i18n.global.t('setting.proxyGeneratedHint', {
          url: (v.urls || []).join('  '), conf: v.confFile,
        })).join('\n'),
      })
    }
  }

  // 对外地址现在由 nginx 那份 vhost 决定,服务自己推断不出来(它只知道内网的 http://ip:端口)。
  // 顺手填好,重启后的跳转就落在真正能打开的地址上;用户手填过就不动——但「我们自动填的」
  // 要跟着域名/路径走:反代开着改域名时,旧地址的 vhost 恰恰在这次保存里被删掉,不跟的话
  // 保存后的跳转和发给客户端的订阅链接就都指向一个已经没人服务的地址,还会一直存在库里。
  // 关掉反代时反过来:若当前值正是我们填的那个,清空它,让跳转按服务自身的域名/端口重推。
  const webAuto = 'https://' + before.webDomain + normalizePath(before.webPath)
  if (webOn && now.webDomain && (!now.webURI || now.webURI === webAuto)) {
    settings.value.webURI = 'https://' + now.webDomain + normalizePath(now.webPath)
  } else if (!webOn && webWasOn && now.webURI === webAuto) {
    settings.value.webURI = ''
  }
  const subAuto = 'https://' + before.subDomain + normalizePath(before.subPath)
  if (subOn && now.subDomain && (!now.subURI || now.subURI === subAuto)) {
    settings.value.subURI = 'https://' + now.subDomain + normalizePath(now.subPath)
  } else if (!subOn && subWasOn && now.subURI === subAuto) {
    settings.value.subURI = ''
  }

  // 两个服务都只在启动时读各自的 Nginx 开关/端口/监听地址,所以这些改动必须带一次重启:
  // 开启后不重启,服务还在原端口上说 TLS,而 nginx 已经用明文 HTTP 连它 —— 443 全站 502;
  // 关闭后不重启则反过来。saveAndRestart 会保存、重启、探活,再跳到新地址。
  if (proxyInputs.some(k => now[k] !== before[k])) {
    loading.value = true
    push.success({
      title: i18n.global.t('success'),
      duration: 8000,
      message: i18n.global.t('setting.proxyRestarting'),
    })
    await saveAndRestart(true)
    loading.value = false
    return
  }

  loading.value = true
  const msg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(settings.value) })
  if (msg.success) {
    push.success({
      title: i18n.global.t('success'),
      duration: 5000,
      message: i18n.global.t('actions.set') + " " + i18n.global.t('pages.settings')
    })
    setData(msg.obj.settings)
  }
  loading.value = false
}

// 路径规整成前后都带斜杠,与后端 normalizeProxyPath 一致——两边拼出的对外地址必须逐字
// 相同,否则「这个 URI 是不是我们自动填的」判不出来,关掉反代时就清不掉那个死地址。
const normalizePath = (p: string) => {
  let s = (p ?? '').trim()
  if (s === '') s = '/'
  if (!s.startsWith('/')) s = '/' + s
  if (!s.endsWith('/')) s += '/'
  return s
}

const sleep = (ms: number) => new Promise(resolve => setTimeout(resolve, ms))

const restartApp = async () => {
  loading.value = true
  const msg = await HttpUtils.post('api/restartApp', {})
  if (msg.success) {
    // webURI 是对外地址的手工覆盖:填了就用它(反代模式下面板推断不出对外地址,
    // 只能靠它),没填才按面板自身的域名/端口/协议拼。条件曾写反,导致填了被覆盖、
    // 没填则 replace("") 原地打转。
    let url = settings.value.webURI
    if (url === "") {
      url = buildURL(settings.value.webDomain, settings.value.webPort.toString(), panelIsTLS(), settings.value.webPath)
    }
    await sleep(3000)
    window.location.replace(url)
  }
  loading.value = false
}

// 面板重启后会在 http 还是 https 上。判定优先级必须跟 web.go 一致:webNginx 最先
// 短路,面板只跑 HTTP、根本不看证书字段;否则有证书路径就是 HTTPS。
// 跳转地址靠它拼,判错就把人送到一个打不开的地址上——而那时面板已经重启完了,
// 没有第二次机会。
const panelIsTLS = () =>
  settings.value.webNginx !== "true" &&
  (settings.value.webCertMode === "acme" || settings.value.webCertFile !== "" || settings.value.webKeyFile !== "")

const buildURL = (host: string, port: string, isTLS: boolean, path: string) => {
  if (!host || host.length == 0) host = window.location.hostname
  if (!port || port.length == 0) port = window.location.port

  const protocol = isTLS ? "https:" : "http:"

  if (port === "" || (isTLS && port === "443") || (!isTLS && port === "80")) {
    port = ""
  } else {
    port = `:${port}`
  }

  return `${protocol}//${host}${port}${path}settings`
}

// 高级选项:由反向代理终结 TLS(面板保持 HTTP)。沿用 webNginx 键,web.go 对它的语义不变
const webBehindProxy = computed({
  get: () => { return settings.value.webNginx == "true" },
  set: (v: boolean) => { settings.value.webNginx = v ? "true" : "false" }
})

// 反代模式下面板跑明文 HTTP,监听地址若不是回环(webListen 为空即 0.0.0.0),
// 明文端口会直接暴露公网、绕过代理的 TLS——仅在这种真有风险时才追加警告,避免常驻噪音
const loopbackListens = ['127.0.0.1', 'localhost', '::1', '[::1]']
const behindProxyDesc = computed(() => {
  const base = i18n.global.t('setting.behindProxyHint')
  if (!webBehindProxy.value || loopbackListens.includes(settings.value.webListen.trim())) return base
  return base + ' ' + i18n.global.t('setting.behindProxyListenWarn')
})

// 订阅侧的反代开关,与面板侧对称(沿用 subNginx 键,sub.go 据此只跑明文 HTTP)
const subBehindProxy = computed({
  get: () => settings.value.subNginx == "true",
  set: (v: boolean) => { settings.value.subNginx = v ? "true" : "false" }
})

// 与面板侧同理:订阅跑明文 HTTP 时,监听地址不是回环就等于把明文端口直接暴露到公网
const subBehindProxyDesc = computed(() => {
  const base = i18n.global.t('setting.behindProxyHint')
  if (!subBehindProxy.value || loopbackListens.includes(settings.value.subListen.trim())) return base
  return base + ' ' + i18n.global.t('setting.behindProxyListenWarn')
})

// uriPathOf returns the path of a manually entered public address, or "" if the
// value is not a parseable absolute URL (something else already reports that).
const uriPathOf = (uri: string) => {
  const v = (uri ?? '').trim()
  if (!v) return ''
  try {
    return normalizePath(new URL(v).pathname)
  } catch {
    return ''
  }
}

// This field only decides where the redirect goes after a restart and how
// subscription links are built. It changes no routing whatsoever — the path the
// panel actually serves comes from webPath, which is also what the generated nginx
// location is built from. Nothing in the two names says so ("面板路径" vs
// "面板 URI"; worse in English, "Base URI" vs "Panel URI"), so editing this one to
// move the panel to a new path is an easy and costly mistake: the redirect lands on
// a 404 while the panel keeps serving the old path. Say it the moment they diverge.
const webUriPathMismatch = computed(() => {
  const p = uriPathOf(settings.value.webURI)
  return p !== '' && p !== normalizePath(settings.value.webPath)
})

// 反代模式下面板只知道自己是 http://内网:端口,推断不出对外地址(代理的域名/端口/协议
// 它都看不到),重启后的跳转只能靠 webURI。仅此时提示,非反代模式它是可选覆盖项。
const webUriHint = computed(() => {
  const parts: string[] = []
  if (webBehindProxy.value) parts.push(i18n.global.t('setting.webUriProxyHint'))
  if (webUriPathMismatch.value) parts.push(i18n.global.t('setting.webUriPathMismatch'))
  return parts.join(' ')
})

// ===== 域名 ↔ 证书 =====
//
// 证书路径不再手填:选中的域名有证书就用它那两个文件,没有就留空(服务跑 HTTP)。
// webCertFile / webKeyFile 仍是面板启动时真正读的字段(web.go 未变),只是它们的
// 值现在由这里派生——「域名与证书」页是唯一的证书来源。
const syncCert = (side: 'web' | 'sub') => {
  const domain = side === 'web' ? settings.value.webDomain : settings.value.subDomain
  // 域名为空时一概不动。存量里有「不填域名、直接手填证书路径」的跑法(面板跑 HTTPS
  // 但不校验 Host),那种配置在这个页面上无从表达,清空等于把人家的 HTTPS 静默降级。
  if (!(domain ?? '').trim()) return
  const c = findCert(domain)
  if (side === 'web') {
    settings.value.webCertFile = c?.certFile ?? ''
    settings.value.webKeyFile = c?.keyFile ?? ''
  } else {
    settings.value.subCertFile = c?.certFile ?? ''
    settings.value.subKeyFile = c?.keyFile ?? ''
  }
}

// 两侧一起派生。清单没有【成功】加载过就一概不动:一次失败的 api/certs 留下的是
// 空清单,拿空清单派生等于把面板正在用的证书路径全部抹掉,保存重启后 HTTPS 就没了。
// setData(快照前)和 save(入库前)各调一次,证书清单的变化(申请/登记/编辑路径)
// 在这两个时点被收拢,平时不需要额外的 watch。
const syncCertsFromList = () => {
  if (!certsLoaded()) return
  syncCert('web')
  syncCert('sub')
}

// 改域名 = 换证书,两个方向都要跟(选到没证书的域名就得清空,否则会拿着上一个
// 域名的证书去跑 HTTPS,浏览器直接报 name mismatch)。
watch(() => settings.value.webDomain, () => { if (certsLoaded()) syncCert('web') })
watch(() => settings.value.subDomain, () => { if (certsLoaded()) syncCert('sub') })

type CertNote = { text: string; kind: 'ok' | 'warn' | 'mute'; offerIssue: boolean }

// 域名框下面那行回执。反代开着却没证书是【保存必失败】的组合(生成不出 vhost),
// 与其等后端报错不如当场说清楚。
const certNote = (domain: string, behindProxy: boolean): CertNote | null => {
  const d = (domain ?? '').trim()
  if (!d) return null
  // 清单拿不到时宁可闭嘴:此时说「还没有证书」是把一次查询故障当成事实陈述
  if (!certsLoaded()) return null
  const c = findCert(d)
  if (!c) {
    return behindProxy
      ? { text: i18n.global.t('setting.certNoteMissingProxy'), kind: 'warn', offerIssue: true }
      : { text: i18n.global.t('setting.certNoteMissing'), kind: 'mute', offerIssue: true }
  }
  if (!c.notAfter) return { text: i18n.global.t('setting.certNoteUnreadable'), kind: 'warn', offerIssue: false }
  const days = daysLeft(c.notAfter)
  if (days < 0) return { text: i18n.global.t('setting.certNoteExpired'), kind: 'warn', offerIssue: false }
  return { text: i18n.global.t('setting.certNoteOk', { days }), kind: days <= 14 ? 'warn' : 'ok', offerIssue: false }
}

const webCertNote = computed(() => certNote(settings.value.webDomain, webBehindProxy.value))
const subCertNote = computed(() => certNote(settings.value.subDomain, subBehindProxy.value))

// 「去申请」:切到证书页并把域名带过去,省得再输一遍
const issueDomain = ref('')
const goIssue = (domain: string) => {
  issueDomain.value = (domain ?? '').trim()
  tab.value = 'certs'
}
// 离开证书页就清掉:不清的话,之后任何一次直接点开证书页,申请表单里都还预填着
// 上次「去申请」留下的域名,一不留神就为早已不想要的主机签发、白烧一次 LE 配额
watch(tab, (t) => {
  if (t !== 'certs') issueDomain.value = ''
})

// 表单当前值里开着反代的域名——证书页申请/续期时要据此上传 behindProxy,
// 决定 acme.sh 装不装续期后的 nginx 重载钩子
const proxiedDomains = computed(() => {
  const out: string[] = []
  if (settings.value.webNginx === 'true' && settings.value.webDomain.trim()) out.push(settings.value.webDomain.trim())
  if (settings.value.subNginx === 'true' && settings.value.subDomain.trim()) out.push(settings.value.subDomain.trim())
  return out
})

// 需要重启才生效的改动的自动收尾:保存设置→重启面板→探活→跳转(sub 留在原页)。
// 注意:保存的是整页 settings,用户其它尚未保存的改动会随本次保存一并生效。
const saveAndRestart = async (isWeb: boolean) => {
  const saveMsg = await HttpUtils.post('api/save', { object: 'settings', action: 'set', data: JSON.stringify(settings.value) })
  if (!saveMsg.success) return
  oldSettings.value = { ...settings.value }
  // 后端安排 500ms 后重启,响应通常能正常返回;但结果一律不看——重启窗口边界上仍可能
  // 连接被掐,而那也只说明重启已开始,不该就此停下把用户留在死页。照样探活+跳转,真没
  // 重启则探活超时后照样跳,由浏览器给出最终错误(与 waitReachable 的兜底哲学一致)。
  // 故意用裸 fetch 而非 HttpUtils:后者会把这种失败经统一处理弹成红色错误,与刚推送的
  // "即将重启"成功提示自相矛盾。返回值本就不看,吞掉异常即可。
  // 路径保持相对形式,与 axios 的 baseURL="./" 解析结果一致(勿写成绝对路径,会破坏
  // 自定义面板路径与 dev 模式);session cookie 由 same-origin 默认携带。
  await fetch('api/restartApp', { method: 'POST', credentials: 'same-origin' }).catch(() => {})
  // 取值顺序必须与 restartApp 一致:填了 webURI 就用它。它不是反代专用——NAT 端口
  // 映射、非标准对外端口、前面挂 CDN 的用户都会填,无条件推断会把他们跳到错地址。
  // 协议同样要推,不能写死 https:这个函数早先只在「证书申请成功」后被调用,那时必然
  // 有证书;如今保存设置也走这条路,而域名换成一个没有证书的就会让面板退回 HTTP——
  // 写死 https 会把人跳到打不开的地址,且面板已经重启完,没有第二次机会。
  const target = isWeb
    ? (settings.value.webURI || buildURL(settings.value.webDomain, settings.value.webPort.toString(), panelIsTLS(), settings.value.webPath))
    : window.location.href
  await sleep(3000)
  await waitReachable(target)
  window.location.replace(target)
}

// 用 no-cors 裸 fetch 探活目标地址:重启期间请求必然失败,走 HttpUtils 会刷错误 toast;
// 跳 https 时与当前页跨协议,CORS 下也读不了响应——opaque 响应能 resolve 就说明面板已就绪。
// 每次尝试单独限时:连接被静默丢包时(切 HTTPS 撞上防火墙规则变更就会这样),fetch 不会
// 很快失败,而是一直挂到浏览器默认连接超时(可达 90s+),固定次数的循环便退化成数分钟的
// 卡死。改用总预算封顶,单次超时由 AbortSignal 保证,整体最坏 probeBudget + probeTimeout。
// 探活超时也照样跳转,由浏览器给出最终错误。
const probeTimeout = 2000
const probeBudget = 30000
const waitReachable = async (url: string) => {
  const deadline = Date.now() + probeBudget
  while (Date.now() < deadline) {
    try {
      await fetch(url, { mode: 'no-cors', cache: 'no-store', signal: AbortSignal.timeout(probeTimeout) })
      return
    } catch {
      await sleep(800)
    }
  }
}

// 证书统一走「手动文件 / acme.sh 申请」，不再提供面板内置自动 ACME 开关

const subEncode = computed({
  get: () => { return settings.value.subEncode == "true" },
  set: (v: boolean) => { settings.value.subEncode = v ? "true" : "false" }
})

const subShowInfo = computed({
  get: () => { return settings.value.subShowInfo == "true" },
  set: (v: boolean) => { settings.value.subShowInfo = v ? "true" : "false" }
})

const webPort = computed({
  get: () => { return settings.value.webPort.length > 0 ? parseInt(settings.value.webPort) : 2095 },
  set: (v: number) => { settings.value.webPort = v > 0 ? v.toString() : "2095" }
})

const sessionMaxAge = computed({
  get: () => { return settings.value.sessionMaxAge.length > 0 ? parseInt(settings.value.sessionMaxAge) : 0 },
  set: (v: number) => { settings.value.sessionMaxAge = v > 0 ? v.toString() : "0" }
})

const trafficAge = computed({
  get: () => { return settings.value.trafficAge.length > 0 ? parseInt(settings.value.trafficAge) : 0 },
  set: (v: number) => { settings.value.trafficAge = v > 0 ? v.toString() : "0" }
})

const subPort = computed({
  get: () => { return settings.value.subPort.length > 0 ? parseInt(settings.value.subPort) : 2096 },
  set: (v: number) => { settings.value.subPort = v > 0 ? v.toString() : "2096" }
})

const subUpdates = computed({
  get: () => { return settings.value.subUpdates.length > 0 ? parseInt(settings.value.subUpdates) : 12 },
  set: (v: number) => { settings.value.subUpdates = v > 0 ? v.toString() : "12" }
})

const stateChange = computed(() => {
  return !FindDiff.deepCompare(settings.value, oldSettings.value)
})

// 反代开着、域名却没证书 = 这次保存必然失败(后端生成不出 vhost)。与其让用户点下去、
// 后端写了一半再回滚,不如从源头禁用保存;域名框下的 certNote 已经把原因和「去申请」
// 摆出来了。清单没成功加载时不拦(certsLoaded 为 false),交给后端判,避免一次查询
// 故障把保存彻底锁死。关反代时不受影响:missing 只在开关仍开着时成立。
const proxyBlocked = computed(() => {
  if (!certsLoaded()) return false
  const missing = (on: boolean, domain: string) =>
    on && !!(domain ?? '').trim() && !findCert(domain)
  return missing(webBehindProxy.value, settings.value.webDomain) ||
    missing(subBehindProxy.value, settings.value.subDomain)
})

/* ===================================================================
 * JSON subscription extension (逻辑平移自旧 components/SubJsonExt.vue)
 * =================================================================== */

const subJsonExt = ref<any>({})

const levels = ["trace", "debug", "info", "warn", "error", "fatal", "panic"]
const dnsTypes = ['udp', 'tcp', 'local', 'tls', 'quic', 'h3']

const defaultLog = {
  "level": "info",
  "timestamp": true
}

const defaultInb = [
  {
    "type": "tun",
    "address": [
      "172.19.0.1/30",
      "fdfe:dcba:9876::1/126"
    ],
    "mtu": 9000,
    "auto_route": true,
    "strict_route": false,
    "endpoint_independent_nat": false,
    "stack": "mixed",
    "exclude_package": <string[]>[],
    "platform": {
      "http_proxy": {
        "enabled": true,
        "server": "127.0.0.1",
        "server_port": 2080
      }
    }
  },
  {
    "type": "mixed",
    "listen": "127.0.0.1",
    "listen_port": 2080,
    "users": []
  }
]

const defaultExp = {
  "clash_api": {
    "external_controller": "127.0.0.1:9090",
    "external_ui": "ui",
    "secret": "",
    "external_ui_download_url": "https://mirror.ghproxy.com/https://github.com/MetaCubeX/Yacd-meta/archive/gh-pages.zip",
    "external_ui_download_detour": "direct",
    "default_mode": "rule"
  },
  "cache_file": {
    "enabled": true,
    "store_fakeip": false
  }
}

const defaultDns = {
  "servers": [
    {
      "type": "tcp",
      "tag": "proxy-dns",
      "server": "8.8.8.8",
      "server_port": 53,
      "detour": "proxy",
      "domain_resolver": "local-dns",
    },
    {
      "tag": "direct-dns",
      "type": "local",
    },
    {
      "tag": "local-dns",
      "type": "local",
    }
  ],
  "rules": [
    {
      "clash_mode": "Global",
      "source_ip_cidr": [
        "172.19.0.0/30",
        "fdfe:dcba:9876::1/126"
      ],
      "action": "route",
      "server": "proxy-dns"
    },
    {
      "clash_mode": "Direct",
      "action": "route",
      "server": "direct-dns"
    },
    {
      "source_ip_cidr": [
        "172.19.0.0/30",
        "fdfe:dcba:9876::1/126"
      ],
      "action": "route",
      "server": "proxy-dns"
    },
  ],
  "final": "local-dns",
  "strategy": "prefer_ipv4"
}

const geositeList = [
  { title: "Private", value: "geosite-private" },
  { title: "Ads", value: "geosite-ads" },
  { title: "🇮🇷 Iran", value: "geosite-ir" },
  { title: "🇨🇳 China", value: "geosite-cn" },
  { title: "🇻🇳 Vietnam", value: "geosite-vn" },
]

const geoList = [
  { title: "Site-Private", value: "geoip-private" },
  { title: "IP-Private", value: "geosite-private" },
  { title: "Site-Ads", value: "geosite-ads" },
  { title: "🇮🇷 Site-Iran", value: "geosite-ir" },
  { title: "🇮🇷 IP-Iran", value: "geoip-ir" },
  { title: "🇨🇳 Site-China", value: "geosite-cn" },
  { title: "🇨🇳 IP-China", value: "geoip-cn" },
  { title: "🇻🇳 Site-Vietnam", value: "geosite-vn" },
  { title: "🇻🇳 IP-Vietnam", value: "geoip-vn" },
]

const geo = [
  {
    tag: "geosite-ads",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ads-all.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-private",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/private.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-ir",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/category-ir.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-cn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geosite/cn.srs",
    download_detour: "direct"
  },
  {
    tag: "geosite-vn",
    type: "remote",
    format: "binary",
    url: "https://github.com/Thaomtam/Geosite-vn/raw/rule-set/Geosite-vn.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-private",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/private.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-ir",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/ir.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-cn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/cn.srs",
    download_detour: "direct"
  },
  {
    tag: "geoip-vn",
    type: "remote",
    format: "binary",
    url: "https://testingcf.jsdelivr.net/gh/MetaCubeX/meta-rules-dat@sing/geo/geoip/vn.srs",
    download_detour: "direct"
  }
]

// 解析 settings.subJsonExt → 本地对象;并按旧实现把规范化后的 JSON 回写
const loadSubJsonExt = () => {
  const s: string = settings.value.subJsonExt ?? ''
  if (s.length > 0) {
    try {
      const parsed = JSON.parse(s)
      subJsonExt.value = parsed
      settings.value.subJsonExt = Object.keys(parsed).length > 0 ? JSON.stringify(parsed, null, 2) : ''
    } catch (e) {
      // 数据库里是坏 JSON:保留原始字符串不动(与旧版一致,不在这里破坏数据)
    }
  } else {
    subJsonExt.value = {}
  }
}

watch(subJsonExt, (v) => {
  settings.value.subJsonExt = Object.keys(v).length > 0 ? JSON.stringify(v, null, 2) : ''
}, { deep: true })

const enableLog = computed({
  get: (): boolean => subJsonExt.value?.log != undefined,
  set: (v: boolean) => { v ? subJsonExt.value.log = defaultLog : delete subJsonExt.value.log }
})

const enableDns = computed({
  get: (): boolean => subJsonExt.value?.dns != undefined,
  set: (v: boolean) => {
    if (v) {
      subJsonExt.value.dns = defaultDns
      if (rules.value == undefined) subJsonExt.value.rules = [{ action: 'sniff' }]
      subJsonExt.value.rules.unshift({ protocol: "dns", action: "hijack-dns" })
    } else {
      delete subJsonExt.value.dns
      const newRules = subJsonExt.value?.rules?.filter((r: any) => r.protocol != "dns") ?? []
      if (newRules.length >= 0) subJsonExt.value.rules = newRules
      if (rules.value?.length == 0) delete subJsonExt.value.rules
    }
  }
})

const enableInb = computed({
  get: (): boolean => subJsonExt.value?.inbounds != undefined,
  set: (v: boolean) => { v ? subJsonExt.value.inbounds = defaultInb.slice() : delete subJsonExt.value.inbounds }
})

const enableExp = computed({
  get: (): boolean => subJsonExt.value?.experimental != undefined,
  set: (v: boolean) => { v ? subJsonExt.value.experimental = defaultExp : delete subJsonExt.value.experimental }
})

const dns = computed((): any => subJsonExt.value?.dns ?? undefined)

const proxyDns = computed((): any => dns.value?.servers?.findLast((d: any) => d.tag == "proxy-dns") ?? {})

const directDns = computed((): any => dns.value?.servers?.findLast((d: any) => d.tag == "direct-dns") ?? {})

// 平移旧 SimpleDNS:切到 local 时去掉 server / server_port
const setDnsType = (server: any, t: string) => {
  server.type = t
  if (t == 'local') {
    delete server.server
    delete server.server_port
  }
}

const dnsTags = computed((): string[] => dns.value?.servers?.map((d: any) => d.tag) ?? [])

const defaultResolver = computed({
  get: (): string => subJsonExt.value?.default_domain_resolver ?? '',
  set: (v: string) => {
    if (v) subJsonExt.value.default_domain_resolver = v
    else delete subJsonExt.value.default_domain_resolver
  }
})

const dnsToDirect = computed({
  get: (): string[] => {
    const ruleIndex = dns.value?.rules?.findIndex((r: any) => r.server == "direct-dns" && Object.hasOwn(r, 'rule_set')) ?? -1
    return ruleIndex >= 0 ? dns.value.rules[ruleIndex].rule_set : []
  },
  set: (v: string[]) => {
    const ruleIndex = dns.value?.rules?.findIndex((r: any) => r.server == "direct-dns" && Object.hasOwn(r, 'rule_set')) ?? -1
    if (v.length > 0) {
      if (ruleIndex >= 0) {
        dns.value.rules[ruleIndex].rule_set = v
      } else {
        dns.value.rules.push({ rule_set: v, action: "route", server: "direct-dns" })
      }
    } else {
      if (ruleIndex != -1) dns.value.rules.splice(ruleIndex, 1)
    }
    updateRuleSets()
  }
})

const inbounds = computed((): any => subJsonExt.value?.inbounds ?? undefined)

const tunAddress = computed({
  get: (): string => (inbounds.value?.[0]?.address ?? []).join(','),
  set: (v: string) => {
    if (!inbounds.value?.[0]) return
    inbounds.value[0].address = v.split(',').map((s: string) => s.trim()).filter((s: string) => s.length > 0)
  }
})

const tunExcludePkg = computed({
  get: (): string => (inbounds.value?.[0]?.exclude_package ?? []).join(','),
  set: (v: string) => {
    if (!inbounds.value?.[0]) return
    inbounds.value[0].exclude_package = v.split(',').map((s: string) => s.trim()).filter((s: string) => s.length > 0)
  }
})

const platformProxy = computed({
  get: (): boolean => inbounds.value?.[0]?.platform != undefined,
  set: (v: boolean) => { subJsonExt.value.inbounds[0].platform = v ? defaultInb[0].platform : undefined }
})

const rules = computed((): any => subJsonExt.value?.rules ?? undefined)

const ruleToDirect = computed({
  get: (): string[] => {
    const ruleIndex = rules.value?.findIndex((r: any) => r.outbound == "direct" && Object.hasOwn(r, 'rule_set')) ?? -1
    return ruleIndex >= 0 ? rules.value[ruleIndex].rule_set : []
  },
  set: (v: string[]) => {
    const ruleIndex = rules.value?.findIndex((r: any) => r.outbound == "direct" && Object.hasOwn(r, 'rule_set')) ?? -1
    if (v.length > 0) {
      if (ruleIndex >= 0) {
        rules.value[ruleIndex].rule_set = v
      } else {
        if (rules.value == undefined) subJsonExt.value.rules = []
        rules.value.push({ rule_set: v, action: "route", outbound: "direct" })
      }
    } else {
      if (ruleIndex != -1) rules.value.splice(ruleIndex, 1)
    }
    updateRuleSets()
  }
})

const ruleToBlock = computed({
  get: (): string[] => {
    const ruleIndex = rules.value?.findIndex((r: any) => r.action == "reject" && Object.hasOwn(r, 'rule_set')) ?? -1
    return ruleIndex >= 0 ? rules.value[ruleIndex].rule_set : []
  },
  set: (v: string[]) => {
    const ruleIndex = rules.value?.findIndex((r: any) => r.action == "reject" && Object.hasOwn(r, 'rule_set')) ?? -1
    if (v.length > 0) {
      if (ruleIndex >= 0) {
        rules.value[ruleIndex].rule_set = v
      } else {
        if (rules.value == undefined) subJsonExt.value.rules = []
        rules.value.push({ rule_set: v, action: "reject" })
      }
    } else {
      if (ruleIndex != -1) rules.value.splice(ruleIndex, 1)
    }
    updateRuleSets()
  }
})

const updateRuleSets = () => {
  const tags = <string[]>[]
  if ((dns.value?.rules?.length ?? 0) > 0) dns.value.rules.forEach((r: any) => { if (r.rule_set) tags.push(...r.rule_set) })
  if ((rules.value?.length ?? 0) > 0) rules.value.forEach((r: any) => { if (r.rule_set) tags.push(...r.rule_set) })
  if (tags.length > 0) {
    subJsonExt.value.rule_set = geo.filter((g: any) => tags.includes(g.tag))
  } else {
    delete subJsonExt.value.rule_set
  }
  if (rules.value?.length == 0) delete subJsonExt.value.rules
}

const saveJsonEditor = (data: string) => {
  try {
    subJsonExt.value = JSON.parse(data)
  } catch (e) {
    push.error({
      message: i18n.global.t('failed') + ": " + i18n.global.t('error.invalidData'),
      duration: 5000,
    })
  }
}

/* ===================================================================
 * Clash subscription extension (逻辑平移自旧 components/SubClashExt.vue)
 * =================================================================== */

const clashNoDefGrp = computed({
  get: () => { return settings.value.subClashNoDefGrp == "true" },
  set: (v: boolean) => { settings.value.subClashNoDefGrp = v ? "true" : "false" }
})

const clashSprtAll = computed({
  get: () => { return settings.value.subClashSprtAll == "true" },
  set: (v: boolean) => { settings.value.subClashSprtAll = v ? "true" : "false" }
})

const defaultConfig: any = {
  "mixed-port": 7890,
  "allow-lan": false,
  "mode": "rule",
  "log-level": "info",
  "external-controller": "127.0.0.1:9090",
  "tun": {
    "enable": true,
    "stack": "system",
    "auto-route": true,
    "auto-detect-interface": true,
    "dns-hijack": ["any:53"],
  },
  "dns": {
    "enable": true,
    "ipv6": false,
    "enhanced-mode": "fake-ip",
    "fake-ip-range": "198.18.0.1/16",
    "default-nameserver": ["8.8.8.8", "1.1.1.1"],
    "nameserver": [
      "https://doh.pub/dns-query",
      "https://1.0.0.1/dns-query"
    ],
    "fallback": ["tcp://9.9.9.9:53"],
    "fake-ip-filter": ["*.lan", "localhost", "*.local"]
  },
  "rules": [
    "GEOIP,Private,DIRECT",
    "MATCH,Proxy"
  ]
}

const clashLevels = ['debug', 'info', 'warning', 'error']

const rulesIP = [
  { title: 'Private-Direct', value: 'GEOIP,Private,DIRECT' },
  { title: 'Private-Block', value: 'GEOIP,Private,REJECT' },
  { title: 'LAN-Direct', value: 'GEOIP,LAN,DIRECT' },
  { title: 'LAN-Block', value: 'GEOIP,LAN,REJECT' },
  { title: 'Ads-Direct', value: 'GEOIP,Ads,DIRECT' },
  { title: 'Ads-Block', value: 'GEOIP,Ads,REJECT' },
  { title: '🇨🇳 China-Direct', value: 'GEOIP,CN,DIRECT' },
  { title: '🇨🇳 China-Block', value: 'GEOIP,CN,REJECT' },
  { title: '🇮🇷 Iran-Direct', value: 'GEOIP,CATEGORY-IR,DIRECT' },
  { title: '🇮🇷 Iran-Block', value: 'GEOIP,CATEGORY-IR,REJECT' },
  { title: '🇻🇳 Vietnam-Direct', value: 'GEOIP,CATEGORY-VN,DIRECT' },
  { title: '🇻🇳 Vietnam-Block', value: 'GEOIP,CATEGORY-VN,REJECT' },
  { title: '🇯🇵 Japan-Direct', value: 'GEOIP,JP,DIRECT' },
  { title: '🇯🇵 Japan-Block', value: 'GEOIP,JP,REJECT' },
]

const metaJson = computed({
  get: (): any => {
    try {
      return yaml.parse(settings.value.subClashExt) ?? {}
    } catch (e) {
      return {}
    }
  },
  set: (v: any) => {
    settings.value.subClashExt = Object.keys(v).length == 0 ? "" : yaml.stringify(v)
  }
})

const updateMetaJson = (data: any, key: string) => {
  const newMetaJson = metaJson.value
  if (data == null) {
    delete newMetaJson[key]
  } else {
    newMetaJson[key] = data
  }
  metaJson.value = newMetaJson
}

const optionMixed = computed({
  get: (): boolean => (metaJson.value['mixed-port'] ?? 0) > 0,
  set: (v: boolean) => {
    updateMetaJson(v ? defaultConfig['mixed-port'] : null, 'mixed-port')
    updateMetaJson(v ? defaultConfig['allow-lan'] : null, 'allow-lan')
  }
})

const optionTun = computed({
  get: (): boolean => metaJson.value['tun']?.['enable'] ?? false,
  set: (v: boolean) => { updateMetaJson(v ? defaultConfig['tun'] : null, 'tun') }
})

const optionExt = computed({
  get: (): boolean => (metaJson.value['external-controller']?.length ?? 0) > 0,
  set: (v: boolean) => { updateMetaJson(v ? defaultConfig['external-controller'] : null, 'external-controller') }
})

const optionLog = computed({
  get: (): boolean => (metaJson.value['log-level']?.length ?? 0) > 0,
  set: (v: boolean) => { updateMetaJson(v ? defaultConfig['log-level'] : null, 'log-level') }
})

const optionDns = computed({
  get: (): boolean => metaJson.value['dns']?.['enable'] ?? false,
  set: (v: boolean) => { updateMetaJson(v ? defaultConfig['dns'] : null, 'dns') }
})

const optionRules = computed({
  get: (): boolean => (metaJson.value['rules']?.length ?? 0) > 0,
  set: (v: boolean) => {
    updateMetaJson(v ? defaultConfig['rules'] : null, 'rules')
    updateMetaJson(v ? defaultConfig['mode'] : null, 'mode')
  }
})

const mixedPort = computed({
  get: (): number => metaJson.value['mixed-port'],
  set: (v: number) => { updateMetaJson(v, 'mixed-port') }
})

const allowLan = computed({
  get: (): boolean => metaJson.value['allow-lan'] ?? false,
  set: (v: boolean) => { updateMetaJson(v, 'allow-lan') }
})

const externalController = computed({
  get: (): string => metaJson.value['external-controller'] ?? '',
  set: (v: string) => { updateMetaJson(v, 'external-controller') }
})

const clashLogLevel = computed({
  get: (): string => metaJson.value['log-level'] ?? '',
  set: (v: string) => { updateMetaJson(v, 'log-level') }
})

const clashDns = computed({
  get: (): boolean => metaJson.value['dns']?.['enable'] ?? false,
  set: (v: boolean) => { updateMetaJson({ ...metaJson.value['dns'], 'enable': v }, 'dns') }
})

const clashTun = computed({
  get: (): boolean => metaJson.value['tun']?.['enable'] ?? false,
  set: (v: boolean) => { updateMetaJson({ ...metaJson.value['tun'], 'enable': v }, 'tun') }
})

const clashRules = computed({
  get: (): string[] => (metaJson.value.rules?.length ?? 0) > 0 ? metaJson.value.rules.filter((r: string) => r != "MATCH,Proxy") : [],
  set: (v: string[]) => {
    const newRules = <string[]>[]
    v.forEach((r: string) => { newRules.push(r) })
    updateMetaJson([...newRules, "MATCH,Proxy"], 'rules')
  }
})

const saveClashEditor = (data: string) => {
  try {
    const result = yaml.parse(data)
    if (typeof result != 'object' || Array.isArray(result)) throw new Error()
  } catch (e) {
    push.error({
      message: i18n.global.t('failed') + ": " + i18n.global.t('error.invalidData'),
      duration: 5000,
    })
    return
  }
  settings.value.subClashExt = data
}
</script>

<style scoped>
.head-row {
  display: flex; align-items: center; gap: 8px;
  border-bottom: 1px solid var(--line);
  flex-wrap: wrap;
}
.head-actions {
  display: flex; gap: 8px; align-items: center;
  margin-inline-start: auto;
}
@media (max-width: 820px) {
  .head-actions { margin-inline-start: 0; padding-bottom: 10px; }
}
/* 域名框下面的证书回执:有证书/没证书/快过期，当场说清楚，不必等保存报错 */
.fieldnote {
  margin-top: 7px;
  font-size: 11.5px;
  line-height: 1.45;
  display: flex;
  align-items: flex-start;
  gap: 6px;
  flex-wrap: wrap;
}
.fieldnote::before {
  content: "";
  width: 6px; height: 6px; border-radius: 50%;
  background: currentColor; flex: none; margin-top: 5px;
}
.fieldnote.ok { color: var(--emerald); }
.fieldnote.warn { color: var(--amber); }
.fieldnote.mute { color: var(--text-3); }
.fieldnote a {
  color: inherit; font-weight: 700; cursor: pointer;
  text-decoration: underline; text-underline-offset: 2px;
}
.sub-label {
  font-size: 12px; font-weight: 700; color: var(--text-3);
  letter-spacing: .04em; text-transform: uppercase;
  margin: 16px 0 10px;
}
.builder-foot {
  display: flex; align-items: center; gap: 10px;
  margin-top: 18px; padding-top: 14px;
  border-top: 1px solid var(--line);
}
</style>
