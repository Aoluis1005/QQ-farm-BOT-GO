<script setup>
import { onMounted, computed, ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'
import { useAccountStore } from '@/stores/account'

const app = useAppStore()
const account = useAccountStore()
const router = useRouter()
const route = useRoute()



// 底部 dock / 侧栏 6 个 tab：鸿蒙沉浸光感细线 SVG（用户指定，不用 emoji）
const ICON_SHELL =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">'
const tabs = [
  { to: '/', label: '首页', icon: '<path d="M3 10.5 12 4l9 6.5"/><path d="M5 9.5V20h14V9.5"/><path d="M10 20v-6h4v6"/>' },
  { to: '/profile', label: '个人', icon: '<circle cx="12" cy="8" r="4"/><path d="M5 20c0-3.9 3.1-7 7-7s7 3.1 7 7"/>' },
  { to: '/account', label: '账号', icon: '<circle cx="8" cy="8" r="4"/><path d="M11 11l8 8"/><path d="M16 16l2-2"/><path d="M19 19l2-2"/>' },
  { to: '/event', label: '活动', icon: '<path d="M12 3l2.6 5.6L20 9.4l-4 4 1 5.8L12 16.6 7 19l1-5.8-4-4 5.4-.8z"/>' },
  { to: '/shop', label: '商城', icon: '<circle cx="9" cy="20" r="1.4"/><circle cx="18" cy="20" r="1.4"/><path d="M2.5 3h2l2.2 11.2a1.5 1.5 0 0 0 1.5 1.2h8.6a1.5 1.5 0 0 0 1.5-1.2L21 6H6"/>' },
  { to: '/more', label: '更多', icon: '<path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h16"/>' },
]
function iconSvg(inner) {
  return ICON_SHELL + inner + '</svg>'
}

function isActive(t) {
  if (t.to === '/') return route.path === '/'
  return route.path === t.to || route.path.startsWith(t.to + '/')
}

onMounted(async () => {
  // 应用持久化的主题（否则刷新后图标与页面主题不一致；对齐 legacy body data-theme）
  document.documentElement.setAttribute('data-theme', app.theme)
  await account.loadAdminStatus()
  if (account.adminLoggedIn) {
    try {
      await account.loadAccounts()
    } catch (e) {
      /* 未登录则停留在登录页 */
    }
  }
  // 优化1：首屏空闲时预加载各 tab 页面 chunk，切换 tab 即开不卡
  const preload = () => {
    import('@/views/Profile.vue')
    import('@/views/Account.vue')
    import('@/views/Activity.vue')
    import('@/views/Shop.vue')
    import('@/views/More.vue')
  }
  if (typeof requestIdleCallback === 'function') requestIdleCallback(preload)
  else setTimeout(preload, 2500)
})

function go(to) {
  router.push(to)
}
// 右上角切换账号：弹出 bottom sheet，点击账号直接切换当前账号并整页刷新（对齐 legacy）
const showAcc = ref(false)
function onSwitchAccount() {
  showAcc.value = true
}
function pickAccount(id) {
  if (String(id) === String(account.currentId)) { showAcc.value = false; return }
  account.switchAccount(id)
  location.reload()
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
          <span class="di" v-html="iconSvg(t.icon)"></span>{{ t.label }}
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
        <span class="di" v-html="iconSvg(t.icon)"></span>{{ t.label }}
      </button>
    </nav>

    <!-- 切换账号 bottom sheet（对齐 legacy #sheet，点击账号直接切换并刷新） -->
    <div class="sheet-mask" :class="{ show: showAcc }" @click="showAcc = false"></div>
    <div class="sheet" :class="{ show: showAcc }">
      <div class="handle"></div>
      <h3>切换账号</h3>
      <p class="sub">选择要登录的农场账号</p>
      <div style="max-height:52vh;overflow:auto">
        <button class="acc" :class="{ active: String(a.id) === String(account.currentId) }" v-for="a in account.accounts" :key="a.id" @click="pickAccount(a.id)">
          <div class="a-av">🐰</div>
          <div class="a-info"><b>{{ a.name || '未命名' }}</b><span>{{ a.platform || 'qq' }} · {{ ({ online: '在线', offline: '离线' })[a.status] || a.status || '离线' }}</span></div>
          <span class="check">{{ String(a.id) === String(account.currentId) ? '✓' : '' }}</span>
        </button>
        <p v-if="!account.accounts.length" style="font-size:12.5px;color:var(--muted);text-align:center;padding:14px 0">暂无账号</p>
      </div>
      <button class="close" style="margin-top:16px" @click="showAcc = false">关闭</button>
    </div>

    <!-- 全局 toast -->
    <div class="toast-wrap">
      <div v-for="t in app.toasts" :key="t.id" class="toast" :class="'toast-' + t.type">
        {{ t.message }}
      </div>
    </div>
  </div>

  <router-view v-else />
</template>

<style>
/* 所有 bottom sheet 恒在底部 dock( z-index:20 )之上，避免扫码/切换等弹层被 dock 遮住 */
.sheet-mask { z-index: 120 !important; }
.sheet { z-index: 121 !important; }
</style>
