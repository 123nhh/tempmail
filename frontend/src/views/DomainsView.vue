<script setup>
import { computed, onMounted, ref } from 'vue'
import { Zap, Globe, Clock, CircleCheck, CirclePause, BookOpen, RefreshCw } from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import SkeletonRows from '../components/ui/SkeletonRows.vue'
import DnsTable from '../components/domain/DnsTable.vue'
import MxDetails from '../components/domain/MxDetails.vue'
import MxRegisterModal from '../components/domain/MxRegisterModal.vue'
import { api } from '../api/client'
import { toast } from '../composables/feedback'
import { usePoller } from '../composables/poller'
import { useSession } from '../stores/session'
import { dnsRecordsFor } from '../utils/dns'
import { timeAgo } from '../utils/format'

const session = useSession()
const loading = ref(true)
const domains = ref([])
const live = ref({}) // 待验证域名的实时状态
const modal = ref(false)

const pending = computed(() => domains.value.filter(d => d.status === 'pending'))
const pool = computed(() => domains.value.filter(d => d.status !== 'pending'))
const guide = computed(() => dnsRecordsFor('example.com', session.settings.smtp_server_ip, session.settings.smtp_hostname))

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

// 每 5 秒检查待验证域名，激活后移入域名池
usePoller(async () => {
  if (!pending.value.length) return
  const results = await Promise.all(pending.value.map(d => api.getDomainStatus(d.id).catch(() => null)))
  let changed = false
  results.forEach(r => {
    if (!r) return
    live.value = { ...live.value, [r.id]: r }
    if (r.status === 'active') {
      changed = true
      toast.success(`域名 ${r.domain} 已通过 MX 验证`)
    }
  })
  if (changed) await load()
}, 5000)

onMounted(load)
</script>

<template>
  <PageHeader title="域名" sub="查看可用于创建邮箱的域名，或提交自己的域名加入域名池">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="load">
        <RefreshCw :class="{ spin: loading }" aria-hidden="true" />刷新
      </button>
      <button type="button" class="btn btn-primary" @click="modal = true"><Zap aria-hidden="true" />提交域名</button>
    </template>
  </PageHeader>

  <section v-if="pending.length" class="card" aria-labelledby="h-pending">
    <div class="card-h">
      <h2 id="h-pending"><Clock aria-hidden="true" />待 MX 验证 · {{ pending.length }}</h2>
      <span class="sub">后台每 30 秒自动检测，通过后自动激活</span>
    </div>
    <div class="table-wrap">
      <table class="tbl">
        <thead><tr><th>域名</th><th>上次检测</th><th>检测结果</th></tr></thead>
        <tbody>
          <tr v-for="d in pending" :key="d.id">
            <td><code>{{ d.domain }}</code></td>
            <td class="muted nowrap">
              {{ (live[d.id]?.mx_checked_at || d.mx_checked_at) ? timeAgo(live[d.id]?.mx_checked_at || d.mx_checked_at) : '等待首次检测' }}
            </td>
            <td>
              <span v-if="!live[d.id]" class="badge b-warn"><Clock aria-hidden="true" />检测中</span>
              <MxDetails v-else :details="live[d.id].mx_details" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>

  <div class="cols">
    <section class="card" aria-labelledby="h-pool">
      <div class="card-h">
        <h2 id="h-pool"><Globe aria-hidden="true" />域名池</h2>
        <span class="sub">{{ pool.filter(d => d.is_active).length }} 个可用</span>
      </div>
      <SkeletonRows v-if="loading" :rows="4" :cols="3" />
      <EmptyState v-else-if="!pool.length" :icon="Globe" title="暂无域名">提交一个域名，MX 验证通过后即可使用。</EmptyState>
      <div v-else class="table-wrap">
        <table class="tbl">
          <thead><tr><th>域名</th><th>能力</th><th>状态</th></tr></thead>
          <tbody>
            <tr v-for="d in pool" :key="d.id">
              <td><code>{{ d.base_domain || d.domain }}</code></td>
              <td>
                <span class="caps">
                  <span v-if="d.supports_single" class="badge b-brand">单域名</span>
                  <span v-if="d.supports_wildcard" class="badge b-brand">多级子域</span>
                  <span v-if="!d.supports_single && !d.supports_wildcard" class="muted small">—</span>
                </span>
              </td>
              <td>
                <span v-if="d.is_active" class="badge b-ok"><CircleCheck aria-hidden="true" />启用</span>
                <span v-else class="badge b-muted"><CirclePause aria-hidden="true" />停用</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="card" aria-labelledby="h-guide">
      <div class="card-h"><h2 id="h-guide"><BookOpen aria-hidden="true" />添加域名指南</h2></div>
      <div class="card-b">
        <ol class="steps">
          <li class="step">
            <div>
              <h4>准备域名</h4>
              <div class="d"><p>在域名注册商购买域名（例如 <code>example.com</code>），并确认你能管理它的 DNS。</p></div>
            </div>
          </li>
          <li class="step">
            <div>
              <h4>添加 DNS 记录</h4>
              <div class="d">
                <span>系统会分别检测单域名 MX 和通配子域 MX，并按检测结果开放对应的生成模式。</span>
                <DnsTable :records="guide" compact />
              </div>
            </div>
          </li>
          <li class="step">
            <div>
              <h4>提交自动验证</h4>
              <div class="d">
                <span>DNS 生效后（通常 5–30 分钟）提交域名。MX 已生效会立即激活；未生效则进入待验证队列，后台每 30 秒自动重试。</span>
                <div><button type="button" class="btn btn-primary btn-sm" @click="modal = true"><Zap aria-hidden="true" />提交域名</button></div>
              </div>
            </div>
          </li>
          <li class="step">
            <div>
              <h4>测试收信</h4>
              <div class="d">激活后用该域名创建邮箱，从其他邮箱发一封测试邮件，通常 30 秒内即可收到。</div>
            </div>
          </li>
        </ol>
      </div>
    </section>
  </div>

  <MxRegisterModal :open="modal" @close="modal = false" @changed="load" />
</template>

<style scoped>
.cols { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.15fr); gap: 16px; align-items: start; }
.caps { display: inline-flex; gap: 4px; flex-wrap: wrap; }
.steps { list-style: none; padding: 0; }
@media (max-width: 1000px) { .cols { grid-template-columns: minmax(0, 1fr); } }
</style>
