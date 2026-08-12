<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const profile = ref(null)
const lands = ref([])
const logs = ref([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const [p, l, lg] = await Promise.all([
      api.get('/api/home/profile'),
      api.get('/api/farm/lands').catch(() => null),
      api.get('/api/home/logs').catch(() => null),
    ])
    profile.value = p.data.data
    lands.value = l?.data?.data || []
    logs.value = lg?.data?.data || []
  } catch (e) {
    app.error('加载首页失败：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

function statusText(s) {
  return { locked: '🔒 未解锁', empty: '🟫 空地', growing: '🌿 生长中', ripe: '🍎 可收获' }[s] || s
}
function statusClass(s) {
  return { locked: 'st-locked', empty: 'st-empty', growing: 'st-grow', ripe: 'st-ripe' }[s] || ''
}

onMounted(load)
</script>

<template>
  <div class="dash">
    <section class="profile-card glass" v-if="profile">
      <div class="avatar">{{ profile.avatar ? '' : '🌾' }}</div>
      <div class="pinfo">
        <div class="pname">
          {{ profile.name || '未命名' }}
          <span class="pconn" :class="profile.connected ? 'on' : 'off'">
            {{ profile.connected ? '已连接' : '未连接' }}
          </span>
        </div>
        <div class="puid">UID: {{ profile.uid || '-' }} · Lv.{{ profile.level || 0 }}</div>
        <div class="pstats">
          <span>💰 {{ profile.gold }}</span>
          <span>🎟️ {{ profile.coupons }}</span>
          <span>🫘 {{ profile.goldenBeans }}</span>
        </div>
        <div class="expbar" v-if="profile.expMax">
          <div class="expfill" :style="{ width: (profile.expPercent || 0) + '%' }"></div>
        </div>
      </div>
    </section>

    <section class="lands glass">
      <div class="sec-head">
        <h2>🌱 我的农场</h2>
        <button class="btn ghost sm" @click="load" :disabled="loading">刷新</button>
      </div>
      <div class="land-grid">
        <div v-for="ld in lands" :key="ld.id" class="land" :class="statusClass(ld.status)">
          <div class="land-top">
            <span class="land-name">{{ ld.landTypeName || '地块' }}</span>
            <span class="land-lv">Lv.{{ ld.level }}</span>
          </div>
          <div class="land-mid">{{ statusText(ld.status) }}</div>
          <div class="land-plant" v-if="ld.plantName">{{ ld.plantName }} · {{ ld.phaseName }}</div>
        </div>
        <div v-if="!lands.length" class="land empty-tip">暂无地块数据</div>
      </div>
    </section>

    <section class="logs glass">
      <div class="sec-head"><h2>📜 操作日志</h2></div>
      <div class="log-list">
        <div v-for="(g, i) in logs" :key="i" class="log-item">
          <span class="log-tag">{{ g.tag }}</span>
          <span class="log-msg">{{ g.msg }}</span>
          <span class="log-time">{{ (g.time || '').split(' ')[1] }}</span>
        </div>
        <div v-if="!logs.length" class="empty-tip">暂无日志</div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.dash {
  display: grid;
  gap: 16px;
}
.profile-card {
  display: flex;
  gap: 16px;
  padding: 18px;
  border-radius: var(--radius-lg);
}
.avatar {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: var(--primary-soft);
  display: grid;
  place-items: center;
  font-size: 32px;
  flex: none;
}
.pinfo {
  flex: 1;
  min-width: 0;
}
.pname {
  font-size: 18px;
  font-weight: 700;
  display: flex;
  align-items: center;
  gap: 8px;
}
.pconn {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 999px;
}
.pconn.on {
  background: color-mix(in oklch, var(--good) 25%, transparent);
  color: var(--good);
}
.pconn.off {
  background: color-mix(in oklch, var(--muted) 25%, transparent);
  color: var(--muted);
}
.puid {
  color: var(--muted);
  font-size: 13px;
  margin: 2px 0 8px;
}
.pstats {
  display: flex;
  gap: 14px;
  font-size: 14px;
  flex-wrap: wrap;
}
.expbar {
  margin-top: 10px;
  height: 8px;
  border-radius: 999px;
  background: var(--card-strong);
  overflow: hidden;
}
.expfill {
  height: 100%;
  background: var(--gradient, var(--primary));
}
.sec-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.sec-head h2 {
  font-size: 16px;
  margin: 0;
}
.lands,
.logs {
  padding: 18px;
  border-radius: var(--radius-lg);
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
.land.st-ripe {
  border-color: color-mix(in oklch, var(--good) 50%, transparent);
}
.land.st-locked {
  opacity: 0.6;
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
.log-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  max-height: 320px;
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
  padding: 8px 0;
}
</style>
