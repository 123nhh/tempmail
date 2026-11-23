<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ArrowLeft, Trash2, Image as ImageIcon, ImageOff, KeyRound, FileText, Code, Paperclip } from '@lucide/vue'
import CopyButton from '../ui/CopyButton.vue'
import EmptyState from '../ui/EmptyState.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { bytes, formatFull, initial, senderName } from '../../utils/format'
import { extractOtp, htmlToText } from '../../utils/otp'

const props = defineProps({
  mailboxId: { type: String, required: true },
  emailId: { type: String, required: true },
  address: { type: String, default: '' },
})
const emit = defineEmits(['back', 'deleted'])

const email = ref(null)
const loading = ref(true)
const error = ref('')
const view = ref('html')
const remote = ref(false)
const frame = ref(null)
let ro = null

const html = computed(() => email.value?.body_html || '')
const text = computed(() => email.value?.body_text || (html.value ? htmlToText(html.value) : ''))
const otp = computed(() => (email.value ? extractOtp({ subject: email.value.subject, text: email.value.body_text, html: html.value }) : null))
const hasRemote = computed(() => /<img[^>]+src=["']?https?:/i.test(html.value) || /url\(\s*['"]?https?:/i.test(html.value))

// 以 srcdoc 渲染邮件：禁止脚本，默认阻止远程资源（防追踪像素），链接新窗口打开
const srcdoc = computed(() => {
  if (!html.value) return ''
  const img = remote.value ? "img-src data: cid: https: http:; style-src 'unsafe-inline' https: http:; font-src https: http: data:;" : "img-src data: cid:; style-src 'unsafe-inline'; font-src data:;"
  const head = `<meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; ${img}"><base target="_blank"><style>html,body{margin:0;padding:16px;font:14px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;color:#1C1917;background:#fff;word-wrap:break-word;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}</style>`
  return `<!doctype html><html><head>${head}</head><body>${html.value}</body></html>`
})

async function load() {
  loading.value = true
  error.value = ''
  remote.value = false
  try {
    email.value = await api.getEmail(props.mailboxId, props.emailId)
    view.value = email.value.body_html ? 'html' : 'text'
  } catch (e) {
    email.value = null
    error.value = e.status === 404 ? '邮件不存在或已被删除' : e.message
  } finally {
    loading.value = false
  }
}

function fit() {
  const f = frame.value
  if (!f) return
  try {
    const doc = f.contentDocument
    if (!doc?.body) return
    f.style.height = `${Math.max(doc.documentElement.scrollHeight, doc.body.scrollHeight) + 4}px`
    if (!ro && window.ResizeObserver) {
      ro = new ResizeObserver(() => {
        try { f.style.height = `${doc.documentElement.scrollHeight + 4}px` } catch { /* 忽略 */ }
      })
      ro.observe(doc.body)
    }
  } catch {
    f.style.height = '70vh'
  }
}

function onLoad() {
  ro?.disconnect()
  ro = null
  fit()
  setTimeout(fit, 300)
}

async function remove() {
  try {
    await api.deleteEmail(props.mailboxId, props.emailId)
    toast.success('邮件已删除')
    emit('deleted', props.emailId)
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
  }
}

watch(() => [props.mailboxId, props.emailId], load, { immediate: true })
watch(view, async v => { if (v === 'html') { await nextTick(); fit() } })
onBeforeUnmount(() => ro?.disconnect())
</script>

