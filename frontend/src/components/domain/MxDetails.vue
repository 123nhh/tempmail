<script setup>
import { CircleCheck, CircleX } from '@lucide/vue'
import { mxKindLabel } from '../../utils/dns'

defineProps({ details: { type: Array, default: () => [] } })
</script>

<template>
  <ul v-if="details && details.length" class="mxd">
    <li v-for="(d, i) in details" :key="i" :class="d.matched ? 't-ok' : 't-bad'">
      <component :is="d.matched ? CircleCheck : CircleX" aria-hidden="true" />
      <span class="k">{{ mxKindLabel(d.kind) }}</span>
      <code>{{ d.name }}</code>
      <span class="s">{{ d.matched ? '已生效' : '未生效' }}<template v-if="d.status"> · {{ d.status }}</template></span>
    </li>
  </ul>
</template>

<style scoped>
.mxd { list-style: none; padding: 0; display: grid; gap: 4px; font-size: 12px; }
.mxd li { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.mxd svg { width: 14px; height: 14px; flex: none; }
.mxd .k { font-weight: 600; }
.mxd code { color: var(--text); }
.mxd .s { color: var(--text-3); }
</style>
