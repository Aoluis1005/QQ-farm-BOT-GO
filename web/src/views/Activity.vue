<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const items = ref([])

async function load() {
  try {
    const { data } = await api.get('/api/activity/list')
    items.value = data.data?.list || data.data?.items || data.data || []
  } catch (e) {
    app.error('加载活动失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head"><h2>🎉 活动中心</h2><button class="btn ghost sm" @click="load">刷新</button></div>
    <div class="act-list">
      <div v-for="(a, i) in items" :key="i" class="act-item">
        <div class="act-name">{{ a.name || a.title || ('活动' + i) }}</div>
        <div class="act-desc">{{ a.desc || a.description || '' }}</div>
      </div>
      <div v-if="!items.length" class="empty-tip">暂无进行中的活动</div>
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
.act-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.act-item {
  padding: 14px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
}
.act-name {
  font-weight: 700;
  margin-bottom: 4px;
}
.act-desc {
  font-size: 13px;
  color: var(--muted);
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
