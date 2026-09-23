# 2S-UI 设计系统 — 组件手册

这套面板 UI 用**自有设计系统**实现(不依赖 Vuetify/Element Plus 等组件库),所有组件手写、零样式覆盖层,以便像素级贴合设计稿。本文是维护这套 UI 的权威参照:改样式、加页面、加组件都先看这里。

> 为什么不用第三方组件库:设计稿是高保真定制稿,任何成熟库的默认外观都对不上,需要大量覆盖样式才能贴近——那层覆盖反而更难维护。手写组件换来的是 100% 还原 + 零覆盖层 + 完全可控。代价(没有公开文档/社区)由本文 + 统一的 API 规范来补。

---

## 1. 目录结构

```
src/
├─ styles/
│  ├─ tokens.css        # 设计 token(颜色/圆角/字体/缓动)+ 明暗主题 —— 改主题来这里
│  └─ app.css           # 布局类、表格、抽屉/弹窗、表单网格等
├─ components/
│  ├─ ui/               # 全部基础组件(本文第 4-9 节)
│  │  └─ icons.ts       # 描边图标注册表(name → SVG path)
│  └─ charts/           # 图表组件(ECharts,第 10 节)
├─ layouts/
│  ├─ default/          # 应用外壳:Sidebar / Topbar / LangMenu / Default
│  └─ drawers/          # 业务弹层(Stats/Qr/Logs/Backup/Editor… 第 11 节)
│     └─ <entity>/      # 各实体编辑抽屉(client/inbound/outbound/…)
├─ views/               # 13 个路由页
├─ plugins/             # colors / utils / httputil / clipboard / randomUtil
└─ locales/             # 6 语言;ui.* 为本次重设计新增文案
```

约定:**组件按需显式 import,无全局注册** —— `import Btn from '@/components/ui/Btn.vue'`。

---

## 2. 全局规则(写任何页面都遵守)

- **颜色/间距/字号一律用 token**(`var(--brand)`、`var(--surface-3)`、`var(--text-2)`、`var(--line)`…),不硬编码色值。协议/动作等语义色走 `@/plugins/colors`。
- **等宽数字/代码加 `class="mono"`**(启用 tabular-nums + JetBrains Mono)。
- **文案走 i18n**:`$t('key')`。优先复用既有 key(`actions.*`、`objects.*`、`client.*`、`in.*`、`out.*`…),重设计新增文案在 `ui.*` 下(6 语言已补齐,见 `scripts/gen-ui-locales.mjs`)。
- **数据/保存走 Pinia `Data` store**(`@/store/modules/data`):`loadData()`、`save(object, action, data, initUsers?)`;HTTP 用 `@/plugins/httputil`。
- 页面根元素:`<div class="page-stack fade-up">`(竖向堆叠 gap 16,带入场动画);更宽间距用 `page-stack-lg`(18)。

---

## 3. 设计 token 速查（`tokens.css`，明暗两套）

| 类别 | 变量 |
|---|---|
| 品牌 | `--brand #635bff` · `--brand-600`(hover)· `--brand-700`(active)· `--brand-soft`(12% 底) |
| 语义色 | `--cyan` `--emerald` `--amber` `--rose` `--violet` `--pink` `--sky` |
| 表面 | `--bg` · `--surface` · `--surface-2` · `--surface-3` · `--elevated` |
| 文字 | `--text` · `--text-2` · `--text-3` |
| 线条 | `--line`(.07)· `--line-2`(.12) |
| 圆角 | `--radius-xs 7` · `-sm 10` · `-md 14` · `-lg 18` · `-xl 24` |
| 字体 | `--font-ui`(Manrope)· `--font-mono`(JetBrains Mono) |
| 缓动 | `--ease cubic-bezier(.22,.61,.36,1)` |
| 阴影 | `--shadow-card` · `--shadow-pop`(浮层) |

**换主题色 = 改 `tokens.css` 里的 `--brand` 三档**,全站自动生效。明暗由 `<html data-theme="dark|light">` 切换(`store/modules/app.ts` 的 `toggleTheme`)。

---

## 4. 布局类（`app.css`）

| class | 用途 |
|---|---|
| `.page-stack` / `.page-stack-lg` | 页面竖向堆叠(gap 16 / 18) |
| `.toolbar` | 页头操作条(flex/wrap/gap 10);内放 `.grow` 占位撑开 |
| `.entity-grid` | 实体卡网格 `auto-fill minmax(248px,1fr)` gap 14 |
| `.grid2` | 表单两列 |
| `.field-grid` | 表单自适应网格 `minmax(180px,1fr)` |
| `.form-divider` | 表单分隔线 `<hr class="form-divider">` |
| `.dtable` | 数据表(大写表头、行 hover、`tr.clickable`) |
| `.kv-row` | 卡内 key/value 行（`.k` / `.v` / `.v.mono`） |
| `.input` | 文本框/textarea 通用样式(`<input class="input">`、`<textarea class="input mono">`) |
| `.card` | 卡片容器 |
| `.mono` | 等宽数字 |

