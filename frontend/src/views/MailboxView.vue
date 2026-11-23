<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft, RefreshCw, Search, Trash2, MailOpen, Inbox, Mail, TimerReset, Radio } from '@lucide/vue'
import CopyButton from '../components/ui/CopyButton.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import TtlCell from '../components/mail/TtlCell.vue'
import EmailReader from '../components/mail/EmailReader.vue'
import { api } from '../api/client'
import { confirm, copyText, toast } from '../composables/feedback'
import { extendMailbox, mailboxBus, mailboxesChanged } from '../composables/mailboxBus'
import { useMailboxLive } from '../composables/live'
import { checkExpiry } from '../composables/expiryReminder'
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

async function loadMailbox() {
  const list = await api.listMailboxes()
  const found = (list || []).find(m => String(m.id) === id.value)
  mailbox.value = found && !isExpired(found) ? found : null
  missing.value = !mailbox.value
}

async function loadEmails({ silent = false } = {}) {
  const fresh = decorate(await api.listEmails(id.value))
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
  loading.value = true
  missing.value = false
  emails.value = []
  unseen.value = new Set()
  try {
    await Promise.all([loadMailbox(), loadEmails()])
  } catch (e) {
    if (e.status === 404 || e.status === 400) missing.value = true
    else toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

// 实时推送：收到新邮件事件后拉一次列表（带 OTP 解析等），连接不可用时退回 8 秒轮询
const live = useMailboxLive(id, {
  onEmail: () => loadEmails({ silent: true }).catch(() => {}),
  onMailbox: mb => { if (mailbox.value && mb?.id === mailbox.value.id) mailbox.value = { ...mailbox.value, ...mb } },
  onExpired: () => { mailbox.value = null; missing.value = true },
  onMissing: () => { mailbox.value = null; missing.value = true },
})
const isLive = computed(() => live.status.value === 'live')
const poller = usePoller(() => (missing.value || isLive.value ? null : loadEmails({ silent: true })), 8000)

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

async function refresh() {
  await poller.tick()
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

async function removeEmail(e) {
  try {
    await api.deleteEmail(id.value, e.id)
    emails.value = emails.value.filter(x => x.id !== e.id)
    toast.success('邮件已删除')
    if (emailId.value === e.id) back()
  } catch (err) {
    toast.error(`删除失败：${err.message}`)
  }
}

async function removeMailbox() {
  if (!mailbox.value) return
  const ok = await confirm({
    title: '删除邮箱',
    message: `确定删除 ${mailbox.value.full_address}？`,
    detail: '邮箱内的所有邮件将被永久删除，且无法恢复。',
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteMailbox(id.value)
    toast.success('邮箱已删除')
    mailboxesChanged()
    router.replace({ name: 'mailboxes' })
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
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
    mailbox.value = null
    missing.value = true
  } else if (mailbox.value) checkExpiry([mailbox.value], t)
})

watch(id, loadAll, { immediate: true })
onMounted(() => document.addEventListener('visibilitychange', onVisible))
onBeforeUnmount(() => {
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
          <template v-else>
            <span class="mono ellipsis">{{ mailbox?.full_address || '邮箱不存在' }}</span>
            <CopyButton v-if="mailbox" :text="mailbox.full_address" label="复制地址" />
          </template>
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
        <button type="button" class="btn btn-secondary" :disabled="poller.busy.value" @click="refresh">
          <RefreshCw :class="{ spin: poller.busy.value }" aria-hidden="true" />刷新
        </button>
        <button v-if="mailbox.expires_at" type="button" class="btn btn-secondary" :disabled="extending" @click="extend">
          <TimerReset :class="{ spin: extending }" aria-hidden="true" />续期
        </button>
        <button type="button" class="btn btn-danger" @click="removeMailbox"><Trash2 aria-hidden="true" />删除邮箱</button>
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
            :class="{ on: e.id === emailId, unread: unseen.has(e.id) }"
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
              <button type="button" class="icon-btn icon-btn-sm danger" aria-label="删除邮件" title="删除邮件" @click="removeEmail(e)">
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
.mi { position: relative; display: flex; align-items: center; border-bottom: 1px solid var(--border); transition: background var(--ease); }
.mi:hover { background: var(--tint); }
.mi.on { background: var(--tint); box-shadow: inset 3px 0 0 var(--accent); }
.mi-btn {
  flex: 1; min-width: 0; display: flex; gap: 10px; align-items: center; text-align: left;
  padding: 12px 8px 12px 14px; border: 0; background: none;
}
.av {
  width: 32px; height: 32px; border-radius: 50%; flex: none; display: grid; place-items: center;
  font-weight: 600; font-size: 13px; background: var(--tint-strong); color: var(--on-fill);
}
[data-theme="dark"] .av { color: var(--text); }
.mi-t { display: grid; gap: 1px; min-width: 0; flex: 1; }
.mi-f { display: flex; gap: 8px; font-weight: 600; font-size: 13px; min-width: 0; }
.mi-f time { margin-left: auto; font-weight: 400; color: var(--text-3); font-size: 12px; flex: none; }
.mi.unread .mi-f > span::before {
  content: ""; display: inline-block; width: 7px; height: 7px; border-radius: 50%;
  background: var(--accent); margin-right: 6px; vertical-align: 1px;
}
.mi-s { font-size: 13px; color: var(--text-2); }
.mi-acts { display: flex; align-items: center; gap: 4px; padding-right: 10px; flex: none; }
.mi-acts .icon-btn { opacity: 0; }
.mi:hover .mi-acts .icon-btn, .mi-acts .icon-btn:focus-visible { opacity: 1; }

@media (hover: none) { .mi-acts .icon-btn { opacity: 1; } }
@media (max-width: 900px) {
  .split { grid-template-columns: minmax(0, 1fr); min-height: 0; }
  .list-pane { border-right: 0; }
  .split.reading .list-pane { display: none; }
  .split:not(.reading) .read-pane { display: none; }
  .read-pane, .mlist { max-height: none; }
}
</style>
