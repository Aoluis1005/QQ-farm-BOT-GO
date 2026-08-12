<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const daily = ref([])
const growth = ref([])
const busy = ref(false)

async function load() {
  try {
    const { data } = await api.get('/api/task/daily')
    daily.value = data.data?.daily || data.data?.dailyTasks || []
    growth.value = data.data?.growth || data.data?.growthTasks || []
  } catch (e) {
    app.error('加载任务失败：' + (e.response?.data?.error || e.message))
  }
}

async function claim(t) {
  busy.value = true
  try {
    const { data } = await api.post('/api/task/claim', { taskId: t.id ?? t.taskId, type: t.type })
    app.success(data.message || '领取成功')
    await load()
  } catch (e) {
    app.error('领取失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}
onMounted(load)
</script>

<template>
  <div class="task-wrap">
    <section class="glass panel">
      <div class="sec-head"><h2>✅ 每日任务</h2><button class="btn ghost sm" @click="load">刷新</button></div>
      <div class="task-list">
        <div v-for="(t, i) in daily" :key="i" class="task-item">
          <div class="t-name">{{ t.name || ('任务' + i) }}</div>
          <div class="t-prog">{{ t.progress ?? '' }} {{ t.reward ? '· 🎟️' + t.reward : '' }}</div>
          <button class="btn primary sm" :disabled="busy || t.claimed" @click="claim({ ...t, type: 'daily' })">
            {{ t.claimed ? '已领' : '领取' }}
          </button>
        </div>
        <div v-if="!daily.length" class="empty-tip">暂无每日任务</div>
      </div>
    </section>

    <section class="glass panel">
      <div class="sec-head"><h2>🌟 成长任务</h2></div>
      <div class="task-list">
        <div v-for="(t, i) in growth" :key="i" class="task-item">
          <div class="t-name">{{ t.name || ('任务' + i) }}</div>
          <div class="t-prog">{{ t.progress ?? '' }}</div>
          <button class="btn primary sm" :disabled="busy || t.claimed" @click="claim({ ...t, type: 'growth' })">
            {{ t.claimed ? '已领' : '领取' }}
          </button>
        </div>
        <div v-if="!growth.length" class="empty-tip">暂无成长任务</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.task-wrap {
  display: grid;
  gap: 16px;
}
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
.task-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.task-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
}
.t-name {
  flex: 1;
  font-size: 14px;
}
.t-prog {
  font-size: 12px;
  color: var(--muted);
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