> 下拉**不要用原生 `<select>`**(OS 渲染的弹层在暗色下很丑)——用 `<Select>`(第 5 节)。

---

## 5. 表单控件

| 组件 | Props | Emits | Slots | 说明 |
|---|---|---|---|---|
| `Field` | `label?` `hint?` `mb=15` | — | default | 表单项外壳:标签 + 控件 + 提示 |
| `Select` | `modelValue` `disabled?` | `update:modelValue` `change` | default | **替代原生 select**;slot 里照常写 `<option>`/`<optgroup>`,组件直接解析 slot vnode(绑定值保留原始类型,数字不会变字符串),渲染暗色卡片浮层(分组、选中高亮、Teleport 不被弹窗裁切)。键盘:↑↓/Home/End 移动高亮、Enter/Space 选中、Esc 关闭;带 listbox/option ARIA |
| `Toggle` | `modelValue=false` `scale=1` | `update:modelValue` | — | 开关;`role="switch"` + `aria-checked`,Tab/Space 可用 |
| `Check` | `checked?` | `toggle` | — | 复选方块（**非 v-model**,便于行内 `@click.stop`）;Tab 可聚焦,Space/Enter 合成真实 click(与鼠标同事件路径,包在 `<label @click>` 里不会双触发) |
| `CheckLabel` | `modelValue` `label` | `update:modelValue` | — | 复选 + 文字 |
| `SwitchLabel` | `modelValue` `label` | `update:modelValue` | — | 行内开关 + 文字 |
| `Segmented` | `modelValue` `options:[value,label][]` `block?` | `update:modelValue` | — | 分段选择;`block` = 抽屉内满宽 |
| `KeyInput` | `modelValue?` `placeholder?` `secret?` `title?` | `update:modelValue` `regenerate` | — | 密钥框 + 重新生成按钮（密码/UUID） |
| `DateTimeInput` | `modelValue`(**epoch 秒**,0=未填) `emptyLabel?`(未填时显示的文案,默认「无限」) | `update:modelValue` | — | 到期时间、下次重置;0 的含义随字段而定,不是「无限期」的字段要传 `emptyLabel`;`fa` locale 自动用波斯历 |
| `ChipSelect` | `modelValue:string[]` `options:{title,value}[]` `label?` `placeholder?` | `update:modelValue` | — | 下拉多选,选中以 chip 展示;面板 Teleport 到 body(弹窗内不被 `overflow` 裁切),视口不足自动向上翻转,chips 换行时自动跟随锚点。键盘 combobox 模式:焦点始终留在锚点,↑↓ 移高亮、Enter/Space 切换选中(面板不关)、Esc 关闭 |
| `MultiPick` | `modelValue:string[]` `options:string[]` | `update:modelValue` | — | chip 多选(selector/urltest 成员） |

```vue
<Field :label="$t('type')">
  <Select v-model="form.type">
    <optgroup :label="$t('ui.proxyProtocols')">
      <option v-for="t in proxyTypes" :key="t" :value="t">{{ t }}</option>
    </optgroup>
  </Select>
</Field>
<Field :label="$t('client.name')"><input class="input" v-model="form.name" /></Field>
<SwitchLabel v-model="form.enable" :label="$t('enable')" />
```

---

## 6. 基础元素

| 组件 | Props | 说明 |
|---|---|---|
| `Ico` | `name` `size=18` `stroke=1.75` | 描边图标;`name` 见 `ui/icons.ts` 注册表 |
| `Btn` | `variant='ghost'(默认)\|'primary'\|'subtle'\|''` `sm` `icon` `loading` `disabled` `type` | slot 放 `<Ico/> 文本`;`icon` = 方形图标按钮 |
| `Chip` | `color?` `dot?` | `color` = 预设名(emerald/rose/amber/brand/cyan)或任意 CSS 色(自动 tint） |
| `Avatar` | `name` `size=30` | 按名字 hash 的渐变头像 |
| `Logo` | `size=30` | 品牌标 |

```vue
<Btn variant="primary" sm @click="add"><Ico name="plus" :size="16" /> {{ $t('actions.add') }}</Btn>
<Chip color="emerald" dot>{{ $t('online') }}</Chip>
```

---

## 7. 卡片 · 面板 · Settings 行

| 组件 | Props | Slots | 说明 |
|---|---|---|---|
| `DPanel` | `title` `sub?` `pad=20` | `right` default | 面板卡(标题行 + 右槽 + 内容);`pad=0` 用于信息行列表 |
| `EntityCard` | `title` `type?` `color?` `icon?` `rows:EntityRow[]` | `chip` `actions` | 实体卡(Outbounds/Services/TLS…);`rows` = `{k,v,mono?,color?}[]`;`#actions` 放 `CardBtn` |
| `RuleCard` | `n` `logical?` `mode?` `action` `targetKey` `target?` `rules` `invert?` | `actions` | 编号规则卡(Rules/DNS),`@edit`/`@del` |
| `SettingsGroup` | `title?` `desc?` | default | Settings 卡片容器 |
| `SRow` | `label` `hint?` | default | Settings 行(左标签 + 右控件,移动端纵排) |
| `ToggleRow` | `modelValue` `label` `desc?` | — | Settings 开关行 |
| `MSwitchRow` | `modelValue` `label` `desc?` | — | 圆角开关区块(抽屉内 TLS/Mux 等开关) |
| `SectionLabel` | — | default | 区块小标题(大写字距) |
| `MHint` | — | default | 虚线提示框 |
| `EmptyState` | `icon='search'` `title` `desc?` | default | 空状态 |

```vue
<EntityCard :title="o.tag" :type="o.type" :color="outColor(o.type)" icon="outbound" :rows="rows(o)">
  <template #chip><Chip v-if="online">{{ $t('online') }}</Chip></template>
  <template #actions>
    <CardBtn icon="edit" @click="edit(o)" />
    <CardBtn icon="trash" border danger @click="del(o)" />
  </template>
</EntityCard>
```

---

## 8. 弹层 · 抽屉 · 标签页

> 编辑界面统一是**居中弹窗**(`.drawer-panel` 已定位为屏幕居中,缩放+淡入)。`Drawer`/`MDrawer`/`Modal` 共用这套定位;弹窗里再开确认框/编辑器会正确叠加。
>
> 浮层公共行为在 `ui/overlay.ts`(Esc 覆盖栈 / body 滚动锁 / 焦点陷阱):`Modal`/`Drawer` 打开即锁背景滚动 + 焦点陷阱(Tab 在面板内循环,关闭时焦点还原到触发元素),`role="dialog" aria-modal`。**Esc 只关最上层**(如 drawer 内开 Select,Esc 先关 Select 再关 drawer)。z-index 阶梯:drawer 40/41 → modal 60 → pop/sel/chip 浮层 98/99(全部 Teleport 到 body,永远压在弹窗之上)。新写浮层组件一律走 `pushOverlay()`,不要自己监听 Esc。

| 组件 | Props | Emits | Slots | 说明 |
|---|---|---|---|---|
| `MDrawer` | `open` `icon?` `color?` `title?` `sub?` `saveLabel?` `width=500` `loading?` | `close` `save` | default | **实体编辑弹窗**:带图标头 + Cancel/Save 脚;每次打开自动 remount 内容 |
| `Drawer` | `open` `width=460` | `close` | `title` default `footer` | 通用弹窗外壳(自定义头/脚时用） |
| `Modal` | `open` `title?` `width=720` | `close` | default `footer` | 居中模态 |
| `DeleteConfirm` | `open` `loading?` | `close` `confirm` | — | 删除确认框(9 个列表页共用):标题/正文/危险色全部内置,只接状态。本地删除(Rules/Dns)不传 `loading` |
| `Pop` | `width?` `minWidth?` `align='end'` `direction='down'` | — | `trigger` default | 弹出菜单:`#trigger="{ toggle }"` 放触发器,默认槽 `="{ close }"` 放 `.pop-item`。菜单 Teleport 到 body 按触发器定位(在 `overflow` 容器/弹窗里不会被裁切),`align` 是逻辑端(RTL 自动镜像),`direction` 是首选方向、视口不足自动翻转 |
| `Tabs` | `modelValue` `tabs:[key,label][]` `page?` `mb=18` | `update:modelValue` | — | 下划线标签;`page` = 页级 tab |
| `IconBtn` | `name` `title?` `danger?` | — | — | 表格行内图标按钮 |
| `CardBtn` | `icon` `label?` `border?` `danger?` `disabled?` | — | — | 卡片底部动作钮(`label` 空 = 46px 方块,`border` 左分隔线) |

---

## 9. 编辑弹窗标准模式（所有实体抽屉一致）

```vue
<MDrawer :open="open" icon="outbound" :color="outColor(form.type)"
  :title="isNew ? $t('ui.out.new') : item.tag"
  :save-label="isNew ? $t('actions.add') : $t('actions.save')"
  :loading="saving" @close="$emit('close')" @save="save">

  <Field :label="$t('type')">
    <Select v-model="form.type"><option v-for="t in types" :key="t" :value="t">{{ t }}</option></Select>
  </Field>
  <div class="grid2"><Field …/><Field …/></div>
  <MSwitchRow v-model="form.tlsOn" :label="$t('ui.tlsReality')" />
</MDrawer>
```

```ts
// 父组件传 item(null=新建);组件内监听 open 初始化本地 form
watch(() => props.open, (v) => { if (v) form.value = props.item ? clone(props.item) : createOutbound() })
const save = async () => {
  saving.value = true
  const ok = await Data().save('outbounds', isNew.value ? 'new' : 'edit', form.value)
  saving.value = false
  if (ok) emit('close')   // payload 结构须与旧逻辑逐字段一致
}
```

---

## 10. 图表（`components/charts/`,ECharts、随主题）

底层 **ECharts**(`echarts/core` 按需引入 + `SVGRenderer`,模块注册在 `plugins/echarts.ts`)。各图表共用 `charts/useChart.ts`:`shallowRef` 存实例、`ResizeObserver` 跟容器尺寸、`MutationObserver` 跟 `data-theme` 重渲染、卸载时 `dispose`;配色经 `resolveColor/cssVar/withAlpha` 从 CSS 变量解析(ECharts 不认 `var()`)。**加新图表类型** → 先在 `echarts.ts` `use()` 注册对应 charts/components 模块,再复用 `useChart`。`BarMini` 例外,是纯 CSS 进度条。

| 组件 | Props |
|---|---|
| `Gauge` | `value`(0-100)`color` `label` `sub` `size=132` —— 径向仪表 |
| `AreaChart` | `data:number[]` `data2?`(第二系列虚线)`color` `color2` `height=160` —— 平滑面积图 |
| `Spark` | `data:number[]` `color` `width=110` `height=34` —— 迷你折线 |
| `Donut` | `data:{value,color}[]` `size=150` `thickness=18` |
| `BarMini` | `value`(0-100)`color` `height=6` —— 进度条 |

---

## 11. 共享业务弹层 · 工具

| 模块 | 契约 |
|---|---|
| `layouts/drawers/StatsModal.vue` | `:visible :resource(对应 objects.* key) :tag @close`;走 `api/stats`,10s 自刷 |
| `layouts/drawers/QrModal.vue` | `:visible :id(client) @close` |
| `LogsModal` / `BackupModal` / `UsageStatsModal` | `:visible @close` |
| `EditorModal.vue` | `:open :title :content @save(value) @close`(JSON/YAML 编辑) |
| `plugins/colors.ts` | `protoColor` `outColor` `dnsColor` `actionColor`(语义色统一来源) |
| `plugins/utils.ts` | `HumanReadable.sizeFormat(bytes)` `formatSecond` `remainedDays(epochSec)` |
| `plugins/clipboard.ts` | `copyToClipboard(txt, containerId?)`(带 toast) |
| `store/modules/app.ts` | `theme` `toggleTheme()` `setLocale(l)` `sidebarOpen`(移动端侧栏) |

---

## 12. 维护配方

**改主题色** → `tokens.css` 改 `--brand` 三档(+ 需要的语义色),全站生效。

**加一个新页面**
1. `views/Xxx.vue`:根 `<div class="page-stack fade-up">` + `.toolbar` + 内容;数据用 `Data()` store。
2. `router/index.ts` 注册路由;`layouts/default/Sidebar.vue` 加导航项 + `ui/icons.ts` 配图标。

**给实体加一个编辑弹窗** → 拷一个现成的 `layouts/drawers/<entity>/XxxDrawer.vue`,套第 9 节模式,改字段与 `Data().save(object,…)` 的 object 名。

**加一种协议/服务表单** → 在 `components/forms/in|out/` 加 `.vue`,父抽屉按 `form.type` 条件渲染(参照现有协议表单)。

**改/加一个基础组件** → 在 `components/ui/` 内改;保持 API 规范:
- 输入类用 `v-model`(`modelValue` + `update:modelValue`);多值用 `string[]`。
- 颜色/尺寸吃 token,不写死。
- 改完跑 `npx vue-tsc --noEmit`(必须 0 错误);本文表格同步更新。

**加图标** → `ui/icons.ts` 注册 `name: '<path d…>'`,`<Ico name="…" />` 即可用(描边 1.75、24 网格)。

---

## 13. 验证

```bash
npm run dev            # :3000 dev(proxy /app/api → 后端 :2095)
npm run build          # vue-tsc 类型检查 + 生产构建 → dist/
```

无单测;改完构建 + 浏览器逐页手动验收(明暗主题 / 移动端 ≤820px / fa RTL)。生产构建产物嵌入后端 `web/html/`。
