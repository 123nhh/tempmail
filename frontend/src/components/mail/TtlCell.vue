<script setup>
import { computed, ref, watch } from 'vue'
import { Clock, TriangleAlert, Infinity as InfinityIcon } from '@lucide/vue'
import { expiryInfo } from '../../utils/format'

const props = defineProps({
  mailbox: { type: Object, required: true },
  now: { type: Number, required: true },
  compact: { type: Boolean, default: false },
})

const info = computed(() => expiryInfo(props.mailbox, props.now))
const icon = computed(() => (info.value.level === 'none' ? InfinityIcon : info.value.level === 'ok' ? Clock : TriangleAlert))
// 续期后到期时间变晚，短暂高亮提示
const renewed = ref(false)
watch(() => props.mailbox.expires_at, (v, old) => {
  if (old && v && new Date(v) > new Date(old)) {
    renewed.value = false
    requestAnimationFrame(() => { renewed.value = true })
    setTimeout(() => { renewed.value = false }, 1000)
  }
})
const suffix = computed(() => (info.value.level === 'bad' && info.value.mins > 0 ? ' · 即将删除' : info.value.level === 'warn' ? ' · 即将过期' : ''))
</script>

<template>
  <div class="ttl" :class="{ compact }">
    <span class="ttl-t" :class="{ 't-warn': info.level === 'warn', 't-bad': info.level === 'bad', 'ttl-renewed': renewed }">
      <component :is="icon" aria-hidden="true" />{{ info.label }}<template v-if="!compact">{{ suffix }}</template>
    </span>
    <div v-if="!compact && info.level !== 'none'" class="bar" :class="info.level" role="presentation">
      <i :style="{ width: `${info.pct}%` }" />
    </div>
  </div>
</template>

<style scoped>
.ttl { display: grid; gap: 5px; min-width: 128px; }
.ttl.compact { min-width: 0; }
.ttl-t { font-size: 12px; display: inline-flex; align-items: center; gap: 4px; white-space: nowrap; font-variant-numeric: tabular-nums; }
.ttl-t svg { width: 13px; height: 13px; flex: none; }
</style>
