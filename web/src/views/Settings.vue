<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const automation = ref({})
const keys = [
  { k: 'farm', label: '🌾 自动巡田' },
  { k: 'friend_steal', label: '🥷 自动偷菜' },
  { k: 'friend_help', label: '🤝 自动帮忙' },
  { k: 'friend_turbo', label: '⚡ 好友加速' },
  { k: 'task', label: '✅ 自动任务' },
  { k: 'land_upgrade', label: '⬆️ 自动升级' },
  { k: 'sell', label: '💱 自动卖果' },
]

async function load() {
  try {
    const { data } = await api.get('/api/settings')
    automation.value = data.data?.automation || data.data?.auto || {}
  } catch (e) {
    app.error('加载设置失败：' + (e.response?.data?.error || e.message))
  }
}

async function toggle(k, val) {
  try {
    await api.post('/api/automation', { key: k, enabled: val })
    app.success('已保存')
  } catch (e) {
    app.error('保存失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head"><h2>⚙️ 自动化设置</h2><button class="btn ghost sm" @click="load">刷新</button></div>
    <div class="set-list">
      <label v-for="x in keys" :key="x.k" class="set-row">
        <span>{{ x.label }}</span>
        <input
          type="checkbox"
          :checked="!!automation[x.k]"
          @change="toggle(x.k, $event.target.checked)"
        />
      </label>
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
.set-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.set-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 14px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
  font-size: 14px;
  cursor: pointer;
}
</style>
