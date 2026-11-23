<script setup>
defineProps({
  title: { type: String, required: true },
  sub: { type: String, default: '' },
})
</script>

<template>
  <header class="page-h">
    <div class="titles">
      <slot name="crumb" />
      <h1><slot name="title">{{ title }}</slot></h1>
      <p v-if="sub || $slots.sub"><slot name="sub">{{ sub }}</slot></p>
    </div>
    <div v-if="$slots.actions" class="acts"><slot name="actions" /></div>
  </header>
</template>
