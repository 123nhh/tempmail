<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  Plus, Inbox, Globe, Mail, Users, Hourglass, ArrowRight, Clock, CircleCheck, CirclePause, RefreshCw,
  MailOpen, KeyRound,
} from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import SkeletonRows from '../components/ui/SkeletonRows.vue'
import CopyButton from '../components/ui/CopyButton.vue'
import TtlCell from '../components/mail/TtlCell.vue'
import { api } from '../api/client'
import { useSession } from '../stores/session'
import { mailboxBus, openCreateMailbox } from '../composables/mailboxBus'
import { useNow } from '../composables/poller'
import { copyText, toast } from '../composables/feedback'
import { expiryInfo, isExpired, num, senderName, timeAgo } from '../utils/format'
import { extractOtp } from '../utils/otp'

const session = useSession()
const router = useRouter()
const now = useNow(30000)

const loading = ref(true)
const allMailboxes = ref([])
const mailboxes = computed(() => allMailboxes.value.filter(m => !isExpired(m, now.value)))
const domains = ref([])
const stats = ref(null)
const recent = ref([])
const recentLoading = ref(false)

// ─── 派生指标 ───
const sortedBoxes = computed(() =>
  [...mailboxes.value].sort((a, b) => expiryInfo(a, now.value).mins - expiryInfo(b, now.value).mins),
)
const expiringSoon = computed(() => sortedBoxes.value.filter(m => expiryInfo(m, now.value).mins <= 60))
const activeDomains = computed(() => domains.value.filter(d => d.is_active && d.status !== 'pending'))
const pendingDomains = computed(() => domains.value.filter(d => d.status === 'pending'))
const disabledDomains = computed(() => domains.value.filter(d => !d.is_active && d.status !== 'pending'))
const wildcardCount = computed(() => activeDomains.value.filter(d => d.supports_wildcard).length)
const singleCount = computed(() => activeDomains.value.filter(d => d.supports_single).length)

const kpis = computed(() => {
  const st = stats.value || {}
  const list = [
    {
      label: '我的邮箱',
      value: num(mailboxes.value.length),
      note: expiringSoon.value.length ? `${expiringSoon.value.length} 个 1 小时内过期` : '当前有效',
      noteCls: expiringSoon.value.length ? 't-warn' : '',
      icon: Inbox,
    },
    { label: '可用域名', value: num(activeDomains.value.length), note: `${wildcardCount.value} 个支持多级子域`, icon: Globe },
    { label: '平台累计邮件', value: num(st.total_emails), note: '全平台收件总数', icon: Mail },
    {
      label: '平台活跃邮箱',
      value: num(st.active_mailboxes),
      note: '全平台未过期的邮箱',
      icon: Hourglass,
    },
  ]
  if (session.isAdmin) {
    list.push(
      { label: '账户总数', value: num(st.total_accounts), note: '已注册用户', icon: Users, to: { name: 'admin-accounts' } },
      {
        label: '待验证域名',
        value: num(st.pending_domains ?? pendingDomains.value.length),
        note: (st.pending_domains ?? pendingDomains.value.length) > 0 ? 'MX 验证中' : '无待处理',
        noteCls: (st.pending_domains ?? pendingDomains.value.length) > 0 ? 't-warn' : '',
        icon: RefreshCw,
        to: { name: 'admin-domains' },
      },
    )
  }
  return list
})

// 域名池构成：状态色 + 图标 + 文字标签，颜色不单独承载含义
const pool = computed(() => {
  const total = domains.value.length
  const segs = [
    { key: 'ok', label: '启用', n: activeDomains.value.length, icon: CircleCheck },
    { key: 'warn', label: '待验证', n: pendingDomains.value.length, icon: Clock },
    { key: 'muted', label: '停用', n: disabledDomains.value.length, icon: CirclePause },
  ]
  return { total, segs: segs.map(s => ({ ...s, pct: total ? (s.n / total) * 100 : 0 })) }
})

