<script setup>
defineProps({
  icon: { type: [Object, Function], default: null },
  title: { type: String, default: '' },
})
</script>

<template>
  <div class="empty">
    <span v-if="icon" class="ico"><component :is="icon" aria-hidden="true" /></span>
    <strong v-if="title">{{ title }}</strong>
    <p v-if="$slots.default"><slot /></p>
    <div v-if="$slots.actions" class="row" style="justify-content: center; margin-top: 6px"><slot name="actions" /></div>
  </div>
</template>
