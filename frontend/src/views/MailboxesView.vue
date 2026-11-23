<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Search, Inbox, Trash2, Mail, RefreshCw, ArrowRight, TimerReset } from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import SkeletonRows from '../components/ui/SkeletonRows.vue'
import CopyButton from '../components/ui/CopyButton.vue'
import TtlCell from '../components/mail/TtlCell.vue'
import { api } from '../api/client'
import { confirm, toast } from '../composables/feedback'
import { extendMailbox, mailboxBus, mailboxesChanged, openCreateMailbox, patchMailbox } from '../composables/mailboxBus'
import { checkExpiry } from '../composables/expiryReminder'
import { useNow } from '../composables/poller'
import { expiryInfo, formatDate, isExpired } from '../utils/format'

const router = useRouter()
const now = useNow(15000)

const loading = ref(true)
const allItems = ref([])
const items = computed(() => allItems.value.filter(m => !isExpired(m, now.value)))
const q = ref('')
const sort = ref('expiry')

const filtered = computed(() => {
  const k = q.value.trim().toLowerCase()
  const list = k ? items.value.filter(m => m.full_address.toLowerCase().includes(k)) : [...items.value]
  if (sort.value === 'expiry') list.sort((a, b) => expiryInfo(a, now.value).mins - expiryInfo(b, now.value).mins)
  else list.sort((a, b) => new Date(b.created_at) - new Date(a.created_at))
  return list
})

async function load() {
  loading.value = true
  try {
    allItems.value = (await api.listMailboxes()) || []
    checkExpiry(allItems.value)
  } catch (e) {
    toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

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
    allItems.value = allItems.value.filter(m => m.id !== mb.id)
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

watch(() => mailboxBus.version, load)
onMounted(load)
</script>

<template>
  <PageHeader title="我的邮箱" :sub="loading ? '加载中…' : `共 ${items.length} 个邮箱，到期后自动删除`">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
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

    <SkeletonRows v-if="loading" :rows="4" :cols="4" />
    <EmptyState v-else-if="!items.length" :icon="Mail" title="还没有邮箱">
      创建一个临时邮箱，用它接收验证码、注册邮件等。
      <template #actions>
        <button type="button" class="btn btn-primary btn-sm" @click="openCreateMailbox"><Plus aria-hidden="true" />新建邮箱</button>
      </template>
    </EmptyState>
    <EmptyState v-else-if="!filtered.length" :icon="Search">没有匹配“{{ q }}”的邮箱</EmptyState>
    <div v-else class="table-wrap">
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
          <tr v-for="mb in filtered" :key="mb.id" class="clickable" @click="open(mb)">
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
.tip a { display: inline-flex; align-items: center; gap: 2px; }
.tip .i { width: 13px; height: 13px; }
@media (max-width: 640px) {
  .hide-sm { display: none; }
  .search { max-width: none; }
}
</style>
