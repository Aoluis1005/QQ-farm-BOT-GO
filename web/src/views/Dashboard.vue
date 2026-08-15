<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const acc = () => getAccountId()

const profile = ref({})
const income = ref({})
const incomeOpen = ref(false)
const patrol = ref({ steal: {}, help: {}, farm: {} })
const logs = ref([])
const loading = ref(false)

const INC_MAP = {
  收获: 'harvest', 偷菜: 'steal', 种植: 'plant', 施肥: 'fertilize', 浇水: 'water',
  除草: 'weed', 除虫: 'insecticide', 一键务农: 'oneKeyFarm', 帮忙: 'helpFarming',
  清黄金虫: 'clearGolden', 放黄金虫: 'putGolden', 任务: 'task',
}
const INC_ITEMS = ['收获','偷菜','种植','施肥','浇水','除草','除虫','一键务农','帮忙','清黄金虫','放黄金虫','任务']

const PATROL_KEY = { 偷菜: 'steal', 帮忙: 'help', 收菜: 'farm' } // POST key
const PATROL_GET = { 偷菜: 'steal', 帮忙: 'help', 收菜: 'farm' } // GET key 同为 farm（对齐 legacy switchMap）
const PATROL_TRIO = { 偷菜: 'steal', 帮忙: 'help', 收菜: 'farm' }
// legacy 各巡查项默认间隔：偷菜 3~10 / 帮忙 3~5 / 收菜 5~10（仅在接口无返回值时兜底）
const PATROL_DEFAULT = { steal: { min: 3, max: 10 }, help: { min: 3, max: 5 }, farm: { min: 5, max: 10 } }

// 生涯统计
const careerOpen = ref(false)
const career = ref(null)
const careerLoading = ref(false)

async function load() {
  if (!acc()) { loading.value = false; return }   // 未选账号：不发起注定失败的游戏数据请求
  loading.value = true
  try {
    const [p, inc, pat, lg] = await Promise.all([
      api.get('/api/home/profile').catch(() => null),
      api.get('/api/home/income/today').catch(() => null),
      api.get('/api/home/patrol').catch(() => null),
      api.get('/api/home/logs').catch(() => null),
    ])
    profile.value = p?.data?.data || {}
    income.value = inc?.data?.data || {}
    patrol.value = pat?.data?.data || { steal: {}, help: {}, farm: {} }
    logs.value = lg?.data?.data || lg?.data?.logs || []
  } finally {
    loading.value = false
  }
}

