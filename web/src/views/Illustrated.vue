<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const items = ref([])
const busy = ref(false)

async function load() {
  try {
    const { data } = await api.get('/api/illustrated')
    items.value = data.data?.list || data.data?.items || data.data || []
  } catch (e) {
    app.error('加载图鉴失败：' + (e.response?.data?.error || e.message))
  }
}

async function buyAll() {
  busy.value = true
  try {
    const { data } = await api.post('/api/illustrated/buy-all', {})
    app.success(data.message || '一键购买完成')
    await load()
  } catch (e) {
    app.error('购买失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head">
      <h2>📖 图鉴</h2>
      <div class="acts">
        <button class="btn ghost sm" @click="load">刷新</button>
        <button class="btn primary sm" :disabled="busy" @click="buyAll">一键购买</button>
      </div>
    </div>
    <div class="ill-grid">
      <div v-for="(it, i) in items" :key="i" class="ill-item">
        <div class="ill-name">{{ it.name || it.itemName || ('条目' + i) }}</div>
        <div class="ill-meta">{{ it.owned ? '✅ 已集' : '⬜ 未集' }}</div>
      </div>
      <div v-if="!items.length" class="empty-tip">暂无图鉴数据</div>
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
.acts {
  display: flex;
  gap: 8px;
}
.ill-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 12px;
}
.ill-item {
  padding: 12px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
  text-align: center;
}
.ill-name {
  font-size: 13px;
  font-weight: 600;
}
.ill-meta {
  font-size: 12px;
  color: var(--muted);
  margin-top: 4px;
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
