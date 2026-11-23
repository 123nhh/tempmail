<script setup>
defineProps({
  modelValue: { type: Boolean, default: false },
  label: { type: String, required: true },
  disabled: { type: Boolean, default: false },
})
defineEmits(['update:modelValue'])
</script>

<template>
  <button
    type="button"
    class="switch"
    role="switch"
    :aria-checked="modelValue ? 'true' : 'false'"
    :aria-label="label"
    :disabled="disabled"
    @click="$emit('update:modelValue', !modelValue)"
  />
</template>
