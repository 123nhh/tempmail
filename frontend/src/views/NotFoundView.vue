<script setup>
import { Compass } from '@lucide/vue'
import EmptyState from '../components/ui/EmptyState.vue'
</script>

<template>
  <section class="card">
    <EmptyState :icon="Compass" title="页面不存在">
      你访问的地址不存在或已被移动。
      <template #actions>
        <RouterLink :to="{ name: 'dashboard' }" class="btn btn-primary btn-sm">返回看板</RouterLink>
      </template>
    </EmptyState>
  </section>
</template>
