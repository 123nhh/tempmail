import { reactive } from 'vue'

// 全局提示：success / error / warn / info
export const toasts = reactive([])
let seq = 0

// action: { label, run } 可选，在提示右侧显示一个操作按钮（如“续期”）
export function toast(message, type = 'info', timeout = 3200, action = null) {
  const id = ++seq
  toasts.push({ id, message: String(message), type, action })
  if (toasts.length > 4) toasts.shift()
  setTimeout(() => dismiss(id), timeout)
  return id
}

export async function runToastAction(t) {
  dismiss(t.id)
  try { await t.action?.run?.() } catch { /* 操作自己负责提示 */ }
}

export function dismiss(id) {
  const i = toasts.findIndex(t => t.id === id)
  if (i >= 0) toasts.splice(i, 1)
}

toast.success = m => toast(m, 'success')
toast.error = m => toast(m, 'error', 5000)
toast.warn = (m, action = null) => toast(m, 'warn', action ? 10000 : 4200, action)
toast.info = m => toast(m, 'info')

// 确认弹窗：返回 Promise<boolean>
export const confirmState = reactive({ open: false, title: '', message: '', detail: '', confirmText: '确认', danger: false, resolve: null })

export function confirm({ title = '确认操作', message = '', detail = '', confirmText = '确认', danger = false } = {}) {
  if (confirmState.resolve) confirmState.resolve(false)
  return new Promise(resolve => {
    Object.assign(confirmState, { open: true, title, message, detail, confirmText, danger, resolve })
  })
}

export function settleConfirm(ok) {
  const r = confirmState.resolve
  confirmState.open = false
  confirmState.resolve = null
  if (r) r(ok)
}

// 复制到剪贴板（非安全上下文下降级为 execCommand）
export async function copyText(text, label = '已复制到剪贴板') {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
    } else {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.setAttribute('readonly', '')
      ta.style.cssText = 'position:fixed;top:-1000px;left:-1000px'
      document.body.appendChild(ta)
      ta.select()
      ta.setSelectionRange(0, ta.value.length)
      const ok = document.execCommand('copy')
      ta.remove()
      if (!ok) throw new Error('copy failed')
    }
    toast.success(label)
    return true
  } catch {
    toast.warn('复制失败，请手动选择')
    return false
  }
}
