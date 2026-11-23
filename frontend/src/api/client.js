// API 客户端：端点与返回值解包逻辑与旧版 app.js 保持一致
const API_BASE = '/api'
const PUBLIC_BASE = '/public'

let apiKeyGetter = () => ''
let onUnauthorized = () => {}

export function configureClient({ getKey, unauthorized }) {
  if (getKey) apiKeyGetter = getKey
  if (unauthorized) onUnauthorized = unauthorized
}

export class ApiError extends Error {
  constructor(message, status, data) {
    super(message)
    this.status = status
    this.data = data || {}
    this.details = this.data.mx_details || this.data.details || []
  }
}

async function request(path, { method = 'GET', body, auth = true } = {}) {
  const headers = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  const key = auth ? apiKeyGetter() : ''
  if (key) headers.Authorization = `Bearer ${key}`

  let res
  try {
    res = await fetch(path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body) })
  } catch {
    throw new ApiError('网络异常，无法连接服务器', 0)
  }

  let data = {}
  try { data = await res.json() } catch { data = {} }

  if (!res.ok) {
    const err = new ApiError(data.error || data.message || `HTTP ${res.status}`, res.status, data)
    if (res.status === 401 && auth && key) onUnauthorized(err)
    throw err
  }
  return data
}

const list = d => (Array.isArray(d) ? d : d.data || [])

export const api = {
  // 公共
  publicSettings: () => request(`${PUBLIC_BASE}/settings`, { auth: false }),
  publicStats: () => request(`${PUBLIC_BASE}/stats`, { auth: false }),
  keyLogin: apiKey => request(`${PUBLIC_BASE}/key-login`, { method: 'POST', body: { api_key: apiKey }, auth: false }),
  register: body => request(`${PUBLIC_BASE}/register`, { method: 'POST', body, auth: false }),
  oauthUrl: provider => `${PUBLIC_BASE}/auth/${provider}`,

  // 账户
  me: () => request(`${API_BASE}/me`),
  stats: () => request(`${API_BASE}/stats`),

  // 域名
  domains: () => request(`${API_BASE}/domains`).then(d => (Array.isArray(d) ? d : d.domains || [])),
  submitDomain: domain => request(`${API_BASE}/domains/submit`, { method: 'POST', body: { domain } }),
  getDomainStatus: id => request(`${API_BASE}/domains/${encodeURIComponent(id)}/status`),

  // 邮箱
  createMailbox: body => request(`${API_BASE}/mailboxes`, { method: 'POST', body: body || {} }).then(d => d.mailbox || d),
  listMailboxes: () => request(`${API_BASE}/mailboxes?size=100`).then(list),
  deleteMailbox: id => request(`${API_BASE}/mailboxes/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  extendMailbox: (id, minutes) =>
    request(`${API_BASE}/mailboxes/${encodeURIComponent(id)}/extend`, { method: 'POST', body: minutes ? { minutes } : {} })
      .then(d => d.mailbox || d),
  mailboxEvents: (id, opts) => openEventStream(`${API_BASE}/mailboxes/${encodeURIComponent(id)}/events`, opts),

  // 邮件
  listEmails: mid => request(`${API_BASE}/mailboxes/${encodeURIComponent(mid)}/emails?size=100`).then(list),
  getEmail: (mid, eid) =>
    request(`${API_BASE}/mailboxes/${encodeURIComponent(mid)}/emails/${encodeURIComponent(eid)}`).then(d => d.email || d),
  deleteEmail: (mid, eid) =>
    request(`${API_BASE}/mailboxes/${encodeURIComponent(mid)}/emails/${encodeURIComponent(eid)}`, { method: 'DELETE' }),

  // 管理
  admin: {
    listAccounts: (page = 1, size = 10) => request(`${API_BASE}/admin/accounts?page=${page}&size=${size}`),
    createAccount: username => request(`${API_BASE}/admin/accounts`, { method: 'POST', body: { username } }),
    deleteAccount: id => request(`${API_BASE}/admin/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    addDomain: domain => request(`${API_BASE}/admin/domains`, { method: 'POST', body: { domain } }),
    deleteDomain: id => request(`${API_BASE}/admin/domains/${encodeURIComponent(id)}`, { method: 'DELETE' }),
    toggleDomain: (id, active) =>
      request(`${API_BASE}/admin/domains/${encodeURIComponent(id)}/toggle`, { method: 'PUT', body: { active } }),
    mxImport: (domain, force = false) =>
      request(`${API_BASE}/admin/domains/mx-import`, { method: 'POST', body: { domain, force } }),
    mxRegister: domain => request(`${API_BASE}/admin/domains/mx-register`, { method: 'POST', body: { domain } }),
    refreshDomainMX: () => request(`${API_BASE}/admin/domains/refresh-mx`, { method: 'POST' }),
    getSettings: () => request(`${API_BASE}/admin/settings`),
    saveSettings: body => request(`${API_BASE}/admin/settings`, { method: 'PUT', body }),
  },
}

// SSE 订阅：EventSource 无法带 Authorization 头，这里用 fetch 读流并按 SSE 格式解析。
// onEvent(type, data) 收到事件；返回值 Promise 在连接结束时 resolve，出错时 reject（ApiError）。
export async function openEventStream(path, { signal, onEvent } = {}) {
  const headers = { Accept: 'text/event-stream' }
  const key = apiKeyGetter()
  if (key) headers.Authorization = `Bearer ${key}`

  let res
  try {
    res = await fetch(path, { headers, signal, cache: 'no-store' })
  } catch (e) {
    if (signal?.aborted) return
    throw new ApiError('网络异常，无法连接服务器', 0)
  }
  if (!res.ok || !res.body) {
    let data = {}
    try { data = await res.json() } catch { data = {} }
    const err = new ApiError(data.error || `HTTP ${res.status}`, res.status, data)
    if (res.status === 401 && key) onUnauthorized(err)
    throw err
  }

  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
  let buf = ''
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      buf += value
      let i
      while ((i = buf.search(/\r?\n\r?\n/)) >= 0) {
        const block = buf.slice(0, i)
        buf = buf.slice(i).replace(/^\r?\n\r?\n/, '')
        let type = 'message'
        const lines = []
        for (const line of block.split(/\r?\n/)) {
          if (line.startsWith(':')) continue
          const idx = line.indexOf(':')
          const field = idx < 0 ? line : line.slice(0, idx)
          const val = idx < 0 ? '' : line.slice(idx + 1).replace(/^ /, '')
          if (field === 'event') type = val
          else if (field === 'data') lines.push(val)
        }
        if (!lines.length) continue
        let data = lines.join('\n')
        try { data = JSON.parse(data) } catch { /* 保留原始字符串 */ }
        onEvent?.(type, data)
      }
    }
  } catch (e) {
    if (signal?.aborted) return
    throw new ApiError('实时连接中断', 0)
  } finally {
    reader.releaseLock?.()
  }
}
