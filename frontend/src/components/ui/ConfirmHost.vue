<script setup>
import { ref } from 'vue'
import { TriangleAlert } from '@lucide/vue'
import BaseModal from './BaseModal.vue'
import { confirmState, settleConfirm } from '../../composables/feedback'

const busy = ref(false)

function ok() {
  busy.value = false
  settleConfirm(true)
}
</script>

<template>
  <BaseModal
    :open="confirmState.open"
    :title="confirmState.title"
    :icon="confirmState.danger ? TriangleAlert : null"
    @close="settleConfirm(false)"
  >
    <p>{{ confirmState.message }}</p>
    <p v-if="confirmState.detail" class="small text-2">{{ confirmState.detail }}</p>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="settleConfirm(false)">取消</button>
      <button type="button" class="btn" :class="confirmState.danger ? 'btn-danger' : 'btn-primary'" :disabled="busy" @click="ok">
        {{ confirmState.confirmText }}
      </button>
    </template>
  </BaseModal>
</template>
