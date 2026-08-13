import { createRouter, createWebHistory } from 'vue-router'

// 严格对齐 legacy-index.html 的真实页面结构：
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

export default router
