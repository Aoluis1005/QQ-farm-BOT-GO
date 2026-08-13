import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './style.css'
import './layout.css'
import './legacy.css'

// 主题：沿用现有 HTML 的 data-theme 机制，CSS 已按 [data-theme] 定义全套 oklch 变量。
// 默认暗色（用户指定的视觉），用户若切换过则从 localStorage 恢复。
const THEME_KEY = 'ui_theme'
const saved = localStorage.getItem(THEME_KEY) || 'dark'
document.documentElement.setAttribute('data-theme', saved)

const app = createApp(App)
app.use(createPinia())
app.use(router)

// 全局错误兜底：弹 toast，避免白屏
app.config.errorHandler = (err) => {
  console.error('Vue 错误:', err)
}
app.config.warnHandler = () => {}

app.mount('#app')
