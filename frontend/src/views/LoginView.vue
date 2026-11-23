<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Mail, KeyRound, UserPlus, Sun, Moon, Monitor, PartyPopper, LogIn, TriangleAlert } from '@lucide/vue'
import { api } from '../api/client'
import { useSession } from '../stores/session'
import { useTheme } from '../stores/theme'
import { toast } from '../composables/feedback'
import KeyValue from '../components/ui/KeyValue.vue'

const session = useSession()
const theme = useTheme()
const router = useRouter()
const route = useRoute()

const tab = ref('login')
const key = ref('')
const username = ref('')
const email = ref('')
const busy = ref(false)
const error = ref('')
const registered = ref(null)
const ready = ref(false)

const s = computed(() => session.settings)
const keyLogin = computed(() => s.value.key_login_enabled !== false)
const linuxdo = computed(() => s.value.linuxdo_login_enabled === true)
const github = computed(() => s.value.github_login_enabled === true)
const oauth = computed(() => linuxdo.value || github.value)
// 与旧版一致：关闭 Key 登录但开启 OAuth 时，仍保留管理员 Key 入口
const showKeyForm = computed(() => keyLogin.value || oauth.value)
const regOpen = computed(() => s.value.registration_open === true || s.value.registration_open === 'true')

const themeChoice = computed(() => (theme.mode === 'auto' ? 'auto' : theme.theme))
const themes = [
  { v: 'light', label: '浅色', icon: Sun },
  { v: 'dark', label: '深色', icon: Moon },
  { v: 'auto', label: '跟随系统', icon: Monitor },
]

onMounted(async () => {
  await session.loadSettings(true)
  ready.value = true
})

function go() {
  const r = route.query.redirect
  router.replace(typeof r === 'string' && r.startsWith('/') && !r.startsWith('//') ? r : { name: 'dashboard' })
}

async function doLogin(k = key.value) {
  const v = String(k || '').trim()
  if (!v) {
    error.value = '请输入 API Key'
    return
  }
  busy.value = true
  error.value = ''
  try {
    const acct = await session.login(v)
    toast.success(`欢迎回来，${acct.username || '用户'}`)
    go()
  } catch (e) {
    error.value = e.status === 403 ? 'API Key 登录已被管理员关闭' : e.status === 401 ? 'API Key 无效' : e.message
  } finally {
    busy.value = false
  }
}

async function doRegister() {
  const u = username.value.trim()
  if (u.length < 2 || u.length > 64) {
    error.value = '用户名长度需为 2–64 个字符'
    return
  }
  busy.value = true
  error.value = ''
  try {
    const body = { username: u }
    if (email.value.trim()) body.email = email.value.trim()
    registered.value = await api.register(body)
  } catch (e) {
    error.value = e.status === 409 ? '用户名已被占用' : e.message
  } finally {
    busy.value = false
  }
}

function switchTab(t) {
  tab.value = t
  error.value = ''
}

function oauthGo(p) {
  window.location.href = api.oauthUrl(p)
}
</script>

