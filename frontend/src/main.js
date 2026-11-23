import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { router } from './router'
import { configureClient } from './api/client'
import { useSession } from './stores/session'
import { useTheme } from './stores/theme'
import { toast } from './composables/feedback'

import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
import './styles/layout.css'
import './styles/motion.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)

const session = useSession()
useTheme().init()

// 401：Key 被删除或失效时回到登录页
let kicking = false
configureClient({
  getKey: () => session.apiKey,
  unauthorized: () => {
    // 会话尚未校验时由路由守卫处理，避免重复跳转
    if (kicking || !session.verified) return
    kicking = true
    session.clear()
    toast.warn('登录状态已失效，请重新登录')
    router.replace({ name: 'login', query: { redirect: router.currentRoute.value.fullPath } }).finally(() => {
      kicking = false
    })
  },
})

app.use(router)
router.isReady().then(() => app.mount('#app'))
