import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api/client'

// 订阅单个邮箱的实时事件（SSE）。连接失败时指数退避重连；
// 后端不支持该接口（旧版本）时进入 fallback，由调用方继续轮询。
//
// status: connecting | live | fallback | closed
export function useMailboxLive(idRef, { onEmail, onMailbox, onExpired, onMissing } = {}) {
  const status = ref('connecting')
  let ctrl = null
  let retry = 0
  let timer = null
  let stopped = false

  function clear() {
    ctrl?.abort()
    ctrl = null
    clearTimeout(timer)
    timer = null
  }

  async function connect(id) {
    clear()
    if (!id || stopped) return
    const mine = new AbortController()
    ctrl = mine
    status.value = 'connecting'
    try {
      await api.mailboxEvents(id, {
        signal: mine.signal,
        onEvent(type, data) {
          if (type === 'ready') {
            retry = 0
            status.value = 'live'
          } else if (type === 'email') onEmail?.(data)
          else if (type === 'mailbox') onMailbox?.(data?.mailbox || data)
          else if (type === 'expired') onExpired?.()
        },
      })
      if (mine.signal.aborted) return
    } catch (e) {
      if (mine.signal.aborted) return
      // 邮箱不存在：停止；接口不存在（旧后端 404 无 error 字段）或不支持：降级为轮询
      if (e.status === 404 && e.data?.error === 'mailbox not found') {
        status.value = 'closed'
        onMissing?.()
        return
      }
      if ([404, 405, 501].includes(e.status)) {
        status.value = 'fallback'
        return
      }
      if (e.status === 401 || e.status === 403) {
        status.value = 'closed'
        return
      }
    }
    if (stopped || mine !== ctrl) return
    // 连接正常结束（服务端重启/代理超时）或网络错误：退避重连，期间按轮询处理
    status.value = 'fallback'
    const delay = Math.min(30000, 1000 * 2 ** Math.min(retry++, 5))
    timer = setTimeout(() => connect(id), delay)
  }

  function onVisible() {
    // 页面重新可见且当前未连接时立即重连，不等退避
    if (document.visibilityState === 'visible' && status.value !== 'live' && status.value !== 'closed') {
      retry = 0
      connect(idRef.value)
    }
  }
  document.addEventListener('visibilitychange', onVisible)

  watch(idRef, id => {
    retry = 0
    connect(id)
  }, { immediate: true })

  onBeforeUnmount(() => {
    stopped = true
    clear()
    document.removeEventListener('visibilitychange', onVisible)
  })

  return { status, reconnect: () => connect(idRef.value) }
}