function fmtNum(n) { return n == null ? '--' : Number(n).toLocaleString() }
// 大数友好缩写：≥1亿→X.XX亿，≥1万→X.X万，否则千分位（避免上亿金币文字过长）
function fmtBig(n) {
  if (n === undefined || n === null || n === '') return 0
  const v = Number(String(n).replace(/,/g, ''))
  if (isNaN(v)) return 0
  if (v >= 1e8) return (v / 1e8).toFixed(2).replace(/\.0+$/, '') + '亿'
  if (v >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return v.toLocaleString()
}

function togglePatrol(who) {
  if (!acc()) return
  const key = PATROL_KEY[who]
  const getKey = PATROL_GET[who]
  const enabled = !(patrol.value[getKey]?.enabled ?? true)
  api.post('/api/home/patrol', { key, enabled }).catch((e) => app.error(e.response?.data?.error || '更新失败'))
  patrol.value = { ...patrol.value, [getKey]: { ...(patrol.value[getKey] || {}), enabled } }
}

function allOn() {
  if (!acc()) return
  ;['steal', 'help', 'farm'].forEach((k) => {
    patrol.value = { ...patrol.value, [k]: { ...(patrol.value[k] || {}), enabled: true } }
  })
  // 逐项持久化（对齐 legacy allOn：三个真实 key 全 POST enabled:true）
  api.post('/api/home/patrol', { key: 'steal', enabled: true }).catch(() => {})
  api.post('/api/home/patrol', { key: 'help', enabled: true }).catch(() => {})
  api.post('/api/home/patrol', { key: 'farm', enabled: true }).catch(() => {})
}

async function clearLogs() {
  if (!acc()) { app.error('请先选择账号'); return }
  try {
    await api.delete('/api/logs')
    logs.value = []
    app.success('日志已清空')
  } catch (e) { app.error(e.response?.data?.error || '清空失败') }
}

async function openCareer() {
  careerOpen.value = true
  careerLoading.value = true
  career.value = null
  try {
    const { data } = await api.get('/api/career')
    career.value = data.data || null
  } catch (e) { app.error(e.response?.data?.error || '加载生涯失败') } finally { careerLoading.value = false }
}

function incVal(label) {
  const k = INC_MAP[label]
  const v = income.value[k]
  return v === undefined || v === null ? '--' : Number(v).toLocaleString()
}

function avatarHtml() {
  const a = profile.value.avatar
  if (a && /^(https?:)?\/\//i.test(a)) return `<img src="${String(a).replace(/"/g, '&quot;')}" alt="" />`
  return profile.value.avatar || '🐰'
}

// 生涯统计（对齐 legacy render：items 按 count 降序前30；level_stats 前30）
const careerPlayer = computed(() => (career.value && career.value.player) || {})
const careerItems = computed(() => {
  const arr = (career.value && career.value.items) || []
  return arr.slice().sort((a, b) => (b.count || 0) - (a.count || 0)).slice(0, 30)
})
const careerLv = computed(() => ((career.value && career.value.level_stats) || []).slice(0, 30))

function cvImg(it, dft) { return it && it.image ? it.image : dft }
function lvPct() { return Math.max(0, Math.min(100, careerPlayer.value.expPercent != null ? careerPlayer.value.expPercent : 0)) }

onMounted(load)
</script>

<template>
  <div class="page-home">
    <!-- 用户资产卡 -->
    <div class="profile">
      <div class="avatar" @click="openCareer">
        <div class="ring"><div class="face" v-html="avatarHtml()"></div></div>
        <span class="lvl">Lv.{{ profile.level || '—' }}</span>
      </div>
      <div class="pinfo">
        <h2>{{ profile.name || '未登录' }}</h2>
        <span class="uid">UID · {{ profile.uid || '—' }}</span>
        <div class="stats">
          <div class="stat"><strong>🪙 {{ fmtBig(profile.gold) }}</strong><span>金币</span></div>
          <div class="stat"><strong>🎟️ {{ fmtBig(profile.coupons) }}</strong><span>点券</span></div>
          <div class="stat"><strong>🫘 {{ fmtBig(profile.goldenBeans) }}</strong><span>金豆</span></div>
        </div>
        <div class="exp">
          <div class="bar"><div class="fill" :style="{ width: (profile.expPercent || 0) + '%' }"></div></div>
          <small>经验 {{ profile.exp ?? '--' }} / {{ profile.expMax ?? '--' }}</small>
        </div>
      </div>
    </div>

    <!-- 今日收益 -->
    <div class="sec-title"><span>今日收益</span><span class="link" @click="incomeOpen = !incomeOpen">{{ incomeOpen ? '收起 ▾' : '详情 ›' }}</span></div>
    <div class="income" :class="{ open: incomeOpen }">
      <div class="inc-top">
        <div class="inc-cell"><span class="inc-l">💰 收益</span><strong>{{ fmtBig(income.totalGold) }}</strong><em>金币</em></div>
        <div class="inc-div"></div>
        <div class="inc-cell"><span class="inc-l">🎁 同气礼包</span><strong>{{ income.dogGifts ?? 0 }}</strong><em>个</em></div>
      </div>
      <div class="income-stats">
        <div v-for="l in INC_ITEMS" :key="l" class="st"><i>{{ { 收获:'🌾',偷菜:'🕵️',种植:'🌱',施肥:'🧴',浇水:'🚿',除草:'🌿',除虫:'🐛',一键务农:'⚙️',帮忙:'🤝',清黄金虫:'🟡',放黄金虫:'🐞',任务:'📋' }[l] }}</i><span>{{ l }}</span><b>{{ incVal(l) }}</b></div>
      </div>
    </div>

    <!-- 巡查间隔 -->
    <div class="sec-title"><span>巡查间隔</span><span class="link" @click="allOn">全部开启</span></div>
    <div class="patrol">
      <div v-for="(o, who) in PATROL_TRIO" :key="who" class="cell" @click="togglePatrol(who)">
        <div class="ic" :class="'ic-' + { 偷菜: 'steal', 帮忙: 'help', 收菜: 'harvest' }[who]">{{ { 偷菜: '🕵️', 帮忙: '🤝', 收菜: '🧺' }[who] }}</div>
        <h4>{{ who }}</h4>
        <div class="timer">随机 {{ patrol[o]?.min ?? PATROL_DEFAULT[o].min }}~{{ patrol[o]?.max ?? PATROL_DEFAULT[o].max }} 秒</div>
        <div class="switch" :class="{ on: patrol[o]?.enabled }"></div>
      </div>
    </div>

    <!-- 操作日志 -->
    <div class="sec-title"><span>操作日志</span><span class="link" @click="clearLogs">清空</span></div>
    <div class="logs">
      <div v-if="!logs.length" class="empty-tip">暂无日志</div>
      <div v-for="(lg, i) in logs" :key="i" class="log-row">
        <span v-if="lg.tag" class="lg-type">{{ lg.tag }}</span>
        <span class="lg-msg">{{ lg.msg || '' }}</span>
        <span class="lg-time">{{ lg.time || '' }}</span>
      </div>
    </div>
  </div>

  <!-- 生涯统计 sheet -->
  <div class="sheet-mask" :class="{ show: careerOpen }" @click="careerOpen = false"></div>
  <div class="sheet" :class="{ show: careerOpen }">
    <div class="handle"></div>
    <h3>🧑🌾 生涯统计</h3>
    <p class="sub" style="margin-bottom:10px">{{ careerLoading ? '加载中…' : (career ? '' : '暂无数据') }}</p>
    <div style="max-height:62vh;overflow:auto">
      <template v-if="career">
        <div class="career-head">
          <div class="c-av"><img v-if="/^(https?:)?\/\//i.test(careerPlayer.avatar || '')" :src="careerPlayer.avatar" alt=""><span v-else>{{ careerPlayer.avatar || '🐰' }}</span></div>
          <div class="c-info">
            <div class="c-name"><b>{{ careerPlayer.name || '未知玩家' }}</b><span class="c-lv">Lv.{{ careerPlayer.level || 0 }}</span></div>
            <div class="c-open">UID {{ careerPlayer.gid || '-' }}</div>
          </div>
        </div>
        <div class="career-exp">
          <div class="bar"><div class="fill" :style="{ width: lvPct() + '%' }"></div></div>
          <small>经验 <b>{{ Number(careerPlayer.exp || 0).toLocaleString() }}</b> / {{ Number(careerPlayer.expMax || 0).toLocaleString() }}</small>
        </div>
        <div class="career-sec"><h4>🌾 收获排行<span class="sub">共 {{ careerItems.length }} 种</span></h4><div class="career-list">
          <div v-for="(it, i) in careerItems" :key="i" class="career-row"><span class="im">🌾</span><span class="nm">{{ it.name }}</span><span class="cnt">× <b>{{ Number(it.count || 0).toLocaleString() }}</b></span></div>
          <div v-if="!careerItems.length" class="career-empty">暂无收获记录</div>
        </div></div>
        <div class="career-sec"><h4>📈 作物等级<span class="sub">共 {{ careerLv.length }} 种</span></h4><div class="career-list">
          <div v-for="(it, i) in careerLv" :key="i" class="career-row"><span class="im">🌟</span><span class="nm">{{ it.name }}</span><span class="cnt">Lv.{{ it.level || 0 }} × {{ Number(it.count || 0).toLocaleString() }}</span></div>
          <div v-if="!careerLv.length" class="career-empty">暂无作物等级数据</div>
        </div></div>
      </template>
    </div>
    <button class="close" style="margin-top:16px" @click="careerOpen = false">关闭</button>
  </div>
</template>

<style scoped>
.empty-tip { text-align: center; color: var(--muted); font-size: 12.5px; padding: 18px 0; }
.log-row { display: flex; align-items: center; gap: 8px; padding: 8px 0; border-bottom: 1px solid var(--border); font-size: 12px; }
.lg-type { flex: none; font-size: 11px; color: var(--muted); min-width: 48px; }
.lg-msg { flex: 1; min-width: 0; color: var(--foreground); }
.lg-time { flex: none; color: var(--muted); font-size: 11px; }
.sheet-mask, .sheet.show { display: block; }
.sheet-mask { display: none; }
.sheet { display: none; }
</style>
