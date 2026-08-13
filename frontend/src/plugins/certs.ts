import { ref } from 'vue'
import HttpUtils from '@/plugins/httputil'
import { i18n } from '@/locales'

/** 「域名与证书」页面上的一条记录，字段与后端 service.CertInfo 一一对应。 */
export interface Cert {
  domain: string
  certFile: string
  keyFile: string
  ca: string
  keyType: string
  /** unix 秒；0 表示证书文件读不到 */
  notAfter: number
  /** unix 秒；0 表示不自动续期（手动登记的证书恒为 0） */
  nextRenew: number
  /** true = acme.sh 托管并自动续期；false = 用户自带、自己维护 */
  managed: boolean
}

// 模块级共享：证书页、面板域名框、订阅域名框都要这份清单，各拉一次纯属浪费。
// 谁先挂载谁触发加载，之后复用，直到有人改动证书（load(true)）。
const certs = ref<Cert[]>([])
let loaded = false
let inflight: Promise<Cert[]> | null = null
// 代次号：并发的两个请求谁后【发起】谁说了算，先发的响应晚到也不能把新快照盖回去
let gen = 0

export function useCerts() {
  return certs
}

/**
 * 清单是否已成功加载过。派生逻辑（按域名回填证书路径）必须看它：一次失败的
 * api/certs 会留下空清单，把空清单当权威去派生，等于把面板正在用的证书路径抹掉。
 */
export function certsLoaded() {
  return loaded
}

/**
 * 取证书清单。force=true 时强制回源——申请、登记、删除之后必须传，否则域名框里
 * 的建议还是旧的。
 *
 * 并发调用共用同一个请求：两个域名框同时挂载时不该打两次接口。
 */
export async function loadCerts(force = false): Promise<Cert[]> {
  if (loaded && !force) return certs.value
  if (inflight && !force) return inflight

  const g = ++gen
  const p = (async () => {
    const r = await HttpUtils.get('api/certs')
    // 只有仍是最新一代才落盘：force 请求发出后，先前那个非 force 请求的响应
    // 可能才姗姗来迟，不设代次它会把 force 的结果整个盖回旧快照
    if (g === gen && r.success && Array.isArray(r.obj)) {
      certs.value = r.obj
      loaded = true
    }
    return certs.value
  })()
  inflight = p
  try {
    return await p
  } finally {
    // 只清自己那一个：先发请求的 finally 不能把还在飞的后发请求从槽位上抹掉
    if (inflight === p) inflight = null
  }
}

/**
 * 按域名查证书。大小写不敏感（后端清单已统一小写，这里再兜一层输入侧的大小写）；
 * 通配符证书按单级泛匹配——`*.example.com` 覆盖 `foo.example.com`，与浏览器的
 * 证书校验规则一致，让手里只有通配符证书的用户也能正常派生路径。
 */
export function findCert(domain: string): Cert | undefined {
  const d = (domain ?? '').trim().toLowerCase()
  if (!d) return undefined
  const exact = certs.value.find((c) => c.domain.toLowerCase() === d)
  if (exact) return exact
  const dot = d.indexOf('.')
  if (dot <= 0) return undefined
  const wildcard = '*' + d.slice(dot)
  return certs.value.find((c) => c.domain.toLowerCase() === wildcard)
}

/** 剩余天数。后端只给 unix 秒：服务器时区不一定是用户的，它算好会差一天。 */
export function daysLeft(unixSec: number): number {
  return Math.floor((unixSec * 1000 - Date.now()) / 86400000)
}

export type CertNote = { text: string; kind: 'ok' | 'warn' | 'mute'; offerIssue: boolean }

// 域名框下面那行回执。反代开着却没证书是【保存必失败】的组合(生成不出 vhost),
// 与其等后端报错不如当场说清楚。
export function certNote(domain: string, behindProxy: boolean): CertNote | null {
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
