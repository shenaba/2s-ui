import HttpUtils from '@/plugins/httputil'
import { defineStore } from 'pinia'
import { push } from 'notivue'
import { i18n } from '@/locales'
import { Inbound } from '@/types/inbounds'
import { Client } from '@/types/clients'
import { NodeStatus } from '@/types/node'

const Data = defineStore('Data', {
  state: () => ({
    lastLoad: 0,
    // Highest config version applied; guards against a stale snapshot landing
    // after a newer push. See applyLive.
    cseq: 0,
    reloadItems: localStorage.getItem("reloadItems")?.split(',')?? <string[]>[],
    subURI: "",
    os: "",
    enableTraffic: false,
    onlines: {inbound: <string[]>[], outbound: <string[]>[], user: <string[]>[]},
    config: <any>{},
    inbounds: <any[]>[],
    outbounds: <any[]>[],
    services: <any[]>[],
    endpoints: <any[]>[],
    clients: <any>[],
    tlsConfigs: <any[]>[],
    nodes: <any[]>[],
    nodesStatus: <Record<number, NodeStatus>>{},
  }),
  getters: {
    // Detour and route targets, inbound side: every inbound plus the endpoints
    // that actually listen. Endpoints without a listen_port have nothing to
    // route *to*, so they only belong in outboundTags.
    inboundTags(state): string[] {
      return [
        ...(state.inbounds ?? []),
        ...(state.endpoints ?? []).filter((e: any) => e.listen_port > 0),
      ].map((o: any) => o.tag).filter((t: any) => t != null)
    },
    outboundTags(state): string[] {
      return [...(state.outbounds ?? []), ...(state.endpoints ?? [])]
        .map((o: any) => o.tag).filter((t: any) => t != null)
    },
  },
  actions: {
    // Shared by the websocket 'load' topic and the one-shot HTTP load below.
    // Websocket pushes are partial: a missing key means "unchanged", so only
    // present keys are applied.
    applyLive(obj: any) {
      if (obj.onlines) this.onlines = obj.onlines
      if (Object.hasOwn(obj, 'nodesStatus')) this.nodesStatus = obj.nodesStatus ?? {}
      if (obj.lastLog) {
        push.error({
          title: i18n.global.t('error.core'),
          duration: 5000,
          message: obj.lastLog
        })
      }
      if (obj.config) {
        // Config payloads carry a version. The hub adds a subscriber to the
        // broadcast set before building its snapshot, so a push that landed
        // mid-build can arrive first — applying the older snapshot on top of it
        // would silently restore the pre-change config. A backend without the
        // field (older than this) applies unconditionally, as it used to.
        if (typeof obj.cseq === 'number' && obj.cseq <= this.cseq) return
        this.setNewData(obj)
      }
    },
    // Views that read config on mount must wait for the first payload. Bounded
    // on purpose: that payload now arrives over the websocket, so a handshake
    // that never completes would otherwise spin here forever behind a spinner
    // that never clears. On timeout the caller renders what it has.
    async waitReady(timeoutMs = 15000): Promise<boolean> {
      const deadline = Date.now() + timeoutMs
      while (this.lastLoad == 0 && Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 100))
      }
      if (this.lastLoad == 0) {
        console.warn('[2s-ui] no panel data after 15s (websocket not connected?) — config views stay blank rather than edit an empty config')
        return false
      }
      return true
    },
    async loadData() {
      const msg = await HttpUtils.get('api/load', this.lastLoad >0 ? {lu: this.lastLoad} : {} )
      // The HTTP response omits nodesStatus when there are no nodes but means
      // "none" — the seed key keeps that reset; spreading obj over it restores
      // the real value whenever the backend did send one.
      if(msg.success) {
        this.applyLive({ nodesStatus: {}, ...msg.obj })
      }
    },
    setNewData(data: any) {
      // Prefer the server's own stamp: lastLoad is sent back as `lu` and
      // compared against the server's change timestamp, so deriving it from the
      // browser clock made a fast clock miss changes (and, with no poll left,
      // never recover) and a slow one refetch the whole config on every
      // reconnect. The fallback only covers a backend older than this field.
      this.lastLoad = Number.isFinite(data.lu) && data.lu > 0
        ? data.lu
        : Math.floor((new Date()).getTime()/1000)
      if (typeof data.cseq === 'number') this.cseq = data.cseq
      if (data.subURI) this.subURI = data.subURI
      if (data.os) this.os = data.os
      if (data.enableTraffic) this.enableTraffic = data.enableTraffic
      if (data.config) this.config = data.config
      if (Object.hasOwn(data, 'clients')) this.clients = data.clients ?? []
      if (Object.hasOwn(data, 'inbounds')) this.inbounds = data.inbounds ?? []
      if (Object.hasOwn(data, 'outbounds')) this.outbounds = data.outbounds ?? []
      if (Object.hasOwn(data, 'services')) this.services = data.services ?? []
      if (Object.hasOwn(data, 'endpoints')) this.endpoints = data.endpoints ?? []
      if (Object.hasOwn(data, 'tls')) this.tlsConfigs = data.tls ?? []
      if (Object.hasOwn(data, 'nodes')) this.nodes = data.nodes ?? []
    },
    async loadInbounds(ids: number[]): Promise<Inbound[]> {
      const options = ids.length > 0 ? {id: ids.join(",")} : {}
      const msg = await HttpUtils.get('api/inbounds', options)
      if(msg.success) {
        return msg.obj.inbounds
      }
      return <Inbound[]>[]
    },
    async loadClients(id: number): Promise<Client> {
      const options = id > 0 ? {id: id} : {}
      const msg = await HttpUtils.get('api/clients', options)
      if(msg.success) {
        return <Client>msg.obj.clients[0]??{}
      }
      return <Client>{}
    },
    async save (object: string, action: string, data: any, initUsers?: number[]): Promise<boolean> {
      const postData = {
        object: object,
        action: action,
        data: JSON.stringify(data, null, 2),
        initUsers: initUsers?.join(',') ?? undefined
      }
      const msg = await HttpUtils.post('api/save', postData)
      if (msg.success) {
        const objectName = ['tls', 'config'].includes(object) ? object : object.substring(0, object.length - 1)
        push.success({
          title: i18n.global.t('success'),
          duration: 5000,
          message: i18n.global.t('actions.' + action) + " " + i18n.global.t('objects.' + objectName)
        })
        this.setNewData(msg.obj)
      }
      return msg.success
    },
    // Check duplicate client name
    checkClientName (id: number, newName: string): boolean {
      const oldName = id > 0 ? this.clients.findLast((i: any) => i.id == id)?.name : null
      if (newName != oldName && this.clients.findIndex((c: any) => c.name == newName) != -1) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('client.name')
        })
        return true
      }
      return false
    },
    // Check bulk client names
    checkBulkClientNames (names: string[]): boolean {
      const newNames = new Set(names)
      const oldNames = new Set(this.clients.map((c: any) => c.name))
      const allNames = new Set([...oldNames, ...newNames])
      if (newNames.size != names.length || oldNames.size + newNames.size != allNames.size) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('client.name')
        })
        return true
      }
      return false
    },
    // check duplicate tag
    checkTag (object: string, id: number, tag: string): boolean {
      let objects = <any[]>[]
      switch (object) {
        case 'inbound':
          objects = this.inbounds
          break
        case 'outbound':
          objects = this.outbounds
          break
        case 'service':
          objects = this.services
          break
        case 'endpoint':
          objects = this.endpoints
          break
        default:
          return false
      }
      const oldObject = id > 0 ? objects.findLast((i: any) => i.id == id) : null
      if (tag != oldObject?.tag && objects.findIndex((i: any) => i.tag == tag) != -1) {
        push.error({
          message: i18n.global.t('error.dplData') + ": " + i18n.global.t('objects.tag')
        })
        return true
      }
      return false
    },
  }
})

export default Data