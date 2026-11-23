<script setup>
import { CircleCheck, CircleAlert, TriangleAlert, Info, X } from '@lucide/vue'
import { toasts, dismiss, runToastAction } from '../../composables/feedback'

const icons = { success: CircleCheck, error: CircleAlert, warn: TriangleAlert, info: Info }
</script>

<template>
  <div class="toasts" role="status" aria-live="polite">
    <TransitionGroup name="toast">
      <div v-for="t in toasts" :key="t.id" class="toast" :class="t.type">
        <component :is="icons[t.type] || Info" aria-hidden="true" />
        <span>{{ t.message }}</span>
        <button v-if="t.action" type="button" class="toast-act" @click="runToastAction(t)">{{ t.action.label }}</button>
        <button type="button" aria-label="关闭提示" @click="dismiss(t.id)"><X /></button>
      </div>
    </TransitionGroup>
  </div>
</template>
