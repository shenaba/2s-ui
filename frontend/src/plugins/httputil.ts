import api from './api'
import { i18n } from '@/locales'
import router from '@/router'
import { push } from 'notivue'

export interface Msg {
  success: boolean
  msg: string
  obj: any | null
}

function _handleMsg(msg: any): void {
  if (!isMsg(msg)) {
    return
  }
  if(msg.msg){
    if (!msg.success && msg.msg == "Invalid login") {
      push.error({
        title: i18n.global.t('invalidLogin'),
      })
      logout()
      return
    }
    if (msg.success) {
      const key = 'actions.' + msg.msg
      const label = i18n.global.te(key) ? i18n.global.t(key) : msg.msg
      push.success({
        message: i18n.global.t('success') + ": " + label,
      })
    } else {
      push.error({
        title: i18n.global.t('failed'),
        message: msg.msg
      })
    }
  }
}

export const logout = async () => {
  const response = await HttpUtils.get('api/logout')
  if(response.success){
    router.push('/login')
  }
}

// A non-2xx answer still carries the panel's own Msg body -- checkLogin's 401
// is the one that matters -- and axios rejects it for the status alone. Unwrap
// it rather than reporting the transport error: without this an expired
// session surfaces as "Request failed with status code 401" and never reaches
// the logout path in _handleMsg.
function _errToMsg(e: any): Msg {
  const data = e?.response?.data
  if (data != null && isMsg(data)) {
    return { success: data.success, msg: data.msg, obj: data.obj ?? null }
  }
  return { success: false, msg: e.toString(), obj: null }
}

function _respToMsg(resp: any): Msg {
  const data = resp.data
  if (data == null) {
    return { success: true, msg: "", obj: null }
  } else if (isMsg(data)) {
    if (data.hasOwnProperty('success')) {
        return { success: data.success, msg: data.msg, obj: data.obj || null }
    } else {
        return data
    }
  } else {
    return { success: false, msg: `unknown data: ${data}`, obj: null }
  }
}

function isMsg(obj: any): obj is Msg {
  return Object.hasOwn(obj,'success') && Object.hasOwn(obj,'msg') && Object.hasOwn(obj, 'obj')
}
  
const HttpUtils = {
  async get(url: string, data: object = {}, options: any[] = []): Promise<Msg> {
    let msg: Msg
    try {
        const resp = await api.get(url, { params: data, ...options })
        msg = _respToMsg(resp)
    } catch (e: any) {
        msg = _errToMsg(e)
    }
    _handleMsg(msg)
    return msg
  },
  async post(url: string, data: object | null, options: any = undefined): Promise<Msg> {
    let msg: Msg
    try {
        const resp = await api.post(url, data, options)
        msg = _respToMsg(resp)
    } catch (e: any) {
        msg = _errToMsg(e)
    }
    _handleMsg(msg)
    return msg
  },
}

export default HttpUtils