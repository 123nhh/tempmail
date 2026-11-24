<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { ArrowLeft, Trash2, Image as ImageIcon, ImageOff, KeyRound, FileText, Code, Paperclip, Moon, Sun } from '@lucide/vue'
import CopyButton from '../ui/CopyButton.vue'
import EmptyState from '../ui/EmptyState.vue'
import { api } from '../../api/client'
import { toast } from '../../composables/feedback'
import { useTheme } from '../../stores/theme'
import { bytes, formatFull, initial, senderName } from '../../utils/format'
import { extractOtp, htmlToText } from '../../utils/otp'

const props = defineProps({
  mailboxId: { type: String, required: true },
  emailId: { type: String, required: true },
  address: { type: String, default: '' },
})
const emit = defineEmits(['back', 'deleted'])

const theme = useTheme()
const email = ref(null)
const loading = ref(true)
const deleting = ref(false)
const error = ref('')
const view = ref('html')
const remote = ref(false)
const dimPaper = ref(false)
const frame = ref(null)
let ro = null
let lastHeight = 0

const html = computed(() => email.value?.body_html || '')
const text = computed(() => email.value?.body_text || (html.value ? htmlToText(html.value) : ''))
const otp = computed(() => (email.value ? extractOtp({ subject: email.value.subject, text: email.value.body_text, html: html.value }) : null))
const isDark = computed(() => theme.theme === 'dark')
const hasRemote = computed(() =>
  /(?:src|srcset|background|poster)\s*=\s*["']?https?:/i.test(html.value) ||
  /<link[^>]+href\s*=\s*["']?https?:/i.test(html.value) ||
  /url\(\s*['"]?https?:/i.test(html.value),
)

// 以 srcdoc 渲染邮件：禁止脚本，默认阻止远程资源（防追踪像素），链接新窗口打开。
// 强制 html/body 高度为 auto，防止含 height:100% / 100vh 的邮件模板触发 ResizeObserver 无限拉高。
// 信纸底色使用与全局暖灰一致的 #FAF9F7，减轻纯白反差。
const srcdoc = computed(() => {
  if (!html.value) return ''
  const img = remote.value
    ? "img-src data: cid: https: http:; style-src 'unsafe-inline' https: http:; font-src https: http: data:;"
    : "img-src data: cid:; style-src 'unsafe-inline'; font-src data:;"
  const head = `<meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; ${img}"><base target="_blank"><style>html,body{margin:0;padding:16px;height:auto!important;min-height:0!important;font:14px/1.6 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",sans-serif;color:#1C1917;background:#FAF9F7;word-wrap:break-word;overflow-wrap:anywhere}img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}</style>`
  return `<!doctype html><html><head>${head}</head><body>${html.value}</body></html>`
})

async function load() {
  const mid = props.mailboxId
  const eid = props.emailId
  loading.value = true
  error.value = ''
  remote.value = false
  try {
    const res = await api.getEmail(mid, eid)
    if (mid !== props.mailboxId || eid !== props.emailId) return
    email.value = res
    view.value = res.body_html ? 'html' : 'text'
  } catch (e) {
    if (mid !== props.mailboxId || eid !== props.emailId) return
    email.value = null
    error.value = e.status === 404 ? '邮件不存在或已被删除' : e.message
  } finally {
    if (mid === props.mailboxId && eid === props.emailId) loading.value = false
  }
}

function applyHeight(f, doc) {
  const next = Math.max(doc.body.scrollHeight, 240) + 4
  if (Math.abs(next - lastHeight) > 2) {
    lastHeight = next
    f.style.height = `${next}px`
  }
}

function fit() {
  const f = frame.value
  if (!f) return
  try {
    const doc = f.contentDocument
    if (!doc?.body) return
    applyHeight(f, doc)
    if (!ro && window.ResizeObserver) {
      ro = new ResizeObserver(() => {
        try { applyHeight(f, doc) } catch { /* 忽略 */ }
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
  lastHeight = 0
  fit()
  setTimeout(fit, 300)
}

async function remove() {
  if (deleting.value) return
  deleting.value = true
  try {
    await api.deleteEmail(props.mailboxId, props.emailId)
    toast.success('邮件已删除')
    emit('deleted', props.emailId)
  } catch (e) {
    toast.error(`删除失败：${e.message}`)
  } finally {
    deleting.value = false
  }
}

watch(() => [props.mailboxId, props.emailId], load, { immediate: true })
watch(view, async v => { if (v === 'html') { await nextTick(); fit() } })
onBeforeUnmount(() => ro?.disconnect())
</script>

<template>
  <article class="reader" aria-live="polite">
    <div class="reader-bar">
      <button type="button" class="btn btn-ghost btn-sm" @click="emit('back')"><ArrowLeft aria-hidden="true" />返回列表</button>
    </div>

    <div v-if="loading" class="reader-body stack">
      <span class="skel" style="width: 60%; height: 20px" />
      <span class="skel" style="width: 40%" />
      <span class="skel" style="width: 100%; height: 160px" />
    </div>

    <EmptyState v-else-if="error" :icon="FileText" title="无法打开邮件">{{ error }}</EmptyState>

    <div v-else-if="email" class="reader-body">
      <div class="subj-row">
        <h2 class="subj">{{ email.subject || '(无主题)' }}</h2>
        <button
          type="button"
          class="icon-btn danger subj-del"
          aria-label="删除此邮件"
          title="删除此邮件"
          :disabled="deleting"
          @click="remove"
        >
          <Trash2 aria-hidden="true" />
        </button>
      </div>
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

      <div v-if="html" class="body-bar">
        <div class="seg" role="radiogroup" aria-label="显示方式">
          <button type="button" role="radio" :aria-checked="view === 'html' ? 'true' : 'false'" @click="view = 'html'"><Code aria-hidden="true" />HTML</button>
          <button type="button" role="radio" :aria-checked="view === 'text' ? 'true' : 'false'" @click="view = 'text'"><FileText aria-hidden="true" />纯文本</button>
        </div>
        <button v-if="view === 'html' && hasRemote" type="button" class="btn btn-ghost btn-sm" @click="remote = !remote">
          <component :is="remote ? ImageOff : ImageIcon" aria-hidden="true" />{{ remote ? '阻止远程图片' : '显示远程图片' }}
        </button>
        <button
          v-if="view === 'html' && isDark"
          type="button"
          class="btn btn-ghost btn-sm"
          @click="dimPaper = !dimPaper"
        >
          <component :is="dimPaper ? Sun : Moon" aria-hidden="true" />{{ dimPaper ? '浅色信纸' : '护眼暗化' }}
        </button>
      </div>
      <p v-if="view === 'html' && hasRemote && !remote" class="small muted blocked">
        <Paperclip aria-hidden="true" />为保护隐私，已阻止加载远程图片。
      </p>

      <!-- 安全约束：allow-same-origin 仅用于读取 contentDocument.scrollHeight；绝不可添加 allow-scripts，否则邮件 HTML 可读取同源 localStorage 中的 API Key -->
      <iframe
        v-if="view === 'html' && html"
        ref="frame"
        :key="`${emailId}-${remote}`"
        class="mail-frame"
        :class="{ dimmed: isDark && dimPaper }"
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
/* 桌面端不渲染空的返回栏，省出阅读空间并避免单封邮件删除按钮与顶部「删除邮箱」视觉混淆 */
.reader-bar {
  display: none; align-items: center; gap: 8px; padding: 10px 16px;
  border-bottom: 1px solid var(--border); position: sticky; top: 0; background: var(--surface); z-index: 2;
}
.reader-body { padding: 22px 24px 28px; display: grid; gap: 16px; align-content: start; min-width: 0; }
.subj-row { display: flex; align-items: flex-start; gap: 12px; min-width: 0; }
.subj { flex: 1; min-width: 0; font-size: 18px; font-weight: 650; overflow-wrap: anywhere; }
.subj-del { flex: none; margin-top: -2px; }
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
.mail-frame {
  width: 100%; min-height: 240px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: #FAF9F7; transition: filter var(--ease);
}
[data-theme="dark"] .mail-frame { filter: brightness(0.94); }
[data-theme="dark"] .mail-frame.dimmed { filter: invert(0.89) hue-rotate(180deg) brightness(0.95); }
.mail-text {
  margin: 0; padding: 16px; border: 1px solid var(--border); border-radius: var(--r-sm);
  background: color-mix(in oklab, var(--bg) 60%, var(--surface));
  white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--font); font-size: 14px; line-height: 1.7;
}
@media (max-width: 900px) {
  .reader-bar { display: flex; }
  .reader-body { padding: 16px; }
}
</style>
