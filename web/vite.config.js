import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// 构建产物输出到 web/dist，由 Go 后端 //go:embed 嵌入二进制。
// base 用绝对路径 '/'：本服务始终部署在根路径(:3009/)，
// 绝对 base 才能让 Vue Router 的 createWebHistory() 正确匹配路由（相对 base './' 会导致 <router-view> 全空→黑屏）。
export default defineConfig({
  plugins: [vue()],
  base: '/',
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      // 本地开发时把 /api 与 /game-config 代理到部署服务器，便于直连 Go 后端调试
      '/api': {
        target: 'http://111.229.128.163:3009',
        changeOrigin: true,
      },
      '/game-config': {
        target: 'http://111.229.128.163:3009',
        changeOrigin: true,
      },
    },
  },
})
