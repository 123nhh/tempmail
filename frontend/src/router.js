import { createRouter, createWebHistory } from 'vue-router'
import { useSession } from './stores/session'
import { toast } from './composables/feedback'

const AppShell = () => import('./components/layout/AppShell.vue')

const routes = [
  { path: '/login', name: 'login', component: () => import('./views/LoginView.vue'), meta: { public: true, title: '登录' } },
  {
    path: '/',
    component: AppShell,
    children: [
      { path: '', name: 'dashboard', component: () => import('./views/DashboardView.vue'), meta: { title: '数据看板', wide: true } },
      { path: 'mailboxes', name: 'mailboxes', component: () => import('./views/MailboxesView.vue'), meta: { title: '我的邮箱' } },
      {
        path: 'mailboxes/:id/:emailId?',
        name: 'mailbox',
        component: () => import('./views/MailboxView.vue'),
        meta: { title: '收件箱', wide: true },
      },
      { path: 'domains', name: 'domains', component: () => import('./views/DomainsView.vue'), meta: { title: '域名' } },
      { path: 'docs', name: 'docs', component: () => import('./views/ApiDocsView.vue'), meta: { title: 'API 文档', wide: true } },
      { path: 'account/key', name: 'apikey', component: () => import('./views/ApiKeyView.vue'), meta: { title: '我的 API Key' } },
      {
        path: 'admin/accounts',
        name: 'admin-accounts',
        component: () => import('./views/admin/AccountsView.vue'),
        meta: { title: '账户管理', admin: true },
      },
      {
        path: 'admin/domains',
        name: 'admin-domains',
        component: () => import('./views/admin/DomainsView.vue'),
        meta: { title: '域名池', admin: true },
      },
      {
        path: 'admin/settings',
        name: 'admin-settings',
        component: () => import('./views/admin/SettingsView.vue'),
        meta: { title: '系统设置', admin: true },
      },
      { path: ':pathMatch(.*)*', name: 'not-found', component: () => import('./views/NotFoundView.vue'), meta: { title: '页面不存在' } },
    ],
  },
]

export const router = createRouter({
  history: createWebHistory(),
  routes,
  scrollBehavior(to, from, saved) {
    if (saved) return saved
    if (to.hash) return { el: to.hash, top: 16, behavior: 'smooth' }
    if (to.name === 'mailbox' && from.name === 'mailbox') return false
    return { top: 0 }
  },
})

router.beforeEach(async to => {
  const session = useSession()
  session.loadSettings()

  if (to.meta.public) {
    // 已登录用户访问登录页直接回看板
    if (to.name === 'login' && session.apiKey && (await session.restore())) return { name: 'dashboard' }
    return true
  }

  const hadKey = !!session.apiKey
  if (!hadKey || !(await session.restore())) {
    if (hadKey) toast.warn('登录状态已失效，请重新登录')
    return { name: 'login', query: to.fullPath !== '/' ? { redirect: to.fullPath } : {} }
  }

  if (to.meta.admin && !session.isAdmin) return { name: 'dashboard' }
  return true
})

router.afterEach(to => {
  const session = useSession()
  const t = to.meta.title
  document.title = t ? `${t} · ${session.siteTitle}` : session.siteTitle
})
