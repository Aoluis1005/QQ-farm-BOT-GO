import { defineStore } from 'pinia'

let tid = 0

export const useAppStore = defineStore('app', {
  state: () => ({
    toasts: [],
    theme: localStorage.getItem('ui_theme') || 'dark',
  }),
  actions: {
    pushToast(type, message, timeout = 3000) {
      const id = ++tid
      this.toasts.push({ id, type, message })
      setTimeout(() => this.removeToast(id), timeout)
      return id
    },
    removeToast(id) {
      this.toasts = this.toasts.filter((t) => t.id !== id)
    },
    success(m) {
      return this.pushToast('success', m, 3200)
    },
    error(m) {
      // 失败弹窗停留更久（5s），确保用户一定看到
      return this.pushToast('error', m, 5000)
    },
    warning(m) {
      return this.pushToast('warning', m, 5000)
    },
    info(m) {
      return this.pushToast('info', m)
    },
    toggleTheme() {
      this.theme = this.theme === 'dark' ? 'light' : 'dark'
      localStorage.setItem('ui_theme', this.theme)
      document.documentElement.setAttribute('data-theme', this.theme)
    },
  },
})
