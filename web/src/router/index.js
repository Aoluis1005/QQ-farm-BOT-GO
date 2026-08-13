import { createRouter, createWebHistory } from 'vue-router'

// 路由严格对齐原 HTML 的 6 个页面（page-home/profile/account/event/shop/more），不再自行发明 tab。
const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue') },
  { path: '/', name: 'home', component: () => import('@/views/Dashboard.vue') },
  { path: '/profile', name: 'profile', component: () => import('@/views/Profile.vue') },
  { path: '/account', name: 'account', component: () => import('@/views/Account.vue') },
  { path: '/event', name: 'event', component: () => import('@/views/Activity.vue') },
  { path: '/shop', name: 'shop', component: () => import('@/views/Shop.vue') },
  { path: '/friends', name: 'friends', component: () => import('@/views/Friends.vue') },
  { path: '/bag', name: 'bag', component: () => import('@/views/Bag.vue') },
  { path: '/backend', name: 'backend', component: () => import('@/views/Backend.vue') },
  { path: '/more', name: 'more', component: () => import('@/views/More.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory('/'),
  routes,
})

export default router
