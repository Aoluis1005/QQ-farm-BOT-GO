<script setup>
import { onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAccountStore } from '@/stores/account'

const app = useAppStore()
const account = useAccountStore()
const router = useRouter()
const route = useRoute()

const navItems = [
  { to: '/', label: '首页', icon: '🏠' },
  { to: '/farm', label: '农场', icon: '🌱' },
  { to: '/bag', label: '背包', icon: '🎒' },
  { to: '/friends', label: '好友', icon: '👫' },
  { to: '/shop', label: '商店', icon: '🛒' },
  { to: '/activity', label: '活动', icon: '🎉' },
  { to: '/illustrated', label: '图鉴', icon: '📖' },
  { to: '/task', label: '任务', icon: '✅' },
  { to: '/settings', label: '设置', icon: '⚙️' },
  { to: '/backend', label: '后台', icon: '🖥️' },
]

const dockItems = navItems.slice(0, 5)

const currentLabel = computed(() => navItems.find((n) => n.to === route.path)?.label || 'QQ 农场')

onMounted(async () => {
  await account.loadAdminStatus()
  if (account.adminLoggedIn) {
    try {
      await account.loadAccounts()
    } catch (e) {
      /* 未登录则跳转登录 */
    }
  }
})

function onSwitch(id) {
  account.switchAccount(id)
  // 切换账号后刷新当前页
  router.go(0)
}
</script>

<template>
  <div class="app-shell" v-if="account.adminLoggedIn">
    <nav class="sb-nav">
      <div class="brand">🌾 QQ 农场</div>
      <button
        v-for="n in navItems"
        :key="n.to"
        class="sb-item"
        :class="{ active: route.path === n.to }"
        @click="router.push(n.to)"
      >
        <span class="mi">{{ n.icon }}</span><span class="lb">{{ n.label }}</span>
      </button>
    </nav>

    <div class="main">
      <header class="topbar">
        <div class="tb-title">{{ currentLabel }}</div>
        <div class="tb-right">
          <select class="acct-select" :value="account.currentId" @change="onSwitch($event.target.value)">
            <option v-for="a in account.accounts" :key="a.id" :value="a.id">
              {{ a.remark || a.name || a.id }}
            </option>
            <option v-if="!account.accounts.length" value="">（无账号）</option>
          </select>
          <button class="icon-btn" @click="app.toggleTheme()" :title="app.theme === 'dark' ? '切浅色' : '切暗色'">
            {{ app.theme === 'dark' ? '🌙' : '☀️' }}
          </button>
        </div>
      </header>

      <main class="content">
        <router-view />
      </main>
    </div>

    <nav class="dock">
      <button
        v-for="n in dockItems"
        :key="n.to"
        class="dock-item"
        :class="{ active: route.path === n.to }"
        @click="router.push(n.to)"
      >
        <span class="mi">{{ n.icon }}</span><span class="lb">{{ n.label }}</span>
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
