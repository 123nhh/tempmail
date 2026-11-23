<script setup>
import { computed } from 'vue'
import { KeyRound, ShieldAlert, BookOpen, ArrowRight } from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import KeyValue from '../components/ui/KeyValue.vue'
import CodeBlock from '../components/ui/CodeBlock.vue'
import { useSession } from '../stores/session'
import { formatFull } from '../utils/format'

const session = useSession()
const base = window.location.origin
const example = computed(() => `curl -H "Authorization: Bearer YOUR_API_KEY" ${base}/api/mailboxes`)
</script>

<template>
  <PageHeader title="我的 API Key" sub="用于登录控制台和调用所有 /api 接口" />

  <section class="card narrow">
    <div class="card-h"><h2><KeyRound aria-hidden="true" />API Key</h2></div>
    <div class="card-b stack">
      <KeyValue :value="session.apiKey" secret copy-label="复制 API Key" />
      <dl class="dl">
        <dt>账户</dt><dd>{{ session.username }}</dd>
        <dt>角色</dt><dd>{{ session.isAdmin ? '管理员' : '普通用户' }}</dd>
        <dt v-if="session.account?.created_at">创建时间</dt><dd v-if="session.account?.created_at">{{ formatFull(session.account.created_at) }}</dd>
      </dl>
      <div class="notice warn">
        <ShieldAlert aria-hidden="true" />
        <span class="grow">API Key 等同于账户密码，请勿公开或提交到代码仓库。丢失或泄露时请联系管理员重新创建账户。</span>
      </div>
    </div>
  </section>

  <section class="card narrow">
    <div class="card-h">
      <h2><BookOpen aria-hidden="true" />调用示例</h2>
      <RouterLink :to="{ name: 'docs' }" class="btn btn-ghost btn-sm">完整文档<ArrowRight aria-hidden="true" /></RouterLink>
    </div>
    <div class="card-b stack">
      <CodeBlock :code="example" lang="bash" />
      <p class="small muted">把 <code>YOUR_API_KEY</code> 换成上面的 Key。也可以用 <code>X-API-Key</code> 请求头或 <code>?api_key=</code> 参数传递。</p>
    </div>
  </section>
</template>

<style scoped>
.narrow { max-width: 720px; }
</style>
