<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const lands = ref([])
const busy = ref(false)

async function load() {
  try {
    const { data } = await api.get('/api/farm/lands')
    lands.value = data.data || []
  } catch (e) {
    app.error('加载农场失败：' + (e.response?.data?.error || e.message))
  }
}

async function harvest() {
  busy.value = true
  try {
    const { data } = await api.post('/api/farm/harvest', {})
    app.success(data.message || '收获完成')
    await load()
  } catch (e) {
    app.error('收获失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

function statusText(s) {
  return { locked: '🔒 未解锁', empty: '🟫 空地', growing: '🌿 生长中', ripe: '🍎 可收获' }[s] || s
}

onMounted(load)
</script>

<template>
  <div class="farm">
    <section class="glass panel">
      <div class="sec-head">
        <h2>🌱 农场地块</h2>
        <div class="acts">
          <button class="btn ghost sm" @click="load">刷新</button>
          <button class="btn primary sm" :disabled="busy" @click="harvest">一键收获</button>
        </div>
      </div>
      <div class="land-grid">
        <div v-for="ld in lands" :key="ld.id" class="land">
          <div class="land-top">
            <span>{{ ld.landTypeName || '地块' }}</span><span>Lv.{{ ld.level }}</span>
          </div>
          <div class="land-mid">{{ statusText(ld.status) }}</div>
          <div class="land-plant" v-if="ld.plantName">{{ ld.plantName }} · {{ ld.phaseName }}</div>
        </div>
        <div v-if="!lands.length" class="empty-tip">暂无地块数据</div>
      </div>
    </section>
  </div>
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
.land-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
}
.land {
  padding: 12px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
}
.land-top {
  display: flex;
  justify-content: space-between;
  font-size: 13px;
  color: var(--muted);
}
.land-mid {
  margin: 8px 0 4px;
  font-weight: 600;
}
.land-plant {
  font-size: 12px;
  color: var(--muted);
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
}
</style>
