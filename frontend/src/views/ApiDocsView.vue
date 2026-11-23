<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { KeyRound, Server, TriangleAlert, Gauge, BookOpen } from '@lucide/vue'
import PageHeader from '../components/ui/PageHeader.vue'
import KeyValue from '../components/ui/KeyValue.vue'
import CodeBlock from '../components/ui/CodeBlock.vue'
import EndpointCard from '../components/docs/EndpointCard.vue'
import { groups, recipes } from '../docs/endpoints'
import { useSession } from '../stores/session'

const session = useSession()
const base = window.location.origin
const LANG_KEY = 'tm_docs_lang'
const lang = ref(localStorage.getItem(LANG_KEY) || 'curl')
const showKey = ref(false)
const active = ref('')

function setLang(l) {
  lang.value = l
  localStorage.setItem(LANG_KEY, l)
}

const visible = computed(() => groups.filter(g => !g.admin || session.isAdmin))
// 示例代码默认用占位符，用户主动选择后才填入真实 Key
const exampleKey = computed(() => (showKey.value && session.apiKey ? session.apiKey : 'YOUR_API_KEY'))
const recipeList = computed(() => recipes(base, exampleKey.value))

const authExamples = computed(() => [
  `curl -H "Authorization: Bearer ${exampleKey.value}" ${base}/api/me`,
  `curl -H "X-API-Key: ${exampleKey.value}" ${base}/api/me`,
  `curl "${base}/api/me?api_key=${exampleKey.value}"`,
].join('\n'))

const errorExample = JSON.stringify({ error: 'invalid api_key' }, null, 2)
const rateExample = JSON.stringify({ error: 'rate limit exceeded', limit: 500, retry_after: 60 }, null, 2)

// 滚动时高亮左侧导航
let io = null
onMounted(() => {
  if (!window.IntersectionObserver) return
  io = new IntersectionObserver(
    entries => {
      const hit = entries.filter(e => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)[0]
      if (hit) active.value = hit.target.id
    },
    { rootMargin: '0px 0px -70% 0px' },
  )
  document.querySelectorAll('.docs-main [data-sec], .docs-main .ep').forEach(el => io.observe(el))
})
onBeforeUnmount(() => io?.disconnect())
</script>

