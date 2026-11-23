<script setup>
import { computed, onMounted, ref } from 'vue'
import { Plus, Zap, RefreshCw, Server, Clock, CircleCheck, CirclePause, Trash2, Power, PowerOff } from '@lucide/vue'
import PageHeader from '../../components/ui/PageHeader.vue'
import EmptyState from '../../components/ui/EmptyState.vue'
import SkeletonRows from '../../components/ui/SkeletonRows.vue'
import MxDetails from '../../components/domain/MxDetails.vue'
import MxRegisterModal from '../../components/domain/MxRegisterModal.vue'
import AddDomainModal from '../../components/domain/AddDomainModal.vue'
import { api } from '../../api/client'
import { confirm, toast } from '../../composables/feedback'
import { usePoller } from '../../composables/poller'
import { formatDate, timeAgo } from '../../utils/format'

const loading = ref(true)
const refreshing = ref(false)
const domains = ref([])
const live = ref({})
const busyId = ref(null)
const addOpen = ref(false)
const mxOpen = ref(false)

const pending = computed(() => domains.value.filter(d => d.status === 'pending'))
const pool = computed(() => domains.value.filter(d => d.status !== 'pending'))
const activeCount = computed(() => pool.value.filter(d => d.is_active).length)

async function load() {
  loading.value = true
  try {
    domains.value = (await api.domains()) || []
  } catch (e) {
    toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

usePoller(async () => {
  if (!pending.value.length) return
  const res = await Promise.all(pending.value.map(d => api.getDomainStatus(d.id).catch(() => null)))
  let changed = false
  res.forEach(r => {
    if (!r) return
    live.value = { ...live.value, [r.id]: r }
    if (r.status === 'active') {
      changed = true
      toast.success(`域名 ${r.domain} 已通过 MX 验证`)
    }
  })
  if (changed) await load()
}, 5000)

async function refreshMx() {
  refreshing.value = true
  try {
    await api.admin.refreshDomainMX()
    toast.success('已重新检测全部域名的 MX 能力')
    await load()
  } catch (e) {
    toast.error(`刷新失败：${e.message}`)
  } finally {
    refreshing.value = false
  }
}

async function toggle(d) {
  busyId.value = d.id
  try {
    await api.admin.toggleDomain(d.id, !d.is_active)
    toast.success(d.is_active ? `已停用 ${d.domain}` : `已启用 ${d.domain}`)
    await load()
  } catch (e) {
    toast.error(`操作失败：${e.message}`)
  } finally {
    busyId.value = null
  }
}

async function remove(d) {
  const ok = await confirm({
    title: '删除域名',
    message: `确定删除 ${d.domain}？`,
    detail: '如果该域名下还有邮箱，系统会拒绝删除。可以先停用域名，避免继续生成新邮箱。',
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.admin.deleteDomain(d.id)
    toast.success('域名已删除')
    await load()
  } catch (e) {
    toast.error(e.message)
  }
}

onMounted(load)
</script>

<template>
  <PageHeader title="域名池" :sub="loading ? '加载中…' : `${activeCount} 个启用 · ${pool.length - activeCount} 个停用 · ${pending.length} 个待验证`">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="refreshing" @click="refreshMx">
        <RefreshCw :class="{ spin: refreshing }" aria-hidden="true" />{{ refreshing ? '检测中…' : '刷新 MX' }}
      </button>
      <button type="button" class="btn btn-secondary" @click="mxOpen = true"><Zap aria-hidden="true" />MX 自动注册</button>
      <button type="button" class="btn btn-primary" @click="addOpen = true"><Plus aria-hidden="true" />手动添加</button>
    </template>
  </PageHeader>

  <section v-if="pending.length" class="card" aria-labelledby="h-pending">
    <div class="card-h">
      <h2 id="h-pending"><Clock aria-hidden="true" />待 MX 验证 · {{ pending.length }}</h2>
      <span class="sub">后台每 30 秒自动检测，本页每 5 秒刷新状态</span>
    </div>
    <div class="table-wrap">
      <table class="tbl">
        <thead><tr><th>域名</th><th>上次检测</th><th>检测结果</th><th class="act"><span class="sr-only">操作</span></th></tr></thead>
        <tbody>
          <tr v-for="d in pending" :key="d.id">
            <td><code>{{ d.domain }}</code></td>
            <td class="muted nowrap">
              {{ (live[d.id]?.mx_checked_at || d.mx_checked_at) ? timeAgo(live[d.id]?.mx_checked_at || d.mx_checked_at) : '从未' }}
            </td>
            <td>
              <span v-if="!live[d.id]" class="badge b-warn"><Clock aria-hidden="true" />检测中</span>
              <MxDetails v-else :details="live[d.id].mx_details" />
            </td>
            <td class="act">
              <button type="button" class="icon-btn icon-btn-sm danger" :aria-label="`删除 ${d.domain}`" title="删除" @click="remove(d)">
                <Trash2 aria-hidden="true" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <section class="card" aria-labelledby="h-list">
    <div class="card-h">
      <h2 id="h-list"><Server aria-hidden="true" />域名列表</h2>
      <span class="sub">后台会定期刷新单域名与通配子域能力</span>
    </div>
    <SkeletonRows v-if="loading && !domains.length" :rows="4" :cols="5" />
    <EmptyState v-else-if="!pool.length" :icon="Server" title="域名池为空">
      添加第一个域名后，用户就可以用它创建邮箱。
      <template #actions>
        <button type="button" class="btn btn-primary btn-sm" @click="addOpen = true"><Plus aria-hidden="true" />手动添加</button>
      </template>
    </EmptyState>
    <div v-else class="table-wrap">
      <table class="tbl">
        <thead>
          <tr><th>域名</th><th>单域名</th><th>多级子域</th><th>状态</th><th>添加时间</th><th class="act"><span class="sr-only">操作</span></th></tr>
        </thead>
        <tbody>
          <tr v-for="d in pool" :key="d.id">
            <td><code>{{ d.base_domain || d.domain }}</code></td>
            <td>
              <span v-if="d.supports_single" class="badge b-ok"><CircleCheck aria-hidden="true" />支持</span>
              <span v-else class="badge b-muted">未通过</span>
            </td>
            <td>
              <span v-if="d.supports_wildcard" class="badge b-ok"><CircleCheck aria-hidden="true" />支持</span>
              <span v-else class="badge b-muted">未通过</span>
            </td>
            <td>
              <span v-if="d.is_active" class="badge b-ok"><CircleCheck aria-hidden="true" />启用</span>
              <span v-else class="badge b-muted"><CirclePause aria-hidden="true" />停用</span>
            </td>
            <td class="muted nowrap">{{ formatDate(d.created_at) }}</td>
            <td class="act">
              <div class="acts">
                <button type="button" class="btn btn-ghost btn-sm" :disabled="busyId === d.id" @click="toggle(d)">
                  <component :is="d.is_active ? PowerOff : Power" aria-hidden="true" />{{ d.is_active ? '停用' : '启用' }}
                </button>
                <button type="button" class="icon-btn icon-btn-sm danger" :aria-label="`删除 ${d.domain}`" title="删除" @click="remove(d)">
                  <Trash2 aria-hidden="true" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <AddDomainModal :open="addOpen" @close="addOpen = false" @changed="load" />
  <MxRegisterModal :open="mxOpen" @close="mxOpen = false" @changed="load" />
</template>

<style scoped>
.acts { display: inline-flex; gap: 4px; align-items: center; }
</style>
