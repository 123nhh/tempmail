<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search, Inbox, Trash2, Mail, RefreshCw, ArrowRight, TimerReset, ChevronLeft, ChevronRight } from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import SkeletonRows from '../components/ui/SkeletonRows.vue'
import CopyButton from '../components/ui/CopyButton.vue'
import TtlCell from '../components/mail/TtlCell.vue'
import { api } from '../api/client'
import { confirm, toast } from '../composables/feedback'
import { extendMailbox, mailboxBus, mailboxesChanged, openCreateMailbox, patchMailbox } from '../composables/mailboxBus'
import { checkExpiry, forgetExpiry } from '../composables/expiryReminder'
import { useNow } from '../composables/poller'
import { formatDate, isExpired, num } from '../utils/format'

const router = useRouter()
const now = useNow(15000)

// 服务端分页 + 搜索 + 排序
const SIZE = 20
const loading = ref(true)
const allItems = ref([])
const total = ref(0)
const page = ref(1)
const pages = computed(() => Math.max(1, Math.ceil(total.value / SIZE)))
// 停留在页面期间到期的邮箱本地先隐藏，下次加载时由后端剔除
const items = computed(() => allItems.value.filter(m => !isExpired(m, now.value)))
const q = ref('')
const sort = ref('expiry')
const keyword = computed(() => q.value.trim())
// 已加载结果对应的关键词，用于区分“没有邮箱”和“没有匹配结果”
const loadedQ = ref('')

let seq = 0
async function load(p = page.value) {
  const my = ++seq
  loading.value = true
  try {
    const kw = keyword.value
    const r = await api.listMailboxesPage(p, SIZE, { q: kw, sort: sort.value })
    if (my !== seq) return // 已被更新的请求取代（快速输入 / 翻页）
    const list = Array.isArray(r) ? r : (r?.data || [])
    const t = typeof r?.total === 'number' ? r.total : list.length
    // 当前页已越界（例如其他页面删除了邮箱）时回退到最后一页
    const last = Math.max(1, Math.ceil(t / SIZE))
    if (!list.length && p > last) {
      load(last)
      return
    }
    allItems.value = list
    total.value = t
    page.value = p
    loadedQ.value = kw
    checkExpiry(list)
  } catch (e) {
    if (my === seq) toast.error(`加载失败：${e.message}`)
  } finally {
    if (my === seq) loading.value = false
  }
}

// 搜索 250ms 防抖，排序切换立即生效；两者都回到第 1 页
let timer = 0
watch(keyword, () => {
  clearTimeout(timer)
  timer = setTimeout(() => load(1), 250)
})
watch(sort, () => {
  clearTimeout(timer)
  load(1)
})
onBeforeUnmount(() => clearTimeout(timer))

const subtitle = computed(() => {
  if (loading.value && !allItems.value.length) return '加载中…'
  if (loadedQ.value) return `找到 ${num(total.value)} 个匹配邮箱`
  return `共 ${num(total.value)} 个邮箱，到期后自动删除`
})

