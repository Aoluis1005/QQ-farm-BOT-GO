<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const profile = ref(null)
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { data } = await api.get('/api/home/profile')
    profile.value = data.data
  } catch (e) {
    app.error('加载个人资料失败：' + (e.response?.data?.error || e.message))
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="dash">
    <section class="profile-card glass" v-if="profile">
      <div class="avatar">{{ profile.avatar ? '' : '🐰' }}</div>
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

    <section class="lands glass" v-if="profile">
      <div class="sec-head"><h2>📊 账户概览</h2></div>
      <div class="kv">
        <div><span>经验</span><b>{{ profile.exp }} / {{ profile.expMax || '-' }}</b></div>
        <div><span>金币</span><b>{{ profile.gold }}</b></div>
        <div><span>点券</span><b>{{ profile.coupons }}</b></div>
        <div><span>金豆</span><b>{{ profile.goldenBeans }}</b></div>
      </div>
    </section>
  </div>
</template>

<style scoped>
.dash { display: grid; gap: 16px; }
.profile-card { display: flex; gap: 16px; padding: 18px; border-radius: var(--radius-lg); }
.avatar { width: 64px; height: 64px; border-radius: 50%; background: var(--primary-soft); display: grid; place-items: center; font-size: 32px; flex: none; }
.pinfo { flex: 1; min-width: 0; }
.pname { font-size: 18px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
.pconn { font-size: 11px; padding: 2px 8px; border-radius: 999px; }
.pconn.on { background: color-mix(in oklch, var(--good) 25%, transparent); color: var(--good); }
.pconn.off { background: color-mix(in oklch, var(--muted) 25%, transparent); color: var(--muted); }
.puid { color: var(--muted); font-size: 13px; margin: 2px 0 8px; }
.pstats { display: flex; gap: 14px; font-size: 14px; flex-wrap: wrap; }
.expbar { margin-top: 10px; height: 8px; border-radius: 999px; background: var(--card-strong); overflow: hidden; }
.expfill { height: 100%; background: var(--gradient, var(--primary)); }
.lands { padding: 18px; border-radius: var(--radius-lg); }
.kv { display: grid; grid-template-columns: repeat(2, 1fr); gap: 10px; }
.kv > div { display: flex; justify-content: space-between; padding: 10px 12px; background: var(--card-strong); border-radius: var(--radius-md); border: 1px solid var(--border); }
.kv span { color: var(--muted); font-size: 13px; }
.kv b { font-size: 14px; }
</style>
