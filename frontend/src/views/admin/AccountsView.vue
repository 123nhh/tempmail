<script setup>
import { onMounted, ref } from 'vue'
import { Plus, Users, Trash2, ChevronLeft, ChevronRight, RefreshCw, ShieldCheck, User } from '@lucide/vue'
import PageHeader from '../../components/ui/PageHeader.vue'
import BaseModal from '../../components/ui/BaseModal.vue'
import KeyValue from '../../components/ui/KeyValue.vue'
import SkeletonRows from '../../components/ui/SkeletonRows.vue'
import EmptyState from '../../components/ui/EmptyState.vue'
import { api } from '../../api/client'
import { confirm, toast } from '../../composables/feedback'
import { formatDate, num } from '../../utils/format'

const SIZE = 10
const page = ref(1)
const total = ref(0)
const rows = ref([])
const loading = ref(true)

const createOpen = ref(false)
const username = ref('')
const creating = ref(false)
const createErr = ref('')
const created = ref(null)

const pages = () => Math.max(1, Math.ceil(total.value / SIZE))

async function load(p = page.value) {
  loading.value = true
  try {
    const r = await api.admin.listAccounts(p, SIZE)
    rows.value = r.data || []
    total.value = r.total || 0
    page.value = p
  } catch (e) {
    toast.error(`加载失败：${e.message}`)
  } finally {
    loading.value = false
  }
}

function openCreate() {
  username.value = ''
  createErr.value = ''
  created.value = null
  createOpen.value = true
}

async function create() {
  const u = username.value.trim()
  if (u.length < 2 || u.length > 64) {
    createErr.value = '用户名长度需为 2–64 个字符'
    return
  }
  creating.value = true
  createErr.value = ''
  try {
    created.value = await api.admin.createAccount(u)
    toast.success('账户已创建')
    load(1)
  } catch (e) {
    createErr.value = e.status === 409 ? '用户名已存在' : e.message
  } finally {
    creating.value = false
  }
}

async function remove(a) {
  const ok = await confirm({
    title: '删除账户',
    message: `确定删除账户 ${a.username}？`,
    detail: '该账户的 API Key 将立即失效，其邮箱和邮件也会一并删除。',
    confirmText: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await api.admin.deleteAccount(a.id)
    toast.success('账户已删除')
    load(rows.value.length === 1 && page.value > 1 ? page.value - 1 : page.value)
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
  }
}

onMounted(() => load(1))
</script>

<template>
  <PageHeader title="账户管理" :sub="`共 ${num(total)} 个账户`">
    <template #actions>
      <button type="button" class="btn btn-secondary" :disabled="loading" @click="load()">
        <RefreshCw :class="{ spin: loading }" aria-hidden="true" />刷新
      </button>
      <button type="button" class="btn btn-primary" @click="openCreate"><Plus aria-hidden="true" />创建账户</button>
    </template>
  </PageHeader>

  <section class="card">
    <SkeletonRows v-if="loading && !rows.length" :rows="5" :cols="5" />
    <EmptyState v-else-if="!rows.length" :icon="Users" title="暂无账户" />
    <div v-else class="table-wrap">
      <table class="tbl">
        <thead>
          <tr>
            <th>用户</th>
            <th>API Key</th>
            <th class="num">邮箱（活跃 / 全部）</th>
            <th class="num">当前邮件</th>
            <th class="num" title="过期邮箱会被自动删除，其收件数不再计入">现存邮箱收件</th>
            <th>创建时间</th>
            <th class="act"><span class="sr-only">操作</span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="a in rows" :key="a.id">
            <td>
              <div class="user">
                <strong>{{ a.username }}</strong>
                <span v-if="a.is_admin" class="badge b-brand"><ShieldCheck aria-hidden="true" />管理员</span>
                <span v-else class="badge b-muted"><User aria-hidden="true" />用户</span>
              </div>
            </td>
            <td class="key"><KeyValue :value="a.api_key" secret copy-label="复制 API Key" /></td>
            <td class="num">{{ num(a.active_mailbox_count || 0) }} / {{ num(a.mailbox_count || 0) }}</td>
            <td class="num">{{ num(a.current_email_count || 0) }}</td>
            <td class="num">{{ num(a.received_email_count || 0) }}</td>
            <td class="muted nowrap">{{ formatDate(a.created_at) }}</td>
            <td class="act">
              <button
                v-if="!a.is_admin"
                type="button"
                class="icon-btn icon-btn-sm danger"
                :aria-label="`删除账户 ${a.username}`"
                title="删除账户"
                @click="remove(a)"
              >
                <Trash2 aria-hidden="true" />
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-if="total > SIZE" class="pager">
      <span class="small muted">第 {{ page }} / {{ pages() }} 页</span>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="load(page - 1)">
        <ChevronLeft aria-hidden="true" />上一页
      </button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="page >= pages() || loading" @click="load(page + 1)">
        下一页<ChevronRight aria-hidden="true" />
      </button>
    </div>
  </section>

  <BaseModal :open="createOpen" title="创建账户" :icon="Plus" @close="createOpen = false">
    <template v-if="!created">
      <form id="acc-form" class="field" @submit.prevent="create">
        <label for="acc-name">用户名</label>
        <input id="acc-name" v-model="username" class="inp" placeholder="username" maxlength="64" autocomplete="off" />
        <span class="hint">2–64 个字符。创建后会生成该账户的 API Key。</span>
      </form>
      <p v-if="createErr" class="err-t" role="alert">{{ createErr }}</p>
    </template>
    <template v-else>
      <p>账户 <strong>{{ created.username }}</strong> 已创建，请把下面的 API Key 发给对方：</p>
      <KeyValue :value="created.api_key" copy-label="复制 API Key" />
    </template>
    <template #footer>
      <template v-if="!created">
        <button type="button" class="btn btn-secondary" @click="createOpen = false">取消</button>
        <button type="submit" form="acc-form" class="btn btn-primary" :disabled="creating">{{ creating ? '创建中…' : '创建' }}</button>
      </template>
      <button v-else type="button" class="btn btn-primary" @click="createOpen = false">完成</button>
    </template>
  </BaseModal>
</template>

<style scoped>
.user { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.key { min-width: 220px; max-width: 320px; }
.key :deep(.kv) { padding: 3px 4px 3px 10px; font-size: 12px; }
.pager { display: flex; align-items: center; justify-content: flex-end; gap: 8px; padding: 12px 14px; border-top: 1px solid var(--border); }
.pager .small { margin-right: auto; }
</style>
