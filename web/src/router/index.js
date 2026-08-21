import { createRouter, createWebHistory } from 'vue-router'
import { useAccountStore } from '@/stores/account'

// 严格
//   dock 6 主 tab：首页/个人/账号/活动/商城/更多
//   more → 子页面：设置(page-settings)、后台(page-backend)
// bot 框架凭空发明的 tab（后端无接口）一律不建路由。
const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue') },
  { path: '/', name: 'home', component: () => import('@/views/Dashboard.vue') },
  { path: '/profile', name: 'profile', component: () => import('@/views/Profile.vue') },
  { path: '/account', name: 'account', component: () => import('@/views/Account.vue') },
  { path: '/event', name: 'event', component: () => import('@/views/Activity.vue') },
  { path: '/shop', name: 'shop', component: () => import('@/views/Shop.vue') },
  { path: '/more', name: 'more', component: () => import('@/views/More.vue') },
  { path: '/settings', name: 'settings', component: () => import('@/views/Settings.vue') },
  { path: '/backend', name: 'backend', component: () => import('@/views/Backend.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory('/'),
  routes,
})

// 鉴权守卫：未登录（本地无 token）且目标非 /login 时，重定向到登录/设置密码页。
// status.hasPassword ? 登录 : 设置密码。避免未登录时渲染无导航栏的空壳首页。
router.beforeEach((to) => {
  const account = useAccountStore()
  if (!account.adminLoggedIn && to.path !== '/login') {
    return { path: '/login' }
  }
  return true
})

export default router
