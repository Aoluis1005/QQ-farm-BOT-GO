<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const items = ref([])

async function load() {
  try {
    const { data } = await api.get('/api/bag')
    items.value = data.data?.items || data.data?.list || data.data || []
  } catch (e) {
    app.error('加载背包失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head"><h2>🎒 背包</h2><button class="btn ghost sm" @click="load">刷新</button></div>
    <div class="bag-grid">
      <div v-for="(it, i) in items" :key="i" class="bag-item">
        <div class="bi-name">{{ it.name || it.itemName || ('物品' + i) }}</div>
        <div class="bi-meta">×{{ it.count ?? it.num ?? it.number ?? '?' }}</div>
      </div>
      <div v-if="!items.length" class="empty-tip">背包为空</div>
    </div>
  </section>
</template>

<style scoped>
.panel {
  padding: 18px;
  border-radius: var(--radius-lg);
}
.sec-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.sec-head h2 {
  margin: 0;
  font-size: 16px;
}
.bag-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 12px;
}
.bag-item {
  padding: 12px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
  text-align: center;
}
.bi-name {
  font-size: 13px;
  margin-bottom: 4px;
}
.bi-meta {
  color: var(--primary);
  font-weight: 600;
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
