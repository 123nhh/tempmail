<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { Save, RotateCcw, Globe, LogIn, Mail, Server, Megaphone, Info } from '@lucide/vue'
import PageHeader from '../../components/ui/PageHeader.vue'
import ToggleSwitch from '../../components/ui/ToggleSwitch.vue'
import SkeletonRows from '../../components/ui/SkeletonRows.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { useSession } from '../../stores/session'

const session = useSession()
const origin = window.location.origin

// 后端所有值都是字符串；布尔项用 'true' / 'false'
const BOOLS = ['registration_open', 'key_login_enabled', 'linuxdo_login_enabled', 'github_login_enabled', 'rate_limit_enabled']
const SECRETS = ['linuxdo_client_secret', 'github_client_secret']
const KEYS = [
  ...BOOLS,
  'site_title', 'site_logo_url', 'announcement',
  'mailbox_ttl_minutes', 'max_mailboxes_per_user', 'default_domain',
  'smtp_server_ip', 'smtp_hostname',
  'linuxdo_client_id', 'linuxdo_redirect_url', 'github_client_id', 'github_redirect_url',
  ...SECRETS,
]
// key_login_enabled 未设置时视为开启，与 /public/settings 一致
const DEFAULT_TRUE = ['key_login_enabled']

const loading = ref(true)
const saving = ref(false)
const form = reactive({})
const orig = ref({})
const secretSet = reactive({ linuxdo_client_secret: false, github_client_secret: false })

const dirty = computed(() => KEYS.some(k => (SECRETS.includes(k) ? !!form[k] : form[k] !== orig.value[k])))

function fromServer(s) {
  const o = {}
  for (const k of KEYS) {
    if (BOOLS.includes(k)) o[k] = DEFAULT_TRUE.includes(k) ? s[k] !== 'false' : s[k] === 'true'
    else o[k] = SECRETS.includes(k) ? '' : (s[k] ?? '')
  }
  return o
}

