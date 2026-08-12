<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const logs = ref([])

async function load() {
  try {
    const { data } = await api.get('/api/home/logs')
    logs.value = data.data || []
  } catch (e) {
    app.error('加载事件失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head"><h2>📡 事件日志</h2><button class="btn ghost sm" @click="load">刷新</button></div>
    <div class="log-list">
      <div v-for="(g, i) in logs" :key="i" class="log-item">
        <span class="log-tag">{{ g.tag }}</span>
        <span class="log-msg">{{ g.msg }}</span>
        <span class="log-time">{{ (g.time || '').split(' ')[1] }}</span>
      </div>
      <div v-if="!logs.length" class="empty-tip">暂无事件</div>
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
.log-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 60vh;
  overflow: auto;
}
.log-item {
  display: flex;
  gap: 10px;
  font-size: 13px;
  align-items: baseline;
}
.log-tag {
  flex: none;
  padding: 1px 8px;
  border-radius: 999px;
  background: var(--primary-soft);
  color: var(--primary);
  font-size: 12px;
}
.log-msg {
  flex: 1;
  min-width: 0;
}
.log-time {
  flex: none;
  color: var(--muted);
  font-size: 12px;
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
