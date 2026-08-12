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
      return this.pushToast('success', m)
    },
    error(m) {
      return this.pushToast('error', m)
    },
    warning(m) {
      return this.pushToast('warning', m)
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
