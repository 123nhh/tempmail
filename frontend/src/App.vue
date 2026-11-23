<script setup>
import ToastHost from './components/ui/ToastHost.vue'
import ConfirmHost from './components/ui/ConfirmHost.vue'
</script>

<template>
  <a class="skip-link" href="#main">跳到主要内容</a>
  <RouterView />
  <ToastHost />
  <ConfirmHost />
</template>
