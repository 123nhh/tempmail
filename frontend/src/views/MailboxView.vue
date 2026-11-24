<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, RefreshCw, Search, Trash2, MailOpen, Inbox, Mail, TimerReset, Radio, Copy, Check } from '@lucide/vue'
import EmptyState from '../components/ui/EmptyState.vue'
import TtlCell from '../components/mail/TtlCell.vue'
import EmailReader from '../components/mail/EmailReader.vue'
import { api } from '../api/client'
import { confirm, copyText, toast } from '../composables/feedback'
import { extendMailbox, mailboxBus, mailboxesChanged } from '../composables/mailboxBus'
import { useMailboxLive } from '../composables/live'
import { checkExpiry, forgetExpiry } from '../composables/expiryReminder'
import { useNow, usePoller } from '../composables/poller'
import { initial, isExpired, senderName, timeAgo } from '../utils/format'
import { extractOtp } from '../utils/otp'

const route = useRoute()
const router = useRouter()
const now = useNow(15000)

const id = computed(() => String(route.params.id))
const emailId = computed(() => (route.params.emailId ? String(route.params.emailId) : ''))

const mailbox = ref(null)
const emails = ref([])
const loading = ref(true)
const missing = ref(false)
const q = ref('')
const unseen = ref(new Set())
const newCount = ref(0)
const baseTitle = document.title

const filtered = computed(() => {
  const k = q.value.trim().toLowerCase()
  if (!k) return emails.value
  return emails.value.filter(e => `${e.sender || ''} ${e.subject || ''}`.toLowerCase().includes(k))
})

const decorate = list => (list || []).map(e => ({ ...e, otp: extractOtp({ subject: e.subject }) }))

async function loadMailbox(cur = id.value) {
  try {
    const found = await api.getMailbox(cur)
    if (cur !== id.value) return
    mailbox.value = found && !isExpired(found) ? found : null
    missing.value = !mailbox.value
  } catch (e) {
    if (cur !== id.value) return
    if (e.status === 404 || e.status === 400) {
      mailbox.value = null
      missing.value = true
      return
    }
    throw e
  }
}

async function loadEmails({ silent = false, targetId = id.value } = {}) {
  const fresh = decorate(await api.listEmails(targetId))
  if (targetId !== id.value) return
  if (silent) {
    const known = new Set(emails.value.map(e => e.id))
    const added = fresh.filter(e => !known.has(e.id))
    if (added.length) {
      added.forEach(e => unseen.value.add(e.id))
      if (document.visibilityState === 'hidden' || emailId.value) newCount.value += added.length
      const first = added[0]
      toast.info(added.length === 1 ? `新邮件：${first.subject || senderName(first.sender)}` : `收到 ${added.length} 封新邮件`)
    }
  }
  emails.value = fresh
}

async function loadAll() {
  const cur = id.value
  loading.value = true
  missing.value = false
  mailbox.value = null
  emails.value = []
  unseen.value = new Set()
  try {
    await loadMailbox(cur)
    if (cur !== id.value || missing.value) return
    await loadEmails({ targetId: cur })
  } catch (e) {
    if (cur !== id.value) return
    if (e.status === 404 || e.status === 400) missing.value = true
    else toast.error(`加载失败：${e.message}`)
  } finally {
    if (cur === id.value) loading.value = false
  }
}

// 实时推送：收到新邮件事件后拉一次列表（带 OTP 解析等），连接不可用时退回 8 秒轮询
const live = useMailboxLive(id, {
  onEmail: () => loadEmails({ silent: true }).catch(() => {}),
  onMailbox: mb => { if (mailbox.value && mb?.id === mailbox.value.id) mailbox.value = { ...mailbox.value, ...mb } },
  onExpired: () => { forgetExpiry(id.value); mailbox.value = null; missing.value = true },
  onMissing: () => { forgetExpiry(id.value); mailbox.value = null; missing.value = true },
})
const isLive = computed(() => live.status.value === 'live')
let liveTicks = 0
usePoller(() => {
  if (missing.value) return null
  // SSE 断开时每 8 秒轮询；SSE 在线时每 8 轮（约 64 秒）对账一次，防实时推送队列满丢事件
  if (isLive.value) {
    if (++liveTicks < 8) return null
    liveTicks = 0
  } else {
    liveTicks = 0
  }
  return loadEmails({ silent: true })
}, 8000)

const extending = ref(false)
async function extend() {
  if (!mailbox.value || extending.value) return
  extending.value = true
  const fresh = await extendMailbox(mailbox.value)
  if (fresh) mailbox.value = { ...mailbox.value, ...fresh }
  extending.value = false
}
// 其他页面（提醒提示）续期了当前邮箱
watch(() => mailboxBus.updated, u => {
  if (u && mailbox.value && u.mailbox.id === mailbox.value.id) mailbox.value = { ...mailbox.value, ...u.mailbox }
})

