import { onBeforeUnmount, onMounted, ref } from 'vue'

// 定时轮询：页面隐藏时暂停，重新可见时立即补一次
export function usePoller(fn, interval, { immediate = false } = {}) {
  let timer = null
  let running = false
  const busy = ref(false)
  const lastRun = ref(0)

  async function tick() {
    if (running) return
    running = true
    busy.value = true
    try {
      await fn()
      lastRun.value = Date.now()
    } catch {
      /* 轮询失败静默处理，下次重试 */
    } finally {
      running = false
      busy.value = false
    }
  }

  function start() {
    stop()
    if (document.visibilityState === 'hidden') return
    timer = setInterval(tick, interval)
  }

  function stop() {
    if (timer) clearInterval(timer)
    timer = null
  }

  function onVisibility() {
    if (document.visibilityState === 'hidden') stop()
    else {
      tick()
      start()
    }
  }

  onMounted(() => {
    document.addEventListener('visibilitychange', onVisibility)
    if (immediate) tick()
    start()
  })
  onBeforeUnmount(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisibility)
  })

  return { busy, lastRun, tick, start, stop }
}

// 每隔一段时间刷新的“当前时间”，用于倒计时显示
export function useNow(interval = 30000) {
  const now = ref(Date.now())
  let t = null
  onMounted(() => { t = setInterval(() => { now.value = Date.now() }, interval) })
  onBeforeUnmount(() => clearInterval(t))
  return now
}