// ─── 数据加载 ───
// 邮箱、域名、统计、最近邮件并行请求，一个往返完成看板加载
async function load() {
  loading.value = true
  try {
    const [mb, dm, st] = await Promise.all([
      api.listMailboxes(),
      api.domains(),
      api.stats().catch(() => null),
      loadRecent(),
    ])
    allMailboxes.value = mb || []
    domains.value = dm || []
    stats.value = st
  } catch (e) {
    toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

// 最近邮件：后端一次性返回当前账户全部活跃邮箱的最新邮件（按收件时间倒序）
const recentMailboxId = e => e.mailbox_id ?? e.mailbox?.id
const recentAddress = e => e.mailbox_address || e.mailbox?.full_address || ''
async function loadRecent() {
  recentLoading.value = true
  try {
    const list = await api.recentEmails(8)
    recent.value = (list || [])
      .filter(e => recentMailboxId(e) != null)
      .map(e => ({ ...e, otp: extractOtp({ subject: e.subject }) }))
  } catch {
    recent.value = []
  } finally {
    recentLoading.value = false
  }
}

function openBox(mb) {
  router.push({ name: 'mailbox', params: { id: mb.id } })
}
function openEmail(e) {
  router.push({ name: 'mailbox', params: { id: recentMailboxId(e), emailId: e.id } })
}

watch(() => mailboxBus.version, load)
onMounted(load)
</script>

<template>
  <PageHeader title="数据看板" :sub="`欢迎回来，${session.username}`">
    <template #actions>
      <RouterLink :to="{ name: 'apikey' }" class="btn btn-secondary"><KeyRound aria-hidden="true" />我的 API Key</RouterLink>
      <button type="button" class="btn btn-primary" @click="openCreateMailbox"><Plus aria-hidden="true" />新建邮箱</button>
    </template>
  </PageHeader>

  <!-- KPI -->
  <section class="kpis" aria-label="关键指标">
    <component
      :is="k.to ? 'RouterLink' : 'div'"
      v-for="k in kpis"
      :key="k.label"
      :to="k.to"
      class="kpi card"
      :class="{ link: k.to }"
    >
      <span class="kpi-l"><component :is="k.icon" aria-hidden="true" />{{ k.label }}</span>
      <span v-if="loading" class="skel" style="width: 56px; height: 26px" />
      <strong v-else class="kpi-v">{{ k.value }}</strong>
      <span class="kpi-s" :class="k.noteCls">{{ loading ? ' ' : k.note }}</span>
    </component>
  </section>

  <div class="grid">
    <!-- 我的邮箱 -->
    <section class="card span-2" aria-labelledby="h-boxes">
      <div class="card-h">
        <h2 id="h-boxes"><Inbox aria-hidden="true" />我的邮箱</h2>
        <RouterLink :to="{ name: 'mailboxes' }" class="btn btn-ghost btn-sm">全部<ArrowRight aria-hidden="true" /></RouterLink>
      </div>
      <SkeletonRows v-if="loading" :rows="3" :cols="3" />
      <EmptyState v-else-if="!mailboxes.length" :icon="Mail" title="还没有邮箱">
        创建一个临时邮箱，收到的邮件会实时显示在这里。
        <template #actions>
          <button type="button" class="btn btn-primary btn-sm" @click="openCreateMailbox"><Plus aria-hidden="true" />新建邮箱</button>
        </template>
      </EmptyState>
      <div v-else class="table-wrap">
        <table class="tbl">
          <thead>
            <tr><th>地址</th><th>剩余时间</th><th class="act"><span class="sr-only">操作</span></th></tr>
          </thead>
          <tbody>
            <tr v-for="mb in sortedBoxes.slice(0, 6)" :key="mb.id" class="clickable" @click="openBox(mb)">
              <td>
                <RouterLink :to="{ name: 'mailbox', params: { id: mb.id } }" class="addr" @click.stop>{{ mb.full_address }}</RouterLink>
              </td>
              <td><TtlCell :mailbox="mb" :now="now" /></td>
              <td class="act"><CopyButton :text="mb.full_address" label="复制地址" small /></td>
            </tr>
          </tbody>
        </table>
        <p v-if="mailboxes.length > 6" class="more small muted">
          还有 {{ mailboxes.length - 6 }} 个邮箱，<RouterLink :to="{ name: 'mailboxes' }">查看全部</RouterLink>
        </p>
      </div>
    </section>

    <!-- 即将过期 -->
    <section class="card" aria-labelledby="h-exp">
      <div class="card-h">
        <h2 id="h-exp"><Hourglass aria-hidden="true" />即将过期</h2>
        <span class="sub">1 小时内</span>
      </div>
      <SkeletonRows v-if="loading" :rows="3" :cols="2" />
      <EmptyState v-else-if="!expiringSoon.length" :icon="CircleCheck">没有即将过期的邮箱</EmptyState>
      <ul v-else class="list">
        <li v-for="mb in expiringSoon.slice(0, 5)" :key="mb.id">
          <RouterLink :to="{ name: 'mailbox', params: { id: mb.id } }" class="addr ellipsis">{{ mb.full_address }}</RouterLink>
          <TtlCell :mailbox="mb" :now="now" compact />
        </li>
      </ul>
    </section>

    <!-- 最近邮件 -->
    <section class="card span-2" aria-labelledby="h-recent">
      <div class="card-h">
        <h2 id="h-recent"><MailOpen aria-hidden="true" />最近邮件</h2>
        <span class="sub">来自全部活跃邮箱</span>
      </div>
      <SkeletonRows v-if="loading || (recentLoading && !recent.length)" :rows="3" :cols="3" />
      <EmptyState v-else-if="!recent.length" :icon="MailOpen">
        {{ mailboxes.length ? '还没有收到邮件，向你的邮箱地址发一封试试。' : '创建邮箱后，新邮件会出现在这里。' }}
      </EmptyState>
      <ul v-else class="list mails">
        <li v-for="e in recent" :key="e.id" class="clickable" @click="openEmail(e)">
          <div class="m-main">
            <span class="m-from ellipsis">{{ senderName(e.sender) }}</span>
            <RouterLink
              :to="{ name: 'mailbox', params: { id: recentMailboxId(e), emailId: e.id } }"
              class="m-subj ellipsis"
              @click.stop
            >
              {{ e.subject || '(无主题)' }}
            </RouterLink>
            <span class="m-to ellipsis small muted">{{ recentAddress(e) }}</span>
          </div>
          <div class="m-side">
            <button
              v-if="e.otp"
              type="button"
              class="otp-chip"
              :aria-label="`复制验证码 ${e.otp}`"
              @click.stop="copyText(e.otp, `已复制验证码 ${e.otp}`)"
            >
              {{ e.otp }}
            </button>
            <time class="small muted" :datetime="e.received_at">{{ timeAgo(e.received_at, now) }}</time>
          </div>
        </li>
      </ul>
    </section>

    <!-- 域名池概况 -->
    <section class="card" aria-labelledby="h-pool">
      <div class="card-h">
        <h2 id="h-pool"><Globe aria-hidden="true" />域名池</h2>
        <RouterLink :to="{ name: session.isAdmin ? 'admin-domains' : 'domains' }" class="btn btn-ghost btn-sm">
          管理<ArrowRight aria-hidden="true" />
        </RouterLink>
      </div>
      <div class="card-b pool">
        <SkeletonRows v-if="loading" :rows="2" :cols="2" />
        <template v-else>
          <div class="pool-total"><strong class="tabular">{{ pool.total }}</strong><span class="muted small">个域名</span></div>
          <div v-if="pool.total" class="stack-bar" role="img" :aria-label="pool.segs.map(s => `${s.label} ${s.n}`).join('，')">
            <i v-for="s in pool.segs.filter(x => x.n)" :key="s.key" :class="s.key" :style="{ width: `${s.pct}%` }" :title="`${s.label} ${s.n}`" />
          </div>
          <ul class="legend">
            <li v-for="s in pool.segs" :key="s.key">
              <span class="sw" :class="s.key" aria-hidden="true" />
              <component :is="s.icon" aria-hidden="true" class="li" />
              <span>{{ s.label }}</span>
              <strong class="tabular">{{ s.n }}</strong>
            </li>
          </ul>
          <dl class="dl caps">
            <dt>支持单域名</dt><dd class="tabular">{{ singleCount }}</dd>
            <dt>支持多级子域</dt><dd class="tabular">{{ wildcardCount }}</dd>
          </dl>
        </template>
      </div>
    </section>
  </div>
</template>

<style scoped>
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 12px; }
.kpi { padding: 14px 16px; display: grid; gap: 4px; text-decoration: none; color: inherit; }
.kpi.link { transition: border-color var(--ease); }
.kpi.link:hover { border-color: var(--fill-edge); }
.kpi-l { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-3); }
.kpi-l svg { width: 14px; height: 14px; }
.kpi-v { font-size: 26px; font-weight: 650; line-height: 1.2; font-variant-numeric: tabular-nums; }
.kpi-s { font-size: 12px; color: var(--text-3); min-height: 18px; }