async function load() {
  loading.value = true
  try {
    const s = await api.admin.getSettings()
    const o = fromServer(s)
    orig.value = o
    Object.assign(form, o)
    for (const k of SECRETS) secretSet[k] = s[`${k}_set`] === 'true'
  } catch (e) {
    toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function reset() {
  Object.assign(form, orig.value)
}

const ttlErr = computed(() => {
  const v = String(form.mailbox_ttl_minutes ?? '').trim()
  if (!v) return ''
  return /^\d+$/.test(v) && +v > 0 ? '' : '请输入正整数'
})
const maxErr = computed(() => {
  const v = String(form.max_mailboxes_per_user ?? '').trim()
  if (!v) return ''
  return /^\d+$/.test(v) ? '' : '请输入非负整数'
})

async function save() {
  if (ttlErr.value || maxErr.value) {
    toast.error('请先修正表单中的错误')
    return
  }
  // 只提交有改动的键；Secret 留空表示不修改
  const body = {}
  for (const k of KEYS) {
    if (SECRETS.includes(k)) {
      if (form[k]) body[k] = form[k]
      continue
    }
    if (form[k] === orig.value[k]) continue
    body[k] = BOOLS.includes(k) ? String(!!form[k]) : String(form[k]).trim()
  }
  if (!Object.keys(body).length) return
  saving.value = true
  try {
    await api.admin.saveSettings(body)
    toast.success('设置已保存')
    await Promise.all([load(), session.loadSettings(true)])
  } catch (e) {
    toast.error(`保存失败：${e.message}`)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <PageHeader title="系统设置" sub="修改后点击保存，立即生效">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="!dirty || saving" @click="reset">
        <RotateCcw aria-hidden="true" />撤销修改
      </button>
      <button type="submit" form="settings-form" class="btn btn-primary" :disabled="!dirty || saving || loading">
        <Save aria-hidden="true" />{{ saving ? '保存中…' : '保存设置' }}
      </button>
    </template>
  </PageHeader>

  <section v-if="loading" class="card"><SkeletonRows :rows="6" :cols="2" /></section>

  <form v-else id="settings-form" class="grid" @submit.prevent="save">
    <section class="card">
      <div class="card-h"><h2><Globe aria-hidden="true" />站点</h2></div>
      <div class="card-b stack">
        <div class="field">
          <label for="s-title">站点名称</label>
          <input id="s-title" v-model="form.site_title" class="inp" placeholder="TempMail" maxlength="64" />
        </div>
        <div class="field">
          <label for="s-logo">Logo 地址</label>
          <input id="s-logo" v-model="form.site_logo_url" class="inp" placeholder="https://…/logo.png" />
          <span class="hint">留空使用默认图标</span>
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-h"><h2><LogIn aria-hidden="true" />注册与登录</h2></div>
      <div class="card-b stack">
        <div class="sw-row">
          <div><strong>开放自行注册</strong><p class="hint">允许访客在登录页注册并获取 API Key</p></div>
          <ToggleSwitch v-model="form.registration_open" label="开放自行注册" />
        </div>
        <div class="sw-row">
          <div><strong>API Key 登录</strong><p class="hint">关闭后登录页不再显示 Key 登录入口</p></div>
          <ToggleSwitch v-model="form.key_login_enabled" label="API Key 登录" />
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-h"><h2><Mail aria-hidden="true" />邮箱</h2></div>
      <div class="card-b stack">
        <div class="field">
          <label for="s-ttl">邮箱有效期（分钟）</label>
          <input id="s-ttl" v-model="form.mailbox_ttl_minutes" class="inp" inputmode="numeric" placeholder="30" :aria-invalid="ttlErr ? 'true' : undefined" />
          <span v-if="ttlErr" class="err-t">{{ ttlErr }}</span>
          <span v-else class="hint">新建邮箱在此时长后自动删除，留空为 30 分钟</span>
        </div>
        <div class="field">
          <label for="s-max">每用户邮箱上限</label>
          <input id="s-max" v-model="form.max_mailboxes_per_user" class="inp" inputmode="numeric" placeholder="不限" :aria-invalid="maxErr ? 'true' : undefined" />
          <span v-if="maxErr" class="err-t">{{ maxErr }}</span>
          <span v-else class="hint warn-hint"><Info aria-hidden="true" />当前后端版本只保存该值，创建邮箱时不做限制</span>
        </div>
        <div class="field">
          <label for="s-dd">默认域名</label>
          <input id="s-dd" v-model="form.default_domain" class="inp inp-mono" placeholder="example.com" />
          <span class="hint warn-hint"><Info aria-hidden="true" />当前后端版本只保存该值，随机创建时不会优先使用</span>
        </div>
        <div class="sw-row">
          <div>
            <strong>速率限制开关</strong>
            <p class="hint warn-hint"><Info aria-hidden="true" />当前后端版本只保存该值；实际限流由 RATE_LIMIT 环境变量控制</p>
          </div>
          <ToggleSwitch v-model="form.rate_limit_enabled" label="速率限制开关" />
        </div>
      </div>
    </section>

    <section class="card">
      <div class="card-h"><h2><Server aria-hidden="true" />邮件服务器</h2></div>
      <div class="card-b stack">
        <p class="hint">用于生成用户添加域名时的 DNS 记录提示，以及 MX 检测时比对的目标地址。</p>
        <div class="field">
          <label for="s-ip">服务器公网 IP</label>
          <input id="s-ip" v-model="form.smtp_server_ip" class="inp inp-mono" placeholder="203.0.113.10" />
        </div>
        <div class="field">
          <label for="s-host">邮件服务器主机名</label>
          <input id="s-host" v-model="form.smtp_hostname" class="inp inp-mono" placeholder="mail.example.com" />
          <span class="hint">MX 记录指向的主机名，需要有指向上方 IP 的 A 记录</span>
        </div>
      </div>
    </section>

    <section class="card wide">
      <div class="card-h"><h2><Megaphone aria-hidden="true" />公告</h2></div>
      <div class="card-b field">
        <label for="s-ann" class="sr-only">公告内容</label>
        <textarea id="s-ann" v-model="form.announcement" class="inp" rows="3" placeholder="显示在所有用户看板顶部，留空不显示"></textarea>
        <span class="hint">纯文本。修改内容后，之前关闭过公告的用户会重新看到。</span>
      </div>
    </section>

    <section v-for="p in [{ id: 'linuxdo', name: 'LinuxDO' }, { id: 'github', name: 'GitHub' }]" :key="p.id" class="card">
      <div class="card-h">
        <h2>{{ p.name }} OAuth</h2>
        <ToggleSwitch v-model="form[`${p.id}_login_enabled`]" :label="`启用 ${p.name} 登录`" />
      </div>
      <div class="card-b stack">
        <div class="field">
          <label :for="`s-${p.id}-id`">Client ID</label>
          <input :id="`s-${p.id}-id`" v-model="form[`${p.id}_client_id`]" class="inp inp-mono" autocomplete="off" />
        </div>
        <div class="field">
          <label :for="`s-${p.id}-sec`">Client Secret</label>
          <input
            :id="`s-${p.id}-sec`"
            v-model="form[`${p.id}_client_secret`]"
            type="password"
            class="inp inp-mono"
            autocomplete="new-password"
            :placeholder="secretSet[`${p.id}_client_secret`] ? '已配置，留空保持不变' : '未配置'"
          />
        </div>
        <div class="field">
          <label :for="`s-${p.id}-cb`">回调地址</label>
          <input :id="`s-${p.id}-cb`" v-model="form[`${p.id}_redirect_url`]" class="inp inp-mono" :placeholder="`${origin}/public/auth/${p.id}/callback`" />
          <span class="hint">留空时自动使用上面的默认地址；需与 {{ p.name }} 应用中登记的回调地址完全一致</span>
        </div>
      </div>
    </section>
  </form>
</template>

<style scoped>
.grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; align-items: start; }
.wide { grid-column: 1 / -1; }
.sw-row { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.sw-row strong { font-size: 13px; font-weight: 500; }
.sw-row .hint { margin-top: 2px; }
.warn-hint { display: flex; gap: 4px; align-items: flex-start; color: var(--warn); }
.warn-hint svg { width: 13px; height: 13px; flex: none; margin-top: 2px; }
textarea.inp { height: auto; padding: 8px 10px; resize: vertical; }
@media (max-width: 900px) { .grid { grid-template-columns: minmax(0, 1fr); } }
</style>