async function remove(mb) {
  const ok = await confirm({
    title: '删除邮箱',
    message: `确定删除 ${mb.full_address}？`,
    detail: '邮箱内的所有邮件将被永久删除，且无法恢复。',
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.deleteMailbox(mb.id)
    forgetExpiry(mb.id)
    // 删掉本页最后一行时回到上一页；mailboxesChanged 触发的重载会读取更新后的 page
    if (allItems.value.length <= 1 && page.value > 1) page.value--
    allItems.value = allItems.value.filter(m => m.id !== mb.id)
    if (total.value > 0) total.value--
    toast.success('邮箱已删除')
    mailboxesChanged()
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
  }
}

const renewing = ref(new Set())
async function renew(mb) {
  renewing.value = new Set(renewing.value).add(mb.id)
  await extendMailbox(mb)
  const next = new Set(renewing.value); next.delete(mb.id); renewing.value = next
}
// 续期结果（本页或其他页面的提醒按钮）原地更新
watch(() => mailboxBus.updated, u => { if (u) patchMailbox(allItems.value, u.mailbox) })
watch(now, t => checkExpiry(items.value, t))

function open(mb) {
  router.push({ name: 'mailbox', params: { id: mb.id } })
}

watch(() => mailboxBus.version, () => load())
onMounted(() => load(1))
</script>

<template>
  <PageHeader title="我的邮箱" :sub="subtitle">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="load()">
        <RefreshCw :class="{ spin: loading }" aria-hidden="true" />刷新
      </button>
      <button type="button" class="btn btn-primary" @click="openCreateMailbox"><Plus aria-hidden="true" />新建邮箱</button>
    </template>
  </PageHeader>

  <section class="card">
    <div class="card-h toolbar">
      <div class="search">
        <Search aria-hidden="true" />
        <input v-model="q" class="inp" type="search" placeholder="搜索邮箱地址" aria-label="搜索邮箱地址" />
      </div>
      <div class="seg" role="radiogroup" aria-label="排序">
        <button type="button" role="radio" :aria-checked="sort === 'expiry' ? 'true' : 'false'" @click="sort = 'expiry'">即将过期</button>
        <button type="button" role="radio" :aria-checked="sort === 'new' ? 'true' : 'false'" @click="sort = 'new'">最新创建</button>
      </div>
    </div>

    <SkeletonRows v-if="loading && !allItems.length" :rows="4" :cols="4" />
    <EmptyState v-else-if="!items.length && loadedQ" :icon="Search">没有匹配“{{ loadedQ }}”的邮箱</EmptyState>
    <EmptyState v-else-if="!items.length" :icon="Mail" title="还没有邮箱">
      创建一个临时邮箱，用它接收验证码、注册邮件等。
      <template #actions>
        <button type="button" class="btn btn-primary btn-sm" @click="openCreateMailbox"><Plus aria-hidden="true" />新建邮箱</button>
      </template>
    </EmptyState>
    <div v-else class="table-wrap" :class="{ busy: loading }" :aria-busy="loading ? 'true' : 'false'">
      <table class="tbl">
        <thead>
          <tr>
            <th>地址</th>
            <th>剩余时间</th>
            <th class="hide-sm">创建时间</th>
            <th class="act"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <TransitionGroup tag="tbody" name="row">
          <tr v-for="mb in items" :key="mb.id" class="clickable" @click="open(mb)">
            <td>
              <RouterLink :to="{ name: 'mailbox', params: { id: mb.id } }" class="addr" @click.stop>{{ mb.full_address }}</RouterLink>
            </td>
            <td><TtlCell :mailbox="mb" :now="now" /></td>
            <td class="hide-sm muted nowrap">{{ formatDate(mb.created_at) }}</td>
            <td class="act">
              <div class="acts">
                <button
                  v-if="mb.expires_at"
                  type="button"
                  class="icon-btn icon-btn-sm"
                  aria-label="续期"
                  title="续期"
                  :disabled="renewing.has(mb.id)"
                  @click.stop="renew(mb)"
                >
                  <TimerReset :class="{ spin: renewing.has(mb.id) }" aria-hidden="true" />
                </button>
                <CopyButton :text="mb.full_address" label="复制地址" small />
                <RouterLink
                  :to="{ name: 'mailbox', params: { id: mb.id } }"
                  class="icon-btn icon-btn-sm"
                  aria-label="打开收件箱"
                  title="打开收件箱"
                  @click.stop
                >
                  <Inbox aria-hidden="true" />
                </RouterLink>
                <button type="button" class="icon-btn icon-btn-sm danger" aria-label="删除邮箱" title="删除邮箱" @click.stop="remove(mb)">
                  <Trash2 aria-hidden="true" />
                </button>
              </div>
            </td>
          </tr>
        </TransitionGroup>
      </table>
    </div>
    <div v-if="total > SIZE" class="pager">
      <span class="small muted">第 {{ page }} / {{ pages }} 页</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="load(page - 1)">
        <ChevronLeft aria-hidden="true" />上一页
      </button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages || loading" @click="load(page + 1)">
        下一页<ChevronRight aria-hidden="true" />
      </button>
    </div>
  </section>

  <p class="small muted tip">
    需要用程序批量创建？查看 <RouterLink :to="{ name: 'docs', hash: '#mailbox-create' }">创建邮箱 API<ArrowRight class="i" aria-hidden="true" /></RouterLink>
  </p>
</template>

<style scoped>
.toolbar { gap: 10px; }
.toolbar > :last-child { margin-left: auto; }
.search { position: relative; flex: 1; min-width: 180px; max-width: 360px; }
.search svg { position: absolute; left: 10px; top: 50%; transform: translateY(-50%); width: 15px; height: 15px; color: var(--text-3); pointer-events: none; }
.search .inp { padding-left: 32px; height: 32px; }
.addr { font-family: var(--mono); font-size: 13px; color: var(--text); text-decoration: none; overflow-wrap: anywhere; }
.addr:hover { color: var(--accent); text-decoration: underline; text-underline-offset: 3px; }
.acts { display: inline-flex; gap: 2px; }
.table-wrap.busy { opacity: .6; transition: opacity var(--ease); }
.pager { display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding: 12px 14px; border-top: 1px solid var(--border); }
.pager .small { margin-right: auto; }
.tip a { display: inline-flex; align-items: center; gap: 2px; }
.tip .i { width: 13px; height: 13px; }
@media (max-width: 640px) {
  .hide-sm { display: none; }
  .search { max-width: none; }
}
</style>
