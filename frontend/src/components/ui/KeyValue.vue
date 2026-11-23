<script setup>
import { computed, ref } from 'vue'
import { Eye, EyeOff } from '@lucide/vue'
import CopyButton from './CopyButton.vue'

const props = defineProps({
  value: { type: String, default: '' },
  secret: { type: Boolean, default: false },
  copyLabel: { type: String, default: '复制' },
})

const shown = ref(!props.secret)
const display = computed(() => {
  if (shown.value || !props.value) return props.value || '—'
  const v = props.value
  return v.length <= 10 ? '•'.repeat(v.length) : `${v.slice(0, 5)}${'•'.repeat(Math.min(v.length - 9, 24))}${v.slice(-4)}`
})
</script>

<template>
  <div class="kv">
    <span class="v" :class="{ masked: !shown }">{{ display }}</span>
    <button
      v-if="secret && value"
      type="button"
      class="icon-btn icon-btn-sm"
      :aria-label="shown ? '隐藏' : '显示'"
      :title="shown ? '隐藏' : '显示'"
      @click="shown = !shown"
    >
      <component :is="shown ? EyeOff : Eye" aria-hidden="true" />
    </button>
    <CopyButton v-if="value" :text="value" :label="copyLabel" small />
  </div>
</template>
