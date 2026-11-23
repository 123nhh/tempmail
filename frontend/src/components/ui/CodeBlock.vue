<script setup>
import CopyButton from './CopyButton.vue'

defineProps({
  code: { type: String, required: true },
  lang: { type: String, default: '' },
})
</script>

<template>
  <div class="codeblock">
    <span v-if="lang" class="lang">{{ lang }}</span>
    <CopyButton :text="code" label="复制代码" small />
    <pre><code>{{ code }}</code></pre>
  </div>
</template>

<style scoped>
.codeblock pre code { background: none; border: 0; padding: 0; font-size: inherit; }
</style>
