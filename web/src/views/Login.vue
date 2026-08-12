<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAccountStore } from '@/stores/account'
import { useAppStore } from '@/stores/app'

const account = useAccountStore()
const app = useAppStore()
const router = useRouter()
const pwd = ref('')
const loading = ref(false)

async function onSubmit() {
  if (!pwd.value) return
  loading.value = true
  try {
    await account.login(pwd.value)
    await account.loadAccounts()
    app.success('登录成功')
    router.replace('/')
  } catch (e) {
    app.error('登录失败：' + (e.response?.data?.error || e.message))
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
      <p class="login-sub">后台管理登录</p>
      <input
        v-model="pwd"
        type="password"
        class="ipt"
        placeholder="请输入后台密码"
        @keyup.enter="onSubmit"
      />
      <button class="btn primary" :disabled="loading" @click="onSubmit">
        {{ loading ? '登录中…' : '登录' }}
      </button>
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
.login-logo {
  font-size: 48px;
}
.login-card h1 {
  margin: 8px 0 2px;
  font-size: 22px;
}
.login-sub {
  color: var(--muted);
  margin: 0 0 20px;
  font-size: 13px;
}
.ipt {
  width: 100%;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border);
  background: var(--card-strong);
  color: var(--foreground);
  font-size: 15px;
  margin-bottom: 14px;
}
.btn.primary {
  width: 100%;
}
</style>
