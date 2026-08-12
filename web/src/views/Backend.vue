<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const status = ref(null)
const sysConfig = ref(null)

async function load() {
  try {
    const [s, c] = await Promise.all([
      api.get('/api/admin/status').catch(() => null),
      api.get('/api/admin/system-config').catch(() => null),
    ])
    status.value = s?.data?.data || s?.data || null
    sysConfig.value = c?.data?.data || c?.data || null
  } catch (e) {
    app.error('加载后台信息失败：' + (e.response?.data?.error || e.message))
  }
}
onMounted(load)
</script>

<template>
  <div class="bk-wrap">
    <section class="glass panel">
      <div class="sec-head"><h2>🖥️ 后台状态</h2><button class="btn ghost sm" @click="load">刷新</button></div>
      <pre class="json" v-if="status">{{ JSON.stringify(status, null, 2) }}</pre>
      <div v-else class="empty-tip">无数据</div>
    </section>
    <section class="glass panel">
      <div class="sec-head"><h2>🔧 系统配置</h2></div>
      <pre class="json" v-if="sysConfig">{{ JSON.stringify(sysConfig, null, 2) }}</pre>
      <div v-else class="empty-tip">无数据</div>
    </section>
  </div>
</template>

<style scoped>
.bk-wrap {
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
.json {
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--muted);
  margin: 0;
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
