<script setup>
import { onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAccountStore } from '@/stores/account'

const app = useAppStore()
const account = useAccountStore()
const router = useRouter()
const route = useRoute()

// 底部 dock / 侧栏 6 个 tab（用户要求：图标统一用「更多」风格的 ☰ 符号）
const tabs = [
  { to: '/', label: '首页', icon: '☰' },
  { to: '/profile', label: '个人', icon: '☰' },
  { to: '/account', label: '账号', icon: '☰' },
  { to: '/event', label: '活动', icon: '☰' },
  { to: '/shop', label: '商城', icon: '☰' },
  { to: '/more', label: '更多', icon: '☰' },
]

function isActive(t) {
  if (t.to === '/') return route.path === '/'
  return route.path === t.to || route.path.startsWith(t.to + '/')
}

onMounted(async () => {
  await account.loadAdminStatus()
  if (account.adminLoggedIn) {
    try {
      await account.loadAccounts()
    } catch (e) {
      /* 未登录则停留在登录页 */
    }
  }
})

function go(to) {
  router.push(to)
}
function onSwitchAccount() {
  router.push('/account')
}
</script>

<template>
  <div v-if="account.adminLoggedIn">
    <!-- 左侧栏（桌面 ≥920px 显示，移动端由 style.css 隐藏） -->
    <aside class="sidebar">
      <div class="sb-brand"><span class="logo">🌾</span> QQ农场 Bot</div>
      <nav class="sb-nav">
        <button
          v-for="t in tabs"
          :key="t.to"
          class="nav-btn"
          :class="{ active: isActive(t) }"
          @click="go(t.to)"
        >
          <span class="di">{{ t.icon }}</span>{{ t.label }}
        </button>
      </nav>
    </aside>

    <!-- 主区 -->
    <div class="app">
      <header class="topbar">
        <div class="brand">
          <span class="logo">🌾</span>
          <span>QQ农场 Bot<small>鸿蒙光感 · 原型</small></span>
        </div>
        <div class="icon-group">
          <button class="icon-btn" @click="app.toggleTheme()" :title="app.theme === 'dark' ? '切浅色' : '切暗色'">
            {{ app.theme === 'dark' ? '🌙' : '☀️' }}
          </button>
          <button class="icon-btn" @click="onSwitchAccount" title="切换账号">🪪</button>
        </div>
      </header>

      <router-view />
    </div>

    <!-- 底部 dock（移动端 <920px 显示，沿用 HTML 原 .dock 居中悬浮药丸样式） -->
    <nav class="dock">
      <button
        v-for="t in tabs"
        :key="t.to"
        :class="{ active: isActive(t) }"
        @click="go(t.to)"
      >
        <span class="di">{{ t.icon }}</span>{{ t.label }}
      </button>
    </nav>

    <!-- 全局 toast -->
    <div class="toast-wrap">
      <div v-for="t in app.toasts" :key="t.id" class="toast" :class="'toast-' + t.type">
        {{ t.message }}
      </div>
    </div>
  </div>

  <router-view v-else />
</template>