<template>
  <article class="reader" aria-live="polite">
    <div class="reader-bar">
      <button type="button" class="btn btn-ghost btn-sm back" @click="emit('back')"><ArrowLeft aria-hidden="true" />返回列表</button>
      <span class="spacer" />
      <button v-if="email" type="button" class="btn btn-danger btn-sm" @click="remove"><Trash2 aria-hidden="true" />删除</button>
    </div>

    <div v-if="loading" class="reader-body stack">
      <span class="skel" style="width: 60%; height: 20px" />
      <span class="skel" style="width: 40%" />
      <span class="skel" style="width: 100%; height: 160px" />
    </div>

    <EmptyState v-else-if="error" :icon="FileText" title="无法打开邮件">{{ error }}</EmptyState>

    <div v-else-if="email" class="reader-body">
      <h2 class="subj">{{ email.subject || '(无主题)' }}</h2>
      <div class="meta">
        <span class="av" aria-hidden="true">{{ initial(email.sender) }}</span>
        <div class="meta-t">
          <div class="from"><strong>{{ senderName(email.sender) }}</strong><span v-if="senderName(email.sender) !== email.sender" class="muted small mono addr-full">{{ email.sender }}</span></div>
          <div class="small muted">
            发送至 <span class="mono">{{ address || '—' }}</span> · <time :datetime="email.received_at">{{ formatFull(email.received_at) }}</time>
            <template v-if="email.size_bytes"> · {{ bytes(email.size_bytes) }}</template>
          </div>
        </div>
      </div>

      <div v-if="otp" class="otp">
        <KeyRound aria-hidden="true" />
        <div class="otp-t">
          <span class="small muted">识别到验证码</span>
          <b>{{ otp }}</b>
        </div>
        <CopyButton :text="otp" label="复制验证码" :toast-text="`已复制验证码 ${otp}`" show-label small />
      </div>

      <div class="body-bar">
        <div v-if="html" class="seg" role="radiogroup" aria-label="显示方式">
          <button type="button" role="radio" :aria-checked="view === 'html' ? 'true' : 'false'" @click="view = 'html'"><Code aria-hidden="true" />HTML</button>
          <button type="button" role="radio" :aria-checked="view === 'text' ? 'true' : 'false'" @click="view = 'text'"><FileText aria-hidden="true" />纯文本</button>
        </div>
        <button v-if="view === 'html' && hasRemote" type="button" class="btn btn-ghost btn-sm" @click="remote = !remote">
          <component :is="remote ? ImageOff : ImageIcon" aria-hidden="true" />{{ remote ? '阻止远程图片' : '显示远程图片' }}
        </button>
      </div>
      <p v-if="view === 'html' && hasRemote && !remote" class="small muted blocked">
        <Paperclip aria-hidden="true" />为保护隐私，已阻止加载远程图片。
      </p>

      <iframe
        v-if="view === 'html' && html"
        ref="frame"
        :key="`${emailId}-${remote}`"
        class="mail-frame"
        title="邮件正文"
        sandbox="allow-same-origin allow-popups allow-popups-to-escape-sandbox"
        referrerpolicy="no-referrer"
        :srcdoc="srcdoc"
        @load="onLoad"
      />
      <pre v-else class="mail-text">{{ text || '(邮件内容为空)' }}</pre>
    </div>
  </article>
</template>

<style scoped>
.reader { display: flex; flex-direction: column; min-width: 0; min-height: 100%; }
.reader-bar { display: flex; align-items: center; gap: 8px; padding: 10px 16px; border-bottom: 1px solid var(--border); position: sticky; top: 0; background: var(--surface); z-index: 2; }
.reader-body { padding: 20px 24px 28px; display: grid; gap: 16px; align-content: start; min-width: 0; }
.subj { font-size: 18px; font-weight: 650; overflow-wrap: anywhere; }
.meta { display: flex; gap: 12px; align-items: center; min-width: 0; }
.meta-t { min-width: 0; display: grid; gap: 2px; }
.meta .from { overflow-wrap: anywhere; }
.addr-full { margin-left: 6px; }
.av { width: 36px; height: 36px; border-radius: 50%; flex: none; display: grid; place-items: center; font-weight: 600; background: var(--tint-strong); color: var(--on-fill); }
[data-theme="dark"] .av { color: var(--text); }
.mono { font-family: var(--mono); }
.otp {
  display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
  padding: 12px 16px; border: 1px dashed var(--fill-edge); border-radius: var(--r-md); background: var(--tint);
}
.otp > svg { width: 18px; height: 18px; color: var(--accent); }
.otp-t { display: grid; }
.otp b { font-family: var(--mono); font-size: 24px; letter-spacing: 0.15em; line-height: 1.2; }
.otp .btn { margin-left: auto; }
.body-bar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.blocked { display: flex; align-items: center; gap: 6px; margin-top: -8px; }
.blocked svg { width: 13px; height: 13px; }
.mail-frame { width: 100%; min-height: 240px; border: 1px solid var(--border); border-radius: var(--r-sm); background: #fff; }
.mail-text {
  margin: 0; padding: 16px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: color-mix(in oklab, var(--bg) 60%, var(--surface));
  white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--font); font-size: 14px; line-height: 1.7;
}
.back { display: none; }
@media (max-width: 900px) {
  .back { display: inline-flex; }
  .reader-body { padding: 16px; }
}
</style>
