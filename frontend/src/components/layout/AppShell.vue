<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  LayoutDashboard, Inbox, Globe, BookOpen, Users, Server, Settings, Mail, Menu, KeyRound, LogOut,
  ChevronsUpDown, Sun, Moon, Monitor,
} from '@lucide/vue'
import { useSession } from '../../stores/session'
import { useTheme } from '../../stores/theme'
import { mailboxBus } from '../../composables/mailboxBus'
import CreateMailboxModal from '../mail/CreateMailboxModal.vue'

const session = useSession()
const theme = useTheme()
const route = useRoute()
const router = useRouter()

const drawer = ref(false)
const menuOpen = ref(false)
const menuWrap = ref(null)

const nav = [
  { to: { name: 'dashboard' }, label: '数据看板', icon: LayoutDashboard },
  { to: { name: 'mailboxes' }, label: '我的邮箱', icon: Inbox, match: ['mailboxes', 'mailbox'] },
  { to: { name: 'domains' }, label: '域名', icon: Globe },
  { to: { name: 'docs' }, label: 'API 文档', icon: BookOpen },
]
const adminNav = [
  { to: { name: 'admin-accounts' }, label: '账户管理', icon: Users },
  { to: { name: 'admin-domains' }, label: '域名池', icon: Server },
  { to: { name: 'admin-settings' }, label: '系统设置', icon: Settings },
]

function isActive(item) {
  const names = item.match || [item.to.name]
  return names.includes(route.name)
}

const themeChoice = computed(() => (theme.mode === 'auto' ? 'auto' : theme.theme))
const themes = [
  { v: 'light', label: '浅色', icon: Sun },
  { v: 'dark', label: '深色', icon: Moon },
  { v: 'auto', label: '系统', icon: Monitor },
]

function logout() {
  menuOpen.value = false
  session.logout()
  router.replace({ name: 'login' })
}

function onDocClick(e) {
  if (menuOpen.value && menuWrap.value && !menuWrap.value.contains(e.target)) menuOpen.value = false
}
function onKey(e) {
  if (e.key === 'Escape') {
    menuOpen.value = false
    drawer.value = false
  }
}

watch(() => route.fullPath, () => {
  drawer.value = false
  menuOpen.value = false
})

onMounted(() => {
  document.addEventListener('click', onDocClick)
  document.addEventListener('keydown', onKey)
})
onBeforeUnmount(() => {
  document.removeEventListener('click', onDocClick)
  document.removeEventListener('keydown', onKey)
})
</script>

<template>
  <div class="shell">
    <aside id="sidebar" class="side" :class="{ open: drawer }" aria-label="主导航">
      <RouterLink :to="{ name: 'dashboard' }" class="brand">
        <span v-if="session.logoUrl" class="brand-mark img"><img :src="session.logoUrl" alt="" /></span>
        <span v-else class="brand-mark"><Mail aria-hidden="true" /></span>
        <span class="brand-txt"><strong>{{ session.siteTitle }}</strong><small>智能邮件平台</small></span>
      </RouterLink>

      <nav>
        <div class="nav-g">邮件</div>
        <RouterLink
          v-for="item in nav"
          :key="item.label"
          :to="item.to"
          class="nav"
          :class="{ on: isActive(item) }"
          :aria-current="isActive(item) ? 'page' : undefined"
          :title="item.label"
        >
          <component :is="item.icon" aria-hidden="true" /><span>{{ item.label }}</span>
        </RouterLink>

        <template v-if="session.isAdmin">
          <div class="nav-g">管理</div>
          <RouterLink
            v-for="item in adminNav"
            :key="item.label"
            :to="item.to"
            class="nav"
            :class="{ on: isActive(item) }"
            :aria-current="isActive(item) ? 'page' : undefined"
            :title="item.label"
          >
            <component :is="item.icon" aria-hidden="true" /><span>{{ item.label }}</span>
          </RouterLink>
        </template>
      </nav>

      <div ref="menuWrap" class="side-foot">
        <div v-if="menuOpen" id="user-menu" class="menu" role="menu">
          <div class="menu-label">外观</div>
          <div class="seg" role="radiogroup" aria-label="主题">
            <button
              v-for="t in themes"
              :key="t.v"
              type="button"
              role="radio"
              :aria-checked="themeChoice === t.v ? 'true' : 'false'"
              @click="theme.set(t.v)"
            >
              <component :is="t.icon" aria-hidden="true" />{{ t.label }}
            </button>
          </div>
          <div class="menu-sep" />
          <RouterLink :to="{ name: 'apikey' }" class="menu-item" role="menuitem"><KeyRound aria-hidden="true" />我的 API Key</RouterLink>
          <RouterLink :to="{ name: 'docs' }" class="menu-item" role="menuitem"><BookOpen aria-hidden="true" />API 文档</RouterLink>
          <div class="menu-sep" />
          <button type="button" class="menu-item danger" role="menuitem" @click="logout"><LogOut aria-hidden="true" />退出登录</button>
        </div>
        <button
          type="button"
          class="user-btn"
          aria-haspopup="menu"
          aria-controls="user-menu"
          :aria-expanded="menuOpen ? 'true' : 'false'"
          @click.stop="menuOpen = !menuOpen"
        >
          <span class="avatar" aria-hidden="true">{{ session.username.charAt(0).toUpperCase() }}</span>
          <span class="user-meta">
            <strong>{{ session.username }}</strong>
            <small>{{ session.isAdmin ? '管理员' : '普通用户' }}</small>
          </span>
          <ChevronsUpDown class="chev" aria-hidden="true" />
        </button>
      </div>
    </aside>

    <div v-if="drawer" class="backdrop" @click="drawer = false" />

    <div class="main">
      <div class="mobile-bar">
        <button
          type="button"
          class="icon-btn"
          aria-label="打开菜单"
          aria-controls="sidebar"
          :aria-expanded="drawer ? 'true' : 'false'"
          @click="drawer = true"
        >
          <Menu aria-hidden="true" />
        </button>
        <strong>{{ session.siteTitle }}</strong>
      </div>
      <main id="main" class="page" :class="{ wide: route.meta.wide }" tabindex="-1">
        <RouterView />
      </main>
    </div>

    <CreateMailboxModal :open="mailboxBus.createOpen" @close="mailboxBus.createOpen = false" />
  </div>
</template>