const refreshing = ref(false)
async function refresh() {
  if (refreshing.value || missing.value) return
  refreshing.value = true
  try {
    await loadEmails({ silent: false })
  } catch (e) {
    toast.error(`刷新失败：${e.message}`)
  } finally {
    refreshing.value = false
  }
}

function openEmail(e) {
  unseen.value.delete(e.id)
  router.push({ name: 'mailbox', params: { id: id.value, emailId: e.id } })
}

function back() {
  router.push({ name: 'mailbox', params: { id: id.value } })
}

function onDeleted(eid) {
  emails.value = emails.value.filter(e => e.id !== eid)
  back()
}

const deletingEmails = ref(new Set())
const deletingMailbox = ref(false)

async function removeEmail(e) {
  if (deletingEmails.value.has(e.id)) return
  deletingEmails.value = new Set(deletingEmails.value).add(e.id)
  try {
    await api.deleteEmail(id.value, e.id)
    emails.value = emails.value.filter(x => x.id !== e.id)
    toast.success('邮件已删除')
    if (emailId.value === e.id) back()
  } catch (err) {
    toast.error(`删除失败：${err.message}`)
  } finally {
    const next = new Set(deletingEmails.value)
    next.delete(e.id)
    deletingEmails.value = next
  }
}

async function removeMailbox() {
  if (!mailbox.value || deletingMailbox.value) return
  const ok = await confirm({
    title: '删除邮箱',
    message: `确定删除 ${mailbox.value.full_address}？`,
    detail: '邮箱内的所有邮件将被永久删除，且无法恢复。',
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  deletingMailbox.value = true
  try {
    await api.deleteMailbox(id.value)
    forgetExpiry(id.value)
    toast.success('邮箱已删除')
    mailboxesChanged()
    router.replace({ name: 'mailboxes' })
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
  } finally {
    deletingMailbox.value = false
  }
}

// 标签页标题显示未读数
function onVisible() {
  if (document.visibilityState === 'visible' && !emailId.value) newCount.value = 0
}
watch(newCount, n => {
  const t = document.title.replace(/^\(\d+\)\s*/, '')
  document.title = n > 0 ? `(${n}) ${t}` : t
})
watch(emailId, v => {
  if (v) unseen.value.delete(v)
  if (!v && document.visibilityState === 'visible') newCount.value = 0
})

// 停留在页面期间到期，同样按已删除处理
watch(now, t => {
  if (mailbox.value && isExpired(mailbox.value, t)) {
    forgetExpiry(id.value)
    mailbox.value = null
    missing.value = true
  } else if (mailbox.value) checkExpiry([mailbox.value], t)
})

const addrCopied = ref(false)
let addrCopyTimer = null
async function copyAddress() {
  if (!mailbox.value?.full_address) return
  if (await copyText(mailbox.value.full_address, '已复制邮箱地址')) {
    addrCopied.value = true
    clearTimeout(addrCopyTimer)
    addrCopyTimer = setTimeout(() => { addrCopied.value = false }, 1400)
  }
}

watch(id, loadAll, { immediate: true })
onMounted(() => document.addEventListener('visibilitychange', onVisible))
onBeforeUnmount(() => {
  clearTimeout(addrCopyTimer)
  document.removeEventListener('visibilitychange', onVisible)
  document.title = document.title.replace(/^\(\d+\)\s*/, '') || baseTitle
})
</script>

<template>
  <div class="inbox-page">
    <header class="page-h">
      <div class="titles">
        <RouterLink :to="{ name: 'mailboxes' }" class="crumb"><ArrowLeft aria-hidden="true" />我的邮箱</RouterLink>
        <h1 class="addr-title">
          <span v-if="loading && !mailbox" class="skel" style="width: 260px; height: 22px" />
          <button
            v-else-if="mailbox"
            type="button"
            class="addr-copy"
            :class="{ copied: addrCopied }"
            title="点击复制邮箱地址"
            aria-label="点击复制邮箱地址"
            @click="copyAddress"
          >
            <span class="mono ellipsis">{{ mailbox.full_address }}</span>
            <span class="addr-pill" aria-hidden="true">
              <component :is="addrCopied ? Check : Copy" />{{ addrCopied ? '已复制' : '复制' }}
            </span>
          </button>
          <span v-else class="mono ellipsis">邮箱不存在</span>
        </h1>
        <p v-if="mailbox" class="sub-line">
          <TtlCell :mailbox="mailbox" :now="now" compact />
          <span class="muted">· {{ emails.length }} 封邮件 ·</span>
          <span class="live-pill" :class="{ on: isLive }" :title="isLive ? '新邮件会实时推送到这里' : '实时连接不可用，每 8 秒自动刷新'">
            <Radio aria-hidden="true" />{{ isLive ? '实时' : '每 8 秒刷新' }}
          </span>
        </p>
      </div>
      <div v-if="mailbox" class="acts">
        <button type="button" class="btn btn-secondary" :disabled="refreshing" @click="refresh">
          <RefreshCw :class="{ spin: refreshing }" aria-hidden="true" />刷新
        </button>
        <button v-if="mailbox.expires_at" type="button" class="btn btn-secondary" :disabled="extending" @click="extend">
          <TimerReset :class="{ spin: extending }" aria-hidden="true" />续期
        </button>
        <button type="button" class="btn btn-danger" :disabled="deletingMailbox" @click="removeMailbox"><Trash2 aria-hidden="true" />{{ deletingMailbox ? '删除中…' : '删除邮箱' }}</button>
      </div>
    </header>

    <section v-if="missing && !loading" class="card">
      <EmptyState :icon="Mail" title="邮箱不存在">
        这个邮箱可能已过期被自动删除，或不属于当前账户。
        <template #actions>
          <RouterLink :to="{ name: 'mailboxes' }" class="btn btn-secondary btn-sm">返回邮箱列表</RouterLink>
        </template>
      </EmptyState>
    </section>

    <section v-else class="card split" :class="{ reading: !!emailId }">
      <div class="list-pane" aria-label="邮件列表">
        <div class="list-search">
          <Search aria-hidden="true" />
          <input v-model="q" class="inp" type="search" placeholder="筛选发件人或主题" aria-label="筛选邮件" />
        </div>
        <div v-if="loading" class="stack pad">
          <span v-for="i in 4" :key="i" class="skel" style="height: 40px" />
        </div>
        <EmptyState v-else-if="!emails.length" :icon="Inbox" title="收件箱是空的">
          向 <code>{{ mailbox?.full_address }}</code> 发送邮件，新邮件会自动出现在这里。
          <template #actions>
            <button v-if="mailbox" type="button" class="btn btn-secondary btn-sm" @click="copyText(mailbox.full_address, '已复制邮箱地址')">复制地址</button>
          </template>
        </EmptyState>
        <EmptyState v-else-if="!filtered.length" :icon="Search">没有匹配的邮件</EmptyState>
        <TransitionGroup v-else tag="ul" name="mail" class="mlist">
          <li
            v-for="e in filtered"
            :key="e.id"
            class="mi"
            :class="{ on: e.id === emailId, unread: unseen.has(e.id), 'has-otp': !!e.otp }"
          >
            <button type="button" class="mi-btn" :aria-current="e.id === emailId ? 'true' : undefined" @click="openEmail(e)">
              <span class="av" aria-hidden="true">{{ initial(e.sender) }}</span>
              <span class="mi-t">
                <span class="mi-f">
                  <span class="ellipsis">{{ senderName(e.sender) }}</span>
                  <time :datetime="e.received_at">{{ timeAgo(e.received_at, now) }}</time>
                </span>
                <span class="mi-s ellipsis">{{ e.subject || '(无主题)' }}</span>
              </span>
            </button>
            <div class="mi-acts">
              <button
                v-if="e.otp"
                type="button"
                class="otp-chip"
                :aria-label="`复制验证码 ${e.otp}`"
                @click="copyText(e.otp, `已复制验证码 ${e.otp}`)"
              >
                {{ e.otp }}
              </button>
              <button type="button" class="icon-btn icon-btn-sm danger" aria-label="删除邮件" title="删除邮件" :disabled="deletingEmails.has(e.id)" @click="removeEmail(e)">
                <Trash2 aria-hidden="true" />
              </button>
            </div>
          </li>
        </TransitionGroup>
      </div>

      <div class="read-pane">
        <EmailReader
          v-if="emailId"
          :mailbox-id="id"
          :email-id="emailId"
          :address="mailbox?.full_address"
          @back="back"
          @deleted="onDeleted"
        />
        <EmptyState v-else :icon="MailOpen" title="选择一封邮件">从左侧列表中选择邮件查看内容</EmptyState>
      </div>
    </section>
  </div>
</template>

<style scoped>
.live-pill {
  display: inline-flex; align-items: center; gap: 4px; font-size: 12px; color: var(--text-3);
}
.live-pill svg { width: 13px; height: 13px; }
.live-pill.on { color: var(--ok); }
.live-pill.on svg { animation: live-pulse 2s ease-in-out infinite; }
@keyframes live-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }

.inbox-page { display: grid; gap: 16px; }
.addr-title { min-width: 0; }
.addr-title .mono { font-family: var(--mono); font-size: 18px; font-weight: 600; }
.addr-copy {
  display: inline-flex; align-items: center; gap: 8px; max-width: 100%; min-width: 0;
  padding: 3px 8px 3px 6px; margin-left: -6px; border: 1px solid transparent;
  border-radius: var(--r-sm); background: none; text-align: left;
  transition: background var(--ease), border-color var(--ease);
}
.addr-copy:hover { background: var(--tint); border-color: color-mix(in oklab, var(--fill-edge) 45%, var(--border)); }
.addr-pill {
  display: inline-flex; align-items: center; gap: 4px; flex: none;
  padding: 2px 7px; border-radius: 999px; font-size: 11px; font-weight: 500;
  background: var(--bg); color: var(--text-2); border: 1px solid var(--border);
  transition: background var(--ease), color var(--ease), border-color var(--ease);
}
.addr-pill svg { width: 12px; height: 12px; }
.addr-copy:hover .addr-pill { background: var(--surface); color: var(--accent); border-color: var(--fill-edge); }
.addr-copy.copied .addr-pill { background: var(--ok-bg); color: var(--ok); border-color: color-mix(in oklab, var(--ok) 35%, var(--border)); }
.sub-line { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }

.split { display: grid; grid-template-columns: minmax(280px, 360px) 1fr; min-height: calc(100vh - 200px); overflow: hidden; }
.list-pane { border-right: 1px solid var(--border); display: flex; flex-direction: column; min-width: 0; }
.read-pane { min-width: 0; overflow: auto; max-height: calc(100vh - 200px); }
.read-pane > .empty { height: 100%; align-content: center; }
.list-search { position: relative; padding: 10px; border-bottom: 1px solid var(--border); }
.list-search svg { position: absolute; left: 20px; top: 50%; transform: translateY(-50%); width: 15px; height: 15px; color: var(--text-3); pointer-events: none; }
.list-search .inp { padding-left: 32px; height: 32px; }
.pad { padding: 14px; }

.mlist { list-style: none; padding: 0; overflow-y: auto; flex: 1; max-height: calc(100vh - 252px); }
.mi { position: relative; border-bottom: 1px solid var(--border); transition: background var(--ease); }
.mi:hover { background: var(--tint); }
.mi.on { background: var(--tint); box-shadow: inset 3px 0 0 var(--accent); }
.mi-btn {
  width: 100%; min-width: 0; display: flex; gap: 10px; align-items: flex-start; text-align: left;
  padding: 11px 12px 11px 14px; border: 0; background: none;
}
.av {
  width: 32px; height: 32px; border-radius: 50%; flex: none; display: grid; place-items: center;
  margin-top: 2px; font-weight: 600; font-size: 13px; background: var(--tint-strong); color: var(--on-fill);
}
[data-theme="dark"] .av { color: var(--text); }
.mi-t { display: grid; gap: 3px; min-width: 0; flex: 1; }
.mi-f { display: flex; align-items: baseline; gap: 8px; font-weight: 600; font-size: 13px; min-width: 0; }
.mi-f time { margin-left: auto; font-weight: 400; color: var(--text-3); font-size: 12px; flex: none; }
.mi.unread .mi-f > span::before {
  content: ""; display: inline-block; width: 7px; height: 7px; border-radius: 50%;
  background: var(--accent); margin-right: 6px; vertical-align: 1px;
}
.mi-s { font-size: 13px; color: var(--text-2); line-height: 20px; }
.mi.has-otp .mi-s { padding-right: 72px; }
.mi:hover .mi-s, .mi:focus-within .mi-s { padding-right: 30px; }
.mi.has-otp:hover .mi-s, .mi.has-otp:focus-within .mi-s { padding-right: 98px; }
.mi-acts {
  position: absolute; right: 10px; bottom: 9px;
  display: flex; align-items: center; gap: 4px; pointer-events: none;
}
.mi-acts > * { pointer-events: auto; }
.mi-acts .icon-btn { opacity: 0; width: 0; overflow: hidden; transition: opacity var(--ease), width var(--ease), background var(--ease), color var(--ease); }
.mi:hover .mi-acts .icon-btn, .mi:focus-within .mi-acts .icon-btn { opacity: 1; width: 24px; }

@media (hover: none) {
  .mi-acts .icon-btn { opacity: 1; width: 24px; }
  .mi .mi-s { padding-right: 30px; }
  .mi.has-otp .mi-s { padding-right: 98px; }
}
@media (max-width: 900px) {
  .split { grid-template-columns: minmax(0, 1fr); min-height: 0; }
  .list-pane { border-right: 0; }
  .split.reading .list-pane { display: none; }
  .split:not(.reading) .read-pane { display: none; }
  .read-pane, .mlist { max-height: none; }
}
</style>
