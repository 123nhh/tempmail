<script setup>
import { computed, ref } from 'vue'
import { Lock, LockOpen, ShieldCheck } from '@lucide/vue'
import CodeBlock from '../ui/CodeBlock.vue'
import { curl, js, python } from '../../docs/endpoints'

const props = defineProps({
  ep: { type: Object, required: true },
  base: { type: String, required: true },
  apiKey: { type: String, required: true },
  lang: { type: String, default: 'curl' },
  admin: { type: Boolean, default: false },
})
const emit = defineEmits(['update:lang'])

const LANGS = [
  { id: 'curl', label: 'cURL', fn: curl, hl: 'bash' },
  { id: 'python', label: 'Python', fn: python, hl: 'python' },
  { id: 'js', label: 'JavaScript', fn: js, hl: 'javascript' },
]

const variant = ref(0)
const body = computed(() => (props.ep.variants ? props.ep.variants[variant.value].body : props.ep.example))
const current = computed(() => LANGS.find(l => l.id === props.lang) || LANGS[0])
const code = computed(() => current.value.fn(props.ep, props.base, props.apiKey, body.value))
const response = computed(() => (props.ep.response ? JSON.stringify(props.ep.response, null, 2) : ''))
const pathHtml = computed(() => props.ep.path.split(/(:[a-z_]+|\{[a-z_]+\})/g))
const pathParams = computed(() => (props.ep.params || []).filter(p => p.in === 'path'))
const queryParams = computed(() => (props.ep.params || []).filter(p => p.in === 'query'))
</script>