<template>
  <PageHeader title="API 文档" sub="用 HTTP 接口创建临时邮箱、收取邮件。所有请求与响应均为 JSON。" />

  <div class="docs">
    <nav class="docs-nav" aria-label="文档目录">
      <a href="#intro" :class="{ on: active === 'intro' }">快速开始</a>
      <a href="#auth" :class="{ on: active === 'auth' }">鉴权</a>
      <a href="#errors" :class="{ on: active === 'errors' }">错误与限流</a>
      <template v-for="g in visible" :key="g.id">
        <a :href="`#${g.id}`" class="grp" :class="{ on: active === g.id }">{{ g.title }}</a>
        <a
          v-for="ep in g.endpoints"
          :key="ep.id"
          :href="`#${ep.id}`"
          class="sub"
          :class="{ on: active === ep.id }"
        >
          <span class="mth" :class="`m-${ep.method.toLowerCase()}`">{{ ep.method === 'DELETE' ? 'DEL' : ep.method }}</span>{{ ep.title }}
        </a>
      </template>
      <a href="#recipes" class="grp" :class="{ on: active === 'recipes' }">实战示例</a>
    </nav>

    <div class="docs-main">
      <section id="intro" data-sec class="card">
        <div class="card-h"><h2><BookOpen aria-hidden="true" />快速开始</h2></div>
        <div class="card-b stack">
          <dl class="dl">
            <dt><Server class="i" aria-hidden="true" />Base URL</dt>
            <dd><KeyValue :value="base" copy-label="复制 Base URL" /></dd>
            <dt><KeyRound class="i" aria-hidden="true" />我的 API Key</dt>
            <dd><KeyValue :value="session.apiKey" secret copy-label="复制 API Key" /></dd>
          </dl>
          <label class="chk small">
            <input v-model="showKey" type="checkbox" />
            在示例代码中填入我的 API Key（默认显示 <code>YOUR_API_KEY</code> 占位符，便于分享截图）
          </label>
          <p class="small text-2">
            典型流程：<a href="#mailbox-create">创建邮箱</a> → 把地址填到需要收信的地方 →
            <a href="#email-list">轮询收件列表</a> → <a href="#email-get">读取邮件</a>。
            邮箱到期后会自动删除，无需手动清理。
          </p>
        </div>
      </section>

      <section id="auth" data-sec class="card">
        <div class="card-h"><h2><KeyRound aria-hidden="true" />鉴权</h2></div>
        <div class="card-b stack">
          <p class="small text-2">
            <code>/api/*</code> 下的接口都需要 API Key，支持三种传递方式，任选其一；推荐使用 <code>Authorization</code> 请求头。
            <code>/public/*</code> 接口无需鉴权。
          </p>
          <table class="tbl mini">
            <thead><tr><th>方式</th><th>示例</th></tr></thead>
            <tbody>
              <tr><td>Authorization 头</td><td><code>Authorization: Bearer &lt;key&gt;</code></td></tr>
              <tr><td>X-API-Key 头</td><td><code>X-API-Key: &lt;key&gt;</code></td></tr>
              <tr><td>查询参数</td><td><code>?api_key=&lt;key&gt;</code>（会出现在日志里，不推荐）</td></tr>
            </tbody>
          </table>
          <CodeBlock :code="authExamples" lang="bash" />
        </div>
      </section>

      <section id="errors" data-sec class="card">
        <div class="card-h"><h2><TriangleAlert aria-hidden="true" />错误与限流</h2></div>
        <div class="card-b stack">
          <p class="small text-2">出错时返回对应的 HTTP 状态码，响应体统一为 <code>{"error": "..."}</code>。</p>
          <div class="two">
            <table class="tbl mini">
              <tbody>
                <tr><td><span class="badge b-warn">400</span></td><td>参数错误</td></tr>
                <tr><td><span class="badge b-warn">401</span></td><td>缺少或无效的 API Key</td></tr>
                <tr><td><span class="badge b-warn">403</span></td><td>无权限（非管理员 / 功能已关闭）</td></tr>
                <tr><td><span class="badge b-warn">404</span></td><td>资源不存在或不属于当前账户</td></tr>
                <tr><td><span class="badge b-warn">409</span></td><td>资源冲突（地址 / 用户名 / 域名已存在）</td></tr>
                <tr><td><span class="badge b-warn">429</span></td><td>请求过于频繁</td></tr>
                <tr><td><span class="badge b-bad">5xx</span></td><td>服务端错误或没有可用域名</td></tr>
              </tbody>
            </table>
            <CodeBlock :code="errorExample" lang="JSON" />
          </div>
          <h3 class="h3"><Gauge class="i" aria-hidden="true" />速率限制</h3>
          <p class="small text-2">
            <code>/api/*</code> 按 API Key 计数，默认每 60 秒 500 次（部署时由 <code>RATE_LIMIT</code>、<code>RATE_WINDOW</code> 环境变量调整）。响应头会返回
            <code>X-RateLimit-Limit</code>、<code>X-RateLimit-Remaining</code>、<code>X-RateLimit-Reset</code>（Unix 秒）。
            超限返回 429，等待 <code>retry_after</code> 秒后重试。
          </p>
          <CodeBlock :code="rateExample" lang="JSON" />
        </div>
      </section>

      <section v-for="g in visible" :key="g.id" class="grp-sec">
        <header :id="g.id" data-sec class="grp-h">
          <h2>{{ g.title }}</h2>
          <p v-if="g.desc" class="small text-2">{{ g.desc }}</p>
        </header>
        <EndpointCard
          v-for="ep in g.endpoints"
          :key="ep.id"
          :ep="ep"
          :base="base"
          :api-key="exampleKey"
          :admin="!!g.admin"
          :lang="lang"
          @update:lang="setLang"
        />
      </section>

      <section class="grp-sec">
        <header id="recipes" data-sec class="grp-h">
          <h2>实战示例</h2>
          <p class="small text-2">可直接复制运行的脚本。</p>
        </header>
        <article v-for="r in recipeList" :id="r.id" :key="r.id" class="card">
          <div class="card-h"><h3 class="h3">{{ r.title }}</h3></div>
          <div class="card-b"><CodeBlock :code="r.code" :lang="r.lang" /></div>
        </article>
      </section>
    </div>
  </div>
</template>

<style scoped>
.docs { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 24px; align-items: start; }
.docs-nav {
  position: sticky; top: 16px; max-height: calc(100vh - 32px); overflow-y: auto;
  display: grid; gap: 1px; font-size: 13px; padding-right: 4px;
}
.docs-nav a {
  display: flex; align-items: center; gap: 6px; padding: 5px 10px; border-radius: var(--r-sm);
  color: var(--text-2); text-decoration: none; min-width: 0;
}
.docs-nav a:hover { background: var(--tint); color: var(--text); }
.docs-nav a.on { background: var(--tint); color: var(--text); box-shadow: inset 2px 0 0 var(--accent); }
.docs-nav .grp { font-weight: 600; color: var(--text); margin-top: 10px; }
.docs-nav .sub { padding-left: 14px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.mth { font: 600 9.5px/1 var(--mono); width: 30px; flex: none; }
.m-get { color: var(--ok); }
.m-post { color: var(--accent); }
.m-put { color: var(--warn); }
.m-delete { color: var(--bad); }

.docs-main { display: grid; grid-template-columns: minmax(0, 1fr); gap: 16px; min-width: 0; }
.docs-main [data-sec] { scroll-margin-top: 16px; }
.grp-sec { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; }
.grp-h { padding-top: 16px; display: grid; gap: 4px; }
.grp-h h2 { font-size: 18px; font-weight: 650; }
.h3 { font-size: 14px; font-weight: 600; display: flex; align-items: center; gap: 6px; }
.i { width: 14px; height: 14px; vertical-align: -2px; margin-right: 4px; }
.dl dt { display: flex; align-items: center; }
.chk { display: flex; align-items: center; gap: 8px; color: var(--text-2); cursor: pointer; }
.chk input { accent-color: var(--accent); width: 15px; height: 15px; }
.two { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; align-items: start; }
.mini td, .mini th { padding: 7px 10px; }
.mini { border: 1px solid var(--border); border-radius: var(--r-sm); }

@media (max-width: 1000px) {
  .docs { grid-template-columns: minmax(0, 1fr); }
  .docs-nav { display: none; }
  .two { grid-template-columns: minmax(0, 1fr); }
}
</style>
