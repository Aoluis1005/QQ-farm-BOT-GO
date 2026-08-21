<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import api from '@/api'
import { useAccountStore } from '@/stores/account'
import { useAppStore } from '@/stores/app'

const account = useAccountStore()
const app = useAppStore()
const router = useRouter()
const pwd = ref('')
const loading = ref(false)
const hasPwd = ref(true)   // 是否已设置后台密码

// 无 token 先探测 status，决定走 设密(setup) 还是 登录(login)
onMounted(async () => {
  try {
    const { data } = await api.get('/api/admin/status')
    hasPwd.value = !!data.hasPassword
  } catch (e) { /* status 不可达则默认按有密码处理 */ }
})

const title = computed(() => hasPwd.value ? '后台管理登录' : '首次运行 · 设置后台密码')
const sub = computed(() => hasPwd.value ? '请输入后台密码' : '尚未设置管理密码，请先设置并登录')
const btnTxt = computed(() => {
  if (loading.value) return hasPwd.value ? '登录中…' : '设置中…'
  return hasPwd.value ? '登录' : '设置密码并登录'
})

async function onSubmit() {
  if (!pwd.value) return
  loading.value = true
  try {
    if (!hasPwd.value) {
      // 首次设密：先 setup 再 login
      const { data: sd } = await api.post('/api/admin/setup', { password: pwd.value })
      if (!(sd && sd.ok)) { app.error((sd && sd.error) || '设置失败'); return }
    }
    await account.login(pwd.value)
    await account.loadAccounts()
    app.success('登录成功')
    router.replace('/')
  } catch (e) {
    app.error(e.response?.data?.error || '登录失败')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-screen">
    <div class="login-card glass">
      <div class="login-logo">🌾</div>
      <h1>QQ 农场</h1>
      <p class="login-sub">{{ sub }}</p>
      <input
        v-model="pwd"
        type="password"
        class="ipt"
        :placeholder="hasPwd ? '请输入后台密码' : '设置后台密码（至少 6 位）'"
        @keyup.enter="onSubmit"
      />
      <button class="btn primary" :disabled="loading" @click="onSubmit">{{ btnTxt }}</button>
    </div>
  </div>
</template>

<style scoped>
.login-screen {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 24px;
}
.login-card {
  width: 100%;
  max-width: 360px;
  padding: 32px 28px;
  border-radius: var(--radius-lg);
  text-align: center;
}
.login-card .login-sub { color: var(--muted); font-size: 13px; margin: -4px 0 20px; }
.ipt {
  width: 100%;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: var(--card-strong);
  color: var(--foreground);
  font-size: 15px;
  margin-bottom: 14px;
  box-sizing: border-box;
}
.btn.primary {
  width: 100%;
  height: 46px;
  font-size: 15px;
  font-weight: 700;
  border-radius: 16px;
  display: flex;
  align-items: center;
  justify-content: center;
}
</style>