<template>
  <div class="auth">
    <div class="auth-theme seg" role="radiogroup" aria-label="主题">
      <button
        v-for="t in themes"
        :key="t.v"
        type="button"
        role="radio"
        :aria-checked="themeChoice === t.v ? 'true' : 'false'"
        :aria-label="t.label"
        :title="t.label"
        @click="theme.set(t.v)"
      >
        <component :is="t.icon" aria-hidden="true" />
      </button>
    </div>

    <main id="main" class="auth-card card">
      <header class="auth-head">
        <span v-if="session.logoUrl" class="brand-mark img lg"><img :src="session.logoUrl" alt="" /></span>
        <span v-else class="brand-mark lg"><Mail aria-hidden="true" /></span>
        <h1>{{ session.siteTitle }}</h1>
        <p>智能邮件接收与管理 · 安全隔离 · 按需分配</p>
      </header>

      <!-- 注册成功 -->
      <section v-if="registered" class="stack">
        <div class="done">
          <span class="done-ico"><PartyPopper aria-hidden="true" /></span>
          <h2>注册成功</h2>
          <p class="small text-2">请立即保存你的 API Key，它只显示这一次。</p>
        </div>
        <KeyValue :value="registered.api_key" copy-label="复制 API Key" />
        <div class="notice warn">
          <TriangleAlert aria-hidden="true" />
          <span>API Key 是唯一的登录凭证，丢失后需要联系管理员重置。</span>
        </div>
        <button type="button" class="btn btn-primary btn-lg btn-block" :disabled="busy" @click="doLogin(registered.api_key)">
          <LogIn aria-hidden="true" />{{ busy ? '登录中…' : '立即登录' }}
        </button>
        <p v-if="error" class="err-t" role="alert">{{ error }}</p>
      </section>

      <template v-else>
        <div class="tabs" role="tablist">
          <button
            v-if="showKeyForm"
            id="tab-login"
            type="button"
            role="tab"
            :aria-selected="tab === 'login' ? 'true' : 'false'"
            aria-controls="panel-login"
            @click="switchTab('login')"
          >
            登录
          </button>
          <button
            id="tab-reg"
            type="button"
            role="tab"
            :aria-selected="tab === 'reg' ? 'true' : 'false'"
            aria-controls="panel-reg"
            :disabled="ready && !regOpen"
            :title="ready && !regOpen ? '管理员已关闭注册' : ''"
            @click="switchTab('reg')"
          >
            注册账户
          </button>
        </div>

        <!-- 登录 -->
        <section v-if="tab === 'login'" id="panel-login" role="tabpanel" aria-labelledby="tab-login" class="stack">
          <form v-if="showKeyForm" class="stack" @submit.prevent="doLogin()">
            <div class="field">
              <label for="login-key">API Key</label>
              <input
                id="login-key"
                v-model="key"
                class="inp inp-mono"
                type="password"
                placeholder="tm_xxxxxxxxxxxx"
                autocomplete="current-password"
                :aria-invalid="error ? 'true' : undefined"
                aria-describedby="login-hint"
              />
              <span id="login-hint" class="hint">
                {{ keyLogin ? '在控制台「我的 API Key」中获取' : 'API Key 登录已关闭，仅管理员 Key 可用于后台维护' }}
              </span>
            </div>
            <p v-if="error" class="err-t" role="alert">{{ error }}</p>
            <button type="submit" class="btn btn-primary btn-lg btn-block" :disabled="busy">
              <KeyRound aria-hidden="true" />{{ busy ? '登录中…' : '登录' }}
            </button>
          </form>

          <div v-if="showKeyForm && oauth" class="or"><span>或</span></div>

          <div v-if="oauth" class="stack oauth">
            <button v-if="linuxdo" type="button" class="btn btn-secondary btn-lg btn-block" @click="oauthGo('linuxdo')">
              使用 Linux DO Connect 登录
            </button>
            <button v-if="github" type="button" class="btn btn-secondary btn-lg btn-block" @click="oauthGo('github')">
              <svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor"><path d="M12 .5A11.5 11.5 0 0 0 8.36 22.9c.58.1.79-.25.79-.56v-2.02c-3.22.7-3.9-1.38-3.9-1.38-.53-1.35-1.3-1.7-1.3-1.7-1.06-.73.08-.72.08-.72 1.18.08 1.8 1.21 1.8 1.21 1.04 1.79 2.74 1.27 3.41.97.1-.76.41-1.27.74-1.56-2.57-.29-5.27-1.29-5.27-5.73 0-1.27.45-2.3 1.2-3.11-.12-.29-.52-1.47.11-3.07 0 0 .98-.31 3.2 1.19a11.1 11.1 0 0 1 5.82 0c2.22-1.5 3.2-1.19 3.2-1.19.63 1.6.23 2.78.11 3.07.75.81 1.2 1.84 1.2 3.11 0 4.45-2.7 5.43-5.28 5.72.42.36.79 1.07.79 2.16v3.2c0 .31.21.67.8.56A11.5 11.5 0 0 0 12 .5Z" /></svg>
              使用 GitHub 登录
            </button>
          </div>

          <p v-if="ready && !showKeyForm && !oauth" class="muted small center">当前没有启用任何登录方式，请联系管理员。</p>
        </section>

        <!-- 注册 -->
        <section v-else id="panel-reg" role="tabpanel" aria-labelledby="tab-reg">
          <form class="stack" @submit.prevent="doRegister">
            <div class="field">
              <label for="reg-username">用户名</label>
              <input id="reg-username" v-model="username" class="inp" placeholder="your_name" autocomplete="username" maxlength="64" />
              <span class="hint">2–64 个字符，注册后会生成专属 API Key</span>
            </div>
            <div class="field">
              <label for="reg-email">联系邮箱 <span class="muted">（可选）</span></label>
              <input id="reg-email" v-model="email" class="inp" type="email" placeholder="contact@example.com" autocomplete="email" />
            </div>
            <p v-if="error" class="err-t" role="alert">{{ error }}</p>
            <button type="submit" class="btn btn-primary btn-lg btn-block" :disabled="busy">
              <UserPlus aria-hidden="true" />{{ busy ? '注册中…' : '注册' }}
            </button>
          </form>
        </section>
      </template>
    </main>

    <a class="gh" href="https://github.com/123nhh/tempmail" target="_blank" rel="noopener noreferrer">GitHub 开源项目</a>
  </div>
</template>

<style scoped>
.auth {
  min-height: 100vh; display: grid; place-items: center; padding: 48px 16px; position: relative;
  background:
    radial-gradient(60rem 30rem at 50% -10%, var(--tint), transparent 70%),
    var(--bg);
}
.auth-theme { position: absolute; top: 16px; right: 16px; }
.auth-card { width: 100%; max-width: 400px; padding: 28px 28px 24px; display: grid; gap: 20px; box-shadow: var(--shadow-pop); }
.auth-head { display: grid; justify-items: center; gap: 6px; text-align: center; }
.auth-head h1 { font-size: 20px; margin-top: 6px; }
.auth-head p { font-size: 13px; color: var(--text-3); }
.brand-mark.lg { width: 44px; height: 44px; border-radius: 12px; }
.brand-mark.lg svg { width: 22px; height: 22px; }
.tabs button { flex: 1; }
.tabs button:disabled { opacity: 0.45; cursor: not-allowed; }
.or { display: flex; align-items: center; gap: 12px; color: var(--text-3); font-size: 12px; }
.or::before, .or::after { content: ""; flex: 1; height: 1px; background: var(--border); }
.oauth { gap: 10px; }
.oauth svg { width: 16px; height: 16px; }
.center { text-align: center; }
.done { display: grid; justify-items: center; gap: 4px; text-align: center; }
.done-ico { width: 44px; height: 44px; border-radius: 50%; display: grid; place-items: center; background: var(--ok-bg); color: var(--ok); margin-bottom: 6px; }
.done-ico svg { width: 22px; height: 22px; }
.done h2 { font-size: 17px; }
.gh { position: absolute; bottom: 16px; font-size: 12px; color: var(--text-3); text-decoration: none; }
.gh:hover { color: var(--accent); }
</style>
