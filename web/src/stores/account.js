import { defineStore } from 'pinia'
import api, { setAccountId, getAccountId, setToken, getToken } from '@/api'

export const useAccountStore = defineStore('account', {
  state: () => ({
    accounts: [],
    currentId: getAccountId(),
    adminLoggedIn: !!getToken(),
    hasPassword: true,
  }),
  getters: {
    current(state) {
      return state.accounts.find((a) => String(a.id) === String(state.currentId)) || null
    },
  },
  actions: {
    async loadAdminStatus() {
      try {
        const { data } = await api.get('/api/admin/status')
        this.hasPassword = !!data.hasPassword
        this.adminLoggedIn = !!getToken()
        return data
      } catch (e) {
        return null
      }
    },
    async login(password) {
      const { data } = await api.post('/api/admin/login', { password })
      if (data.token) {
        setToken(data.token)
        this.adminLoggedIn = true
      }
      return data
    },
    logout() {
      setToken('')
      this.adminLoggedIn = false
    },
    async loadAccounts() {
      const { data } = await api.get('/api/accounts')
      this.accounts = data.accounts || data.list || []
      // 若当前无选中账号，默认选中首个
      if (!this.currentId && this.accounts.length) {
        this.switchAccount(this.accounts[0].id)
      }
      return this.accounts
    },
    switchAccount(id) {
      this.currentId = String(id)
      setAccountId(String(id))
    },
  },
})
