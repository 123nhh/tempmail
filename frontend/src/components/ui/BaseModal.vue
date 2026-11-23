<script setup>
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { X } from '@lucide/vue'

const props = defineProps({
  open: { type: Boolean, default: false },
  title: { type: String, default: '' },
  size: { type: String, default: '' },
  icon: { type: [Object, Function], default: null },
  dismissible: { type: Boolean, default: true },
})
const emit = defineEmits(['close'])

const panel = ref(null)
let lastFocus = null
const titleId = `m-${Math.random().toString(36).slice(2, 8)}`

function close() {
  if (props.dismissible) emit('close')
}

function onKey(e) {
  if (e.key === 'Escape') close()
  if (e.key !== 'Tab' || !panel.value) return
  // 焦点限制在弹窗内
  const f = panel.value.querySelectorAll('button:not([disabled]),[href],input:not([disabled]),select,textarea,[tabindex]:not([tabindex="-1"])')
  if (!f.length) return
  const first = f[0]
  const last = f[f.length - 1]
  if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus() }
  else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus() }
}

watch(
  () => props.open,
  async v => {
    if (v) {
      lastFocus = document.activeElement
      document.addEventListener('keydown', onKey)
      document.body.style.overflow = 'hidden'
      await nextTick()
      const auto = panel.value?.querySelector('[autofocus],input:not([type=hidden]),select,textarea') || panel.value?.querySelector('.modal-f .btn-primary')
      ;(auto || panel.value)?.focus()
    } else {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
      lastFocus?.focus?.()
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey)
  document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body">
    <Transition name="modal">
      <div v-if="open" class="modal-overlay" @mousedown.self="close">
        <div ref="panel" class="modal" :class="size" role="dialog" aria-modal="true" :aria-labelledby="titleId" tabindex="-1">
          <div class="modal-h">
            <h2 :id="titleId"><component :is="icon" v-if="icon" aria-hidden="true" />{{ title }}</h2>
            <button v-if="dismissible" type="button" class="icon-btn" aria-label="关闭" @click="close"><X /></button>
          </div>
          <div class="modal-b"><slot /></div>
          <div v-if="$slots.footer" class="modal-f"><slot name="footer" /></div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>
