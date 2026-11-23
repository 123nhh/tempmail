<script setup>
import { ref } from 'vue'
import { Copy, Check } from '@lucide/vue'
import { copyText } from '../../composables/feedback'

const props = defineProps({
  text: { type: String, required: true },
  label: { type: String, default: '复制' },
  toastText: { type: String, default: '已复制到剪贴板' },
  small: { type: Boolean, default: false },
  showLabel: { type: Boolean, default: false },
})

const done = ref(false)
async function copy() {
  if (await copyText(props.text, props.toastText)) {
    done.value = true
    setTimeout(() => { done.value = false }, 1400)
  }
}
</script>

<template>
  <button
    v-if="showLabel"
    type="button"
    class="btn btn-secondary"
    :class="{ 'btn-sm': small, copied: done }"
    @click.stop="copy"
  >
    <component :is="done ? Check : Copy" aria-hidden="true" />{{ done ? '已复制' : label }}
  </button>
  <button
    v-else
    type="button"
    class="icon-btn copy"
    :class="{ 'icon-btn-sm': small, copied: done }"
    :aria-label="label"
    :title="label"
    @click.stop="copy"
  >
    <component :is="done ? Check : Copy" aria-hidden="true" />
  </button>
</template>
