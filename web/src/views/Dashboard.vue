<script setup>
import { ref, onMounted, computed } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const profile = ref(null)
const lands = ref([])
const logs = ref([])
const income = ref(null)
const loading = ref(false)
const incomeExpanded = ref(false)
const patrol = ref({ steal: { min: 0, max: 0, enabled: true }, help: { min: 0, max: 0, enabled: true }, farm: { min: 0, max: 0, enabled: true } })
const patrolLoading = ref(false)

async function load() {
  loading.value = true
  try {
    const [p, l, lg, inc] = await Promise.all([
      api.get('/api/home/profile'),
      api.get('/api/farm/lands').catch(() => null),
      api.get('/api/home/logs').catch(() => null),
      api.get('/api/home/income/today').catch(() => null),
    ])
    profile.value = p.data.data
    lands.value = l?.data?.data || []
    logs.value = lg?.data?.data || []
    income.value = inc?.data?.data || null
  } catch (e) {
    app.error('加载首页失败：' + (e.response?.data?.error || e.message))
  } finally { loading.value = false }
}

async function loadPatrol() {
  try {
    const { data } = await api.get('/api/home/patrol')
    if (data.data) patrol.value = { ...patrol.value, ...data.data }
  } catch { /* silent */ }
}
async function togglePatrol(key) {
  patrolLoading.value = true
  try {
    const enabled = !(patrol.value[key]?.enabled ?? true)
    await api.post('/api/home/patrol', { key, enabled })
    patrol.value = { ...patrol.value, [key]: { ...(patrol.value[key] || {}), enabled } }
    app.success('巡查状态已更新')
  } catch (e) {
    app.error('更新巡查失败：' + (e.response?.data?.error || e.message))
  } finally { patrolLoading.value = false }
}
function patrolLabel(key) {
  return { steal: '偷菜', help: '帮忙', farm: '收菜' }[key] || key
}

const incomeCategories = computed(() => {
  if (!income.value) return []
  const map = [
    { key: 'harvest', label: '🌾 收获', icon: '🌾' },
    { key: 'steal', label: '🕵️ 偷菜', icon: '🕵️' },
    { key: 'plant', label: '🌱 种植', icon: '🌱' },
    { key: 'fertilize', label: '🧪 施肥', icon: '🧪' },
    { key: 'water', label: '💧 浇水', icon: '💧' },
    { key: 'weed', label: '🌿 除草', icon: '🌿' },
    { key: 'insect', label: '🐛 除虫', icon: '🐛' },
    { key: 'turbo', label: '⚡ 一键务农', icon: '⚡' },
    { key: 'helpWater', label: '💧 帮浇水', icon: '💧' },
    { key: 'helpWeed', label: '🌿 帮除草', icon: '🌿' },
    { key: 'helpInsect', label: '🐛 帮除虫', icon: '🐛' },
    { key: 'goldenBugClear', label: '✨ 清黄金虫', icon: '✨' },
    { key: 'goldenBugPut', label: '🐛 放黄金虫', icon: '🐛' },
    { key: 'task', label: '📋 任务', icon: '📋' },
  ]
  return map.map(m => ({ ...m, value: income.value[m.key] || 0 }))
})

function statusText(s) {
  return { locked: '🔒 未解锁', empty: '🟫 空地', growing: '🌿 生长中', ripe: '🍎 可收获' }[s] || s
}
function statusClass(s) {
  return { locked: 'st-locked', empty: 'st-empty', growing: 'st-grow', ripe: 'st-ripe' }[s] || ''
}

