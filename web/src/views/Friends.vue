<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const friends = ref([])

async function load() {
  try {
    const { data } = await api.get('/api/friends/list')
    friends.value = data.data?.list || data.data?.friends || data.data || []
  } catch (e) {
    app.error('加载好友失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="sec-head"><h2>👫 好友</h2><button class="btn ghost sm" @click="load">刷新</button></div>
    <div class="friend-grid">
      <div v-for="(f, i) in friends" :key="i" class="friend-card">
        <div class="f-av">👤</div>
        <div class="f-name">{{ f.name || f.nickName || ('好友' + i) }}</div>
        <div class="f-sub">{{ f.uid || f.uin || '' }}</div>
      </div>
      <div v-if="!friends.length" class="empty-tip">暂无好友</div>
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
.friend-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 12px;
}
.friend-card {
  padding: 14px 10px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
  text-align: center;
}
.f-av {
  font-size: 30px;
}
.f-name {
  font-size: 13px;
  margin-top: 4px;
  font-weight: 600;
}
.f-sub {
  font-size: 11px;
  color: var(--muted);
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
