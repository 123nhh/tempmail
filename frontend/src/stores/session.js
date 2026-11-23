import { defineStore } from 'pinia'
import { api } from '../api/client'

const KEY = 'tm_apikey'
const ACCOUNT = 'tm_account'

function loadJSON(key) {
  try {
    return JSON.parse(localStorage.getItem(key) || 'null')
  } catch {
    localStorage.removeItem(key)
    return null
  }
}

// 会话：沿用旧版 localStorage 键（OAuth 回调页也直接写这两个键）
export const useSession = defineStore('session', {
  state: () => ({
    apiKey: localStorage.getItem(KEY) || '',
    account: loadJSON(ACCOUNT),
    verified: false,
    settings: {},
    settingsLoaded: false,
  }),
  getters: {
    loggedIn: s => !!s.apiKey && !!s.account,
    isAdmin: s => !!s.account?.is_admin,
    username: s => s.account?.username || '用户',
    siteTitle: s => s.settings.site_title || '云信邮件',
    logoUrl: s => (s.settings.site_logo_url || '').trim(),
  },
  actions: {
    persist(key, account) {
      this.apiKey = key
      this.account = account
      localStorage.setItem(KEY, key)
      localStorage.setItem(ACCOUNT, JSON.stringify(account))
    },
    clear() {
      this.apiKey = ''
      this.account = null
      this.verified = false
      localStorage.removeItem(KEY)
      localStorage.removeItem(ACCOUNT)
    },
    async loadSettings(force = false) {
      if (this.settingsLoaded && !force) return this.settings
      try {
        this.settings = (await api.publicSettings()) || {}
      } catch {
        this.settings = this.settings || {}
      }
      this.settingsLoaded = true
      return this.settings
    },
    // API Key 登录（受“API Key 登录”开关控制）
    async login(key) {
      const acct = await api.keyLogin(key)
      this.persist(acct.api_key || key, acct)
      this.verified = true
      return acct
    },
    // 用已保存的 Key 恢复会话，走 /api/me，不受登录开关影响
    async restore() {
      if (!this.apiKey) return false
      if (this.verified) return true
      try {
        const acct = await api.me()
        this.persist(this.apiKey, { ...(this.account || {}), ...acct })
        this.verified = true
        return true
      } catch (e) {
        if (e.status === 401 || e.status === 403) this.clear()
        else if (this.account) return true // 网络异常时保留本地会话
        return false
      }
    },
    logout() {
      this.clear()
    },
  },
})
