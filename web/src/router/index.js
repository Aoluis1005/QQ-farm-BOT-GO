import { createRouter, createWebHistory } from 'vue-router'

const routes = [
  { path: '/login', name: 'login', component: () => import('@/views/Login.vue'), meta: { public: true } },
  { path: '/', name: 'dashboard', component: () => import('@/views/Dashboard.vue') },
  { path: '/farm', name: 'farm', component: () => import('@/views/Farm.vue') },
  { path: '/bag', name: 'bag', component: () => import('@/views/Bag.vue') },
  { path: '/friends', name: 'friends', component: () => import('@/views/Friends.vue') },
  { path: '/shop', name: 'shop', component: () => import('@/views/Shop.vue') },
  { path: '/activity', name: 'activity', component: () => import('@/views/Activity.vue') },
  { path: '/illustrated', name: 'illustrated', component: () => import('@/views/Illustrated.vue') },
  { path: '/task', name: 'task', component: () => import('@/views/Task.vue') },
  { path: '/settings', name: 'settings', component: () => import('@/views/Settings.vue') },
  { path: '/backend', name: 'backend', component: () => import('@/views/Backend.vue') },
  { path: '/event', name: 'event', component: () => import('@/views/Event.vue') },
  { path: '/:pathMatch(.*)*', redirect: '/' },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

export default router
