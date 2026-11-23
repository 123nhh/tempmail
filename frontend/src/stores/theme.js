import { defineStore } from 'pinia'

const query = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null
const systemTheme = () => (query?.matches ? 'dark' : 'light')

// 主题：auto 跟随系统 / manual 手动；存储键与旧版一致
export const useTheme = defineStore('theme', {
  state: () => {
    const mode = localStorage.getItem('tm_theme_mode') || (localStorage.getItem('tm_theme') ? 'manual' : 'auto')
    const theme = mode === 'auto' ? systemTheme() : localStorage.getItem('tm_theme') || 'light'
    return { mode, theme }
  },
  getters: {
    label: s => (s.mode === 'auto' ? `跟随系统 · ${s.theme === 'dark' ? '深色' : '浅色'}` : s.theme === 'dark' ? '深色' : '浅色'),
  },
  actions: {
    apply() {
      document.documentElement.dataset.theme = this.theme
      if (this.mode === 'auto') localStorage.removeItem('tm_theme')
      else localStorage.setItem('tm_theme', this.theme)
      localStorage.setItem('tm_theme_mode', this.mode)
    },
    set(choice) {
      if (choice === 'auto') {
        this.mode = 'auto'
        this.theme = systemTheme()
      } else {
        this.mode = 'manual'
        this.theme = choice
      }
      this.apply()
    },
    init() {
      this.apply()
      const onChange = e => {
        if (this.mode !== 'auto') return
        this.theme = e.matches ? 'dark' : 'light'
        this.apply()
      }
      if (query?.addEventListener) query.addEventListener('change', onChange)
      else if (query?.addListener) query.addListener(onChange)
    },
  },
})
