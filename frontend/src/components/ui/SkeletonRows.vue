<script setup>
defineProps({
  rows: { type: Number, default: 4 },
  cols: { type: Number, default: 4 },
})
</script>

<template>
  <div class="skel-rows" aria-busy="true" aria-label="加载中">
    <div v-for="r in rows" :key="r" class="skel-row">
      <span v-for="c in cols" :key="c" class="skel" :style="{ width: c === 1 ? '38%' : `${14 + ((r * c) % 4) * 6}%` }" />
    </div>
  </div>
</template>

<style scoped>
.skel-rows { display: grid; }
.skel-row { display: flex; gap: 24px; align-items: center; padding: 16px 16px; border-bottom: 1px solid var(--border); }
.skel-row:last-child { border-bottom: 0; }
</style>
