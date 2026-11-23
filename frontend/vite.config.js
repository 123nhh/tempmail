import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// 开发时把后端接口代理到本地 API（默认 8080，可用 VITE_API_TARGET 覆盖）
const target = process.env.VITE_API_TARGET || 'http://localhost:8080'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': target,
      '/public': target,
      '/health': target,
    },
  },
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    chunkSizeWarningLimit: 600,
  },
})