onMounted(() => { load(); loadPatrol() })
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

    <!-- 今日收益 -->
    <section class="income glass" v-if="income">
      <div class="sec-head">
        <h2>💰 今日收益</h2>
        <span class="link" @click="incomeExpanded = !incomeExpanded">
          {{ incomeExpanded ? '收起' : '详情 ›' }}
        </span>
      </div>
      <div class="inc-top">
        <div class="inc-cell">
          <span class="inc-l">💰 收益</span>
          <strong>{{ income.totalGold || '--' }}</strong><em>金币</em>
        </div>
        <div class="inc-div"></div>
        <div class="inc-cell">
          <span class="inc-l">🎁 同气礼包</span>
          <strong>{{ income.giftCount || 0 }}</strong><em>个</em>
        </div>
      </div>
      <div class="income-stats" v-if="incomeExpanded">
        <div v-for="c in incomeCategories" :key="c.key" class="st">
          <i>{{ c.icon }}</i><span>{{ c.label }}</span><b>{{ c.value || '--' }}</b>
        </div>
      </div>
    </section>

    <!-- 巡查间隔面板 -->
    <section class="patrol glass">
      <div class="sec-head">
        <h2>🔄 巡查间隔</h2>
        <span class="link" @click="patrol.steal.enabled = patrol.help.enabled = patrol.farm.enabled = true">全部开启</span>
      </div>
      <div class="patrol-grid">
        <div class="patrol-cell" @click="togglePatrol('steal')">
          <div class="ic ic-steal">🕵️</div>
          <h4>偷菜</h4>
          <small class="patrol-t">随机 {{ patrol.steal.min }}~{{ patrol.steal.max }} 秒</small>
          <div class="switch" :class="{ on: patrol.steal.enabled }" :disabled="patrolLoading"></div>
        </div>
        <div class="patrol-cell" @click="togglePatrol('help')">
          <div class="ic ic-help">🤝</div>
          <h4>帮忙</h4>
          <small class="patrol-t">随机 {{ patrol.help.min }}~{{ patrol.help.max }} 秒</small>
          <div class="switch" :class="{ on: patrol.help.enabled }" :disabled="patrolLoading"></div>
        </div>
        <div class="patrol-cell" @click="togglePatrol('farm')">
          <div class="ic ic-harvest">🧺</div>
          <h4>收菜</h4>
          <small class="patrol-t">随机 {{ patrol.farm.min }}~{{ patrol.farm.max }} 秒</small>
          <div class="switch" :class="{ on: patrol.farm.enabled }" :disabled="patrolLoading"></div>
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
.sec-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.sec-head h2 { font-size: 16px; margin: 0; }
.link { color: var(--primary); cursor: pointer; font-size: 13px; }
.income, .patrol, .lands, .logs { padding: 18px; border-radius: var(--radius-lg); }
.inc-top { display: flex; gap: 12px; margin-bottom: 10px; }
.inc-cell { flex: 1; text-align: center; }
.inc-l { font-size: 12px; color: var(--muted); display: block; }
.inc-cell strong { font-size: 20px; display: block; margin: 4px 0; }
.inc-cell em { font-size: 12px; color: var(--muted); }
.inc-div { width: 1px; background: var(--border); }
.income-stats { display: grid; grid-template-columns: repeat(auto-fill, minmax(120px, 1fr)); gap: 8px; }
.st { display: flex; align-items: center; gap: 6px; padding: 6px 0; font-size: 13px; }
.st i { font-size: 14px; }
.st span { flex: 1; color: var(--muted); }
.st b { color: var(--foreground); }
.patrol-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 12px; }
.patrol-cell { padding: 12px; border-radius: var(--radius-md); background: var(--card-strong); border: 1px solid var(--border); text-align: center; cursor: pointer; }
.patrol-cell h4 { font-size: 14px; margin: 8px 0; }
.patrol-t { font-size: 11px; color: var(--muted); display:block; margin-bottom:6px; }
.ic { font-size: 28px; }
.switch { width: 44px; height: 24px; border-radius: 999px; background: var(--muted); margin: 0 auto; position: relative; transition: background 0.2s; }
.switch.on { background: var(--primary); }
.switch:disabled { opacity: 0.5; }
.land-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 12px; }
.land { padding: 12px; border-radius: var(--radius-md); background: var(--card-strong); border: 1px solid var(--border); }
.land.st-ripe { border-color: color-mix(in oklch, var(--good) 50%, transparent); }
.land.st-locked { opacity: 0.6; }
.land-top { display: flex; justify-content: space-between; font-size: 13px; color: var(--muted); }
.land-mid { margin: 8px 0 4px; font-weight: 600; }
.land-plant { font-size: 12px; color: var(--muted); }
.log-list { display: flex; flex-direction: column; gap: 6px; max-height: 320px; overflow: auto; }
.log-item { display: flex; gap: 10px; font-size: 13px; align-items: baseline; }
.log-tag { flex: none; padding: 1px 8px; border-radius: 999px; background: var(--primary-soft); color: var(--primary); font-size: 12px; }
.log-msg { flex: 1; min-width: 0; }
.log-time { flex: none; color: var(--muted); font-size: 12px; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 8px 0; }
</style>
