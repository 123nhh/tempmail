<script setup>
import CopyButton from '../ui/CopyButton.vue'

defineProps({
  records: { type: Array, default: () => [] },
  compact: { type: Boolean, default: false },
})
</script>

<template>
  <div class="table-wrap dns" :class="{ compact }">
    <table class="tbl">
      <thead>
        <tr><th>类型</th><th>主机记录</th><th>记录值</th><th class="num">优先级</th></tr>
      </thead>
      <tbody>
        <tr v-for="(r, i) in records" :key="i">
          <td><span class="badge b-muted">{{ r.type }}</span></td>
          <td>
            <span class="cell"><code>{{ r.host || '@' }}</code><CopyButton :text="r.host || '@'" label="复制主机记录" small /></span>
          </td>
          <td>
            <span class="cell"><code class="val">{{ r.value }}</code><CopyButton :text="r.value" label="复制记录值" small /></span>
          </td>
          <td class="num">{{ r.priority || '—' }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.dns { border: 1px solid var(--border); border-radius: var(--r-sm); }
.cell { display: inline-flex; align-items: center; gap: 2px; max-width: 100%; }
.cell code { background: none; border: 0; padding: 0; }
/* 记录值（如 SPF）只在空格处换行；表格过窄时横向滚动而不是逐字断开 */
.cell code.val { white-space: normal; }
.compact .tbl td, .compact .tbl th { padding: 7px 10px; }
</style>