.grid { --card-h: 420px; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); grid-auto-rows: var(--card-h); gap: 16px; }
.span-2 { grid-column: span 2; }
/* 固定高度卡片：标题固定，内容区超出时在卡片内滚动 */
.grid > .card { display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
.grid > .card > .card-h { flex: none; }
.grid > .card > :not(.card-h) { flex: 1; min-height: 0; overflow-y: auto; }
.grid > .card > .empty { align-content: center; }
.grid .table-wrap thead th { position: sticky; top: 0; z-index: 1; background: var(--surface); }

.addr { font-family: var(--mono); font-size: 13px; color: var(--text); text-decoration: none; }
a.addr:hover { color: var(--accent); text-decoration: underline; text-underline-offset: 3px; }
.more { padding: 10px 14px; border-top: 1px solid var(--border); }

.list { list-style: none; padding: 0; }
.list li {
  display: flex; align-items: center; gap: 12px; justify-content: space-between;
  padding: 11px 18px; border-bottom: 1px solid var(--border); min-width: 0;
}
.list li:last-child { border-bottom: 0; }
.list .addr { min-width: 0; }
.list li.clickable { cursor: pointer; transition: background var(--ease); }
.list li.clickable:hover { background: var(--tint); }

.mails .m-main { display: grid; min-width: 0; gap: 1px; }
.mails .m-from { font-weight: 600; font-size: 13px; }
.mails .m-subj { color: var(--text-2); font-size: 13px; text-decoration: none; }
.mails .m-subj:hover { color: var(--accent); }
.mails .m-side { display: flex; align-items: center; gap: 10px; flex: none; }

.pool { display: grid; gap: 14px; }
.pool-total { display: flex; align-items: baseline; gap: 6px; }
.pool-total strong { font-size: 26px; font-weight: 650; }
.stack-bar { display: flex; gap: 2px; height: 10px; border-radius: 5px; overflow: hidden; background: var(--surface); }
.stack-bar i { display: block; height: 100%; min-width: 4px; }
.stack-bar i:first-child { border-radius: 4px 0 0 4px; }
.stack-bar i:last-child { border-radius: 0 4px 4px 0; }
.stack-bar i:only-child { border-radius: 4px; }
.ok { background: var(--ok); }
.warn { background: var(--warn); }
.muted.sw, .stack-bar i.muted, .sw.muted { background: var(--text-3); }
.legend { list-style: none; padding: 0; display: grid; gap: 6px; font-size: 13px; }
.legend li { display: flex; align-items: center; gap: 6px; }
.legend strong { margin-left: auto; font-weight: 600; }
.legend .sw { width: 10px; height: 10px; border-radius: 3px; flex: none; }
.legend .li { width: 14px; height: 14px; color: var(--text-3); }
.caps { padding-top: 12px; border-top: 1px solid var(--border); }
.caps dd { text-align: right; font-weight: 600; }

@media (max-width: 1100px) {
  .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .span-2 { grid-column: span 2; }
}
@media (max-width: 768px) {
  .grid { grid-template-columns: minmax(0, 1fr); }
  .span-2 { grid-column: auto; }
  .grid { --card-h: 380px; }
  .kpis { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .kpi-v { font-size: 22px; }
}
</style>