<template>
  <article :id="ep.id" class="card ep">
    <header class="ep-h">
      <div class="ep-line">
        <span class="method" :class="`m-${ep.method.toLowerCase()}`">{{ ep.method }}</span>
        <code class="path"><template v-for="(seg, i) in pathHtml" :key="i"><span v-if="/^[:{]/.test(seg)" class="pv">{{ seg }}</span><template v-else>{{ seg }}</template></template></code>
      </div>
      <h3>{{ ep.title }}</h3>
      <p v-if="ep.desc" class="text-2 small">{{ ep.desc }}</p>
      <div class="auth small">
        <template v-if="ep.auth === false"><LockOpen aria-hidden="true" />无需鉴权</template>
        <template v-else-if="admin"><ShieldCheck aria-hidden="true" />需要管理员 API Key</template>
        <template v-else><Lock aria-hidden="true" />需要 API Key</template>
      </div>
    </header>

    <div class="ep-b">
      <div class="ep-doc">
        <div v-if="pathParams.length || queryParams.length" class="blk">
          <h4>{{ pathParams.length && queryParams.length ? '路径与查询参数' : pathParams.length ? '路径参数' : '查询参数' }}</h4>
          <table class="ptbl">
            <tbody>
              <tr v-for="p in [...pathParams, ...queryParams]" :key="p.name">
                <td><code>{{ p.name }}</code></td>
                <td class="ty">{{ p.type }}</td>
                <td><span v-if="p.required" class="req">必填</span></td>
                <td class="d">{{ p.desc }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="ep.body" class="blk">
          <h4>请求体 <span class="muted small">application/json</span></h4>
          <table class="ptbl">
            <tbody>
              <tr v-for="p in ep.body" :key="p.name">
                <td><code>{{ p.name }}</code></td>
                <td class="ty">{{ p.type }}</td>
                <td><span v-if="p.required" class="req">必填</span><span v-else class="muted small">可选</span></td>
                <td class="d">{{ p.desc }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-if="ep.errors?.length" class="blk">
          <h4>常见错误</h4>
          <table class="ptbl">
            <tbody>
              <tr v-for="e in ep.errors" :key="e.code">
                <td><span class="badge" :class="e.code >= 500 ? 'b-bad' : 'b-warn'">{{ e.code }}</span></td>
                <td class="d" colspan="3">{{ e.desc }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="!ep.noCode" class="ep-code">
        <div class="code-bar">
          <div class="seg" role="radiogroup" aria-label="示例语言">
            <button
              v-for="l in LANGS"
              :key="l.id"
              type="button"
              role="radio"
              :aria-checked="lang === l.id ? 'true' : 'false'"
              @click="emit('update:lang', l.id)"
            >
              {{ l.label }}
            </button>
          </div>
          <select v-if="ep.variants" v-model="variant" class="inp sel" aria-label="示例场景">
            <option v-for="(v, i) in ep.variants" :key="i" :value="i">{{ v.label }}</option>
          </select>
        </div>
        <CodeBlock :code="code" :lang="current.label" />
        <template v-if="response">
          <h4 class="rh">响应 <span class="badge b-ok">{{ ep.status || 200 }}</span></h4>
          <CodeBlock :code="response" lang="JSON" />
          <p v-if="ep.extra" class="small muted">{{ ep.extra }}</p>
        </template>
      </div>
    </div>
  </article>
</template>

<style scoped>
.ep { scroll-margin-top: 16px; }
.ep-h { padding: 16px 20px; border-bottom: 1px solid var(--border); display: grid; gap: 6px; }
.ep-line { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.path { font-family: var(--mono); font-size: 14px; background: none; border: 0; padding: 0; color: var(--text); overflow-wrap: anywhere; }
.pv { color: var(--accent); }
.ep-h h3 { font-size: 15px; font-weight: 600; }
.auth { display: inline-flex; align-items: center; gap: 5px; color: var(--text-3); }
.auth svg { width: 13px; height: 13px; }

.method {
  font: 600 11px/1 var(--mono); letter-spacing: 0.04em; padding: 5px 7px; border-radius: 4px;
  border: 1px solid transparent; flex: none;
}
.m-get { background: var(--ok-bg); color: var(--ok); }
.m-post { background: var(--fill); color: var(--on-fill); }
.m-put { background: var(--warn-bg); color: var(--warn); }
.m-delete { background: var(--bad-bg); color: var(--bad); }

.ep-b { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1.1fr); }
.ep-doc { padding: 16px 20px; display: grid; grid-template-columns: minmax(0, 1fr); gap: 18px; align-content: start; }
.ep-code { padding: 16px 20px; border-left: 1px solid var(--border); display: grid; grid-template-columns: minmax(0, 1fr); gap: 10px; align-content: start; background: color-mix(in oklab, var(--bg) 55%, var(--surface)); min-width: 0; }
.ep-doc:empty { display: none; }
.ep-doc:empty + .ep-code { grid-column: 1 / -1; border-left: 0; }

.blk h4, .rh { font-size: 12px; font-weight: 600; color: var(--text-2); text-transform: uppercase; letter-spacing: 0.04em; margin-bottom: 6px; display: flex; align-items: center; gap: 6px; }
.blk h4 .muted { text-transform: none; letter-spacing: 0; font-weight: 400; }
.rh { margin: 6px 0 0; }
.ptbl { width: 100%; border-collapse: collapse; font-size: 13px; }
.ptbl td { padding: 7px 8px 7px 0; border-top: 1px solid var(--border); vertical-align: top; }
.ptbl tr:first-child td { border-top: 0; }
.ptbl code { white-space: nowrap; }
.ptbl .ty { color: var(--text-3); font-family: var(--mono); font-size: 12px; white-space: nowrap; }
.ptbl .d { color: var(--text-2); width: 100%; }
.req { font-size: 11px; color: var(--bad); white-space: nowrap; }

.code-bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.sel { width: auto; height: 30px; font-size: 12px; padding: 0 28px 0 8px; }

@media (max-width: 1200px) {
  .ep-b { grid-template-columns: minmax(0, 1fr); }
  .ep-code { border-left: 0; border-top: 1px solid var(--border); }
  .ep-doc:empty + .ep-code { border-top: 0; }
}
</style>
