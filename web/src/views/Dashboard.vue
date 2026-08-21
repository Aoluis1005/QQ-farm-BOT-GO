<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
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
const PATROL_GET = { 偷菜: 'steal', 帮忙: 'help', 收菜: 'farm' } // GET key 同为 farm
const PATROL_TRIO = { 偷菜: 'steal', 帮忙: 'help', 收菜: 'farm' }
// legacy 各巡查项默认间隔：偷菜 3~10 / 帮忙 3~5 / 收菜 5~10（仅在接口无返回值时兜底）
const PATROL_DEFAULT = { steal: { min: 3, max: 10 }, help: { min: 3, max: 5 }, farm: { min: 5, max: 10 } }
// 巡查钟配色与扫动节奏
const PATROL_COLOR = { 偷菜: 'var(--good)', 帮忙: 'var(--warn)', 收菜: 'var(--primary)' }
const PATROL_DUR = { 偷菜: 6, 帮忙: 11, 收菜: 8 }

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
  // 逐项持久化
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
  visibleCount.value = 35
  itemImgErr.value = {}
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

// 巡查钟 SVG：12 刻度 + 始终旋转的指针（CSS .pc-hand 动画，扫动表示巡查进行中）
function patrolClockSvg(who) {
  const s = 56, c = s / 2, r = s / 2 - 9
  const ok = PATROL_TRIO[who]
  const on = !!patrol.value[ok]?.enabled
  const dur = PATROL_DUR[who] || 8
  const col = on ? PATROL_COLOR[who] : 'var(--muted)'
  let t = ''
  for (let i = 0; i < 12; i++) {
    const major = i % 3 === 0
    const th = (i * 30) * Math.PI / 180
    const x1 = c + Math.sin(th) * r, y1 = c - Math.cos(th) * r
    const x2 = c + Math.sin(th) * (r - (major ? 7 : 4)), y2 = c - Math.cos(th) * (r - (major ? 7 : 4))
    t += `<line x1="${x1.toFixed(1)}" y1="${y1.toFixed(1)}" x2="${x2.toFixed(1)}" y2="${y2.toFixed(1)}" style="stroke:var(--muted);stroke-width:${major ? 2 : 1};stroke-linecap:round;opacity:.5"/>`
  }
  // 旋转组：手 + 渐隐拖尾（绕圆心 28,28 旋转，时长按巡检节奏）
  const R = r - 6
  const th0 = (-60) * Math.PI / 180
  const ax0 = (c + Math.sin(th0) * R).toFixed(1), ay0 = (c - Math.cos(th0) * R).toFixed(1)
  const ax1 = c.toFixed(1), ay1 = (c - R).toFixed(1)
  const sweep = `<g class="pc-hand" style="animation-duration:${dur}s">`
    + `<path d="M ${ax0} ${ay0} A ${R} ${R} 0 0 1 ${ax1} ${ay1}" style="stroke:${col};stroke-width:5;fill:none;opacity:.18;stroke-linecap:butt"/>`
    + `<line x1="${c}" y1="${c}" x2="${c}" y2="${c - (r - 5)}" style="stroke:${col};stroke-width:2.6;stroke-linecap:round"/></g>`
  return `<svg width="${s}" height="${s}" viewBox="0 0 ${s} ${s}">`
    + `<circle cx="${c}" cy="${c}" r="${r}" style="fill:none;stroke:var(--border);stroke-width:5"/>`
    + t + sweep
    + `<circle cx="${c}" cy="${c}" r="3.4" style="fill:${col}"/></svg>`
}

// 生涯统计
const careerPlayer = computed(() => (career.value && career.value.player) || {})
const careerMeta = computed(() => (career.value && career.value.meta) || {})
const totalHarvest = computed(() => Number(careerMeta.value.stats_total || 0))
const totalFriendPick = computed(() => Number(careerMeta.value.stats_count || 0))
const careerItems = computed(() => ((career.value && career.value.items) || []).slice().sort((a, b) => (b.count || 0) - (a.count || 0)))
// 领奖台：中间最高
const podiumItems = computed(() =>
  [1, 0, 2].flatMap((i, idx) => { const it = careerItems.value[i]; return it ? [{ item: it, rank: idx + 1 }] : [] })
)
const visibleCount = ref(35)
const remainingItems = computed(() => careerItems.value.slice(3, visibleCount.value))
const itemImgErr = ref({})
const iconHarvest = computed(() => itemImgErr.value.harvest ? '' : '/game-config/seed_images_named/' + encodeURIComponent('10001_收获_icon_harvest.png'))
const iconSteal = computed(() => itemImgErr.value.steal ? '' : '/game-config/seed_images_named/' + encodeURIComponent('10008_摘菜_icon_steal.png'))
function onImgErr(k) { itemImgErr.value[k] = true }
function onStatErr(k) { itemImgErr.value[k] = true }
function loadMoreItems() { visibleCount.value += 40 }
function fmtCompact(n) {
  n = Number(n || 0); if (!isFinite(n)) return '0'
  const v = Math.round(n)
  if (Math.abs(v) >= 1e8) return (v / 1e8).toFixed(1).replace(/\.0$/, '') + '亿'
  if (Math.abs(v) >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return v.toLocaleString()
}
function rankBadge(idx) {
  return idx === 0 ? 'rank-gold' : idx === 1 ? 'rank-silver' : 'rank-bronze'
}

// 首页日志/资产实时刷新：每 5s 拉一次；切号事件立即用新账号重拉（热切换）
let pollTimer = null
function onSwitched() { load() }
onMounted(() => {
  load()
  pollTimer = setInterval(load, 5000)
  window.addEventListener('account-switched', onSwitched)
})
onUnmounted(() => { if (pollTimer) { clearInterval(pollTimer); pollTimer = null }; window.removeEventListener('account-switched', onSwitched) })
</script>

<template>
  <div class="page-home">
    <!-- 用户资产卡 — 左头像+环形经验进度 / 右三资源竖排 -->
    <div class="profile">
      <div class="pc-body">
        <!-- 左：头像 + 环形经验进度 + 文字 -->
        <div class="pc-left">
          <div class="avatar-ring">
            <svg width="104" height="104" viewBox="0 0 104 104">
              <circle cx="52" cy="52" r="46" style="fill:none;stroke:var(--border);stroke-width:8"/>
              <circle cx="52" cy="52" r="46" style="fill:none;stroke:var(--primary);stroke-width:8;stroke-linecap:round"
                      stroke-dasharray="289"
                      :stroke-dashoffset="289 * (1 - (profile.expPercent || 0) / 100)"/>
            </svg>
            <div class="av" @click="openCareer">
              <img v-if="profile.avatar && /^(https?:)?\/\//i.test(profile.avatar)" :src="profile.avatar" alt="">
              <span v-else>{{ (profile.name || profile.avatar || '?').charAt(0).toUpperCase() }}</span>
            </div>
          </div>
          <div class="pc-text">
            <div class="pc-name">{{ profile.name || '未登录' }}</div>
            <div class="pc-uid">UID · {{ profile.uid || '—' }}</div>
            <div class="pc-exp">经验 <b>{{ fmtNum(profile.exp) }} / {{ fmtNum(profile.expMax) }}</b></div>
            <span class="pc-lvl">Lv.{{ profile.level || '—' }}</span>
          </div>
        </div>
        <!-- 右：金币 / 点券 / 金豆 各一排 -->
        <div class="pc-right">
          <div class="res-row" style="--rc:var(--warn)">
            <div class="res-ic">🪙</div>
            <div class="res-meta"><span class="res-label">金币</span><span class="res-val">{{ fmtBig(profile.gold) }}</span></div>
          </div>
          <div class="res-row" style="--rc:var(--primary)">
            <div class="res-ic">🎟️</div>
            <div class="res-meta"><span class="res-label">点券</span><span class="res-val">{{ fmtBig(profile.coupons) }}</span></div>
          </div>
          <div class="res-row" style="--rc:var(--good)">
            <div class="res-ic">🫘</div>
            <div class="res-meta"><span class="res-label">金豆</span><span class="res-val">{{ fmtBig(profile.goldenBeans) }}</span></div>
          </div>
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
      <div v-for="(o, who) in PATROL_TRIO" :key="who" class="cell" :style="{ '--tc': PATROL_COLOR[who] }" @click="togglePatrol(who)">
        <div class="pr-clock" v-html="patrolClockSvg(who)"></div>
        <div class="pr-mid">
          <div class="pr-name">{{ who }}</div>
          <div class="pr-sub">随机 {{ patrol[o]?.min ?? PATROL_DEFAULT[o].min }}~{{ patrol[o]?.max ?? PATROL_DEFAULT[o].max }} 秒</div>
        </div>
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

  <!-- 生涯统计弹窗 -->
  <div class="sheet-mask" :class="{ show: careerOpen }" @click="careerOpen = false"></div>
  <div class="sheet career-sheet" :class="{ show: careerOpen }">
    <button class="career-x" aria-label="关闭" @click="careerOpen = false">✕</button>
    <template v-if="!careerLoading && career">
      <!-- 头部 -->
      <div class="career-top">
        <img v-if="/^(https?:)?\/\//i.test(careerPlayer.avatar || '')" :src="careerPlayer.avatar" :alt="careerPlayer.name">
        <div v-else class="career-av-fb">{{ (careerPlayer.name || '?').charAt(0).toUpperCase() }}</div>
        <div class="career-top-info">
          <div class="career-name">{{ careerPlayer.name || '未设置昵称' }}</div>
          <div class="career-tags">
            <span class="t-lv">Lv.{{ careerPlayer.level || 0 }}</span>
            <span class="t-exp">经验 {{ fmtCompact(careerPlayer.exp || 0) }}</span>
          </div>
          <div class="career-gid" :title="String(careerPlayer.gid || 0)">角色编号：{{ careerPlayer.gid || 0 }}</div>
        </div>
      </div>
      <!-- 生涯块 -->
      <div class="career-block">
        <div class="cb-title">生涯</div>
        <div class="cb-grid">
          <div class="cb-cell">
            <div class="cb-cell-h">
              <img v-if="iconHarvest" :src="iconHarvest" alt="" @error="onStatErr('harvest')"><span>历史累计收获</span>
            </div>
            <div class="cb-val orange" :title="totalHarvest.toLocaleString()">{{ fmtCompact(totalHarvest) }}</div>
          </div>
          <div class="cb-cell">
            <div class="cb-cell-h">
              <img v-if="iconSteal" :src="iconSteal" alt="" @error="onStatErr('steal')"><span>累计摘取好友作物</span>
            </div>
            <div class="cb-val rose" :title="totalFriendPick.toLocaleString()">{{ fmtCompact(totalFriendPick) }}</div>
          </div>
        </div>
        <div v-if="podiumItems.length" class="cb-podium">
          <div v-for="e in podiumItems" :key="'p' + e.rank" class="pd-item">
            <div class="pd-rank" :class="rankBadge(e.rank - 1)">{{ e.rank }}</div>
            <img v-if="e.item.image && !itemImgErr[e.item.id]" :src="e.item.image" :alt="e.item.name" @error="onImgErr(e.item.id)">
            <div v-else class="pd-fb">{{ (e.item.name || '?').charAt(0) }}</div>
            <div class="pd-name" :title="e.item.name">{{ e.item.name }}</div>
            <div class="pd-cnt" :title="e.item.count.toLocaleString()">{{ fmtCompact(e.item.count) }}</div>
          </div>
        </div>
      </div>
      <!-- 收获明细 -->
      <div class="cd-title"><span>收获明细</span><span class="cd-sub">({{ careerItems.length }})</span></div>
      <div v-if="remainingItems.length" class="cd-grid">
        <div v-for="it in remainingItems" :key="it.id" class="cd-item">
          <img v-if="it.image && !itemImgErr[it.id]" :src="it.image" :alt="it.name" loading="lazy" @error="onImgErr(it.id)">
          <div v-else class="cd-fb">{{ (it.name || '?').charAt(0) }}</div>
          <div class="cd-name" :title="it.name">{{ it.name }}</div>
          <div class="cd-cnt" :title="it.count.toLocaleString()">{{ fmtCompact(it.count) }}</div>
        </div>
      </div>
      <button v-if="visibleCount < careerItems.length" class="cd-more" @click="loadMoreItems">加载更多（剩余 {{ careerItems.length - visibleCount }}）</button>
      <div v-if="!careerItems.length" class="cd-empty">暂无收获数据</div>
    </template>
    <template v-else>
      <p class="sub">{{ careerLoading ? '加载中…' : '暂无数据' }}</p>
    </template>
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

/* ===== 生涯统计弹窗 ===== */
.career-sheet { width: min(520px, calc(100vw - 32px)); max-width: 92vw; max-height: 82dvh; overflow-y: auto; padding: 20px; scrollbar-width: none; }
.career-sheet::-webkit-scrollbar { width: 0; height: 0; display: none; }
.career-x { position: absolute; right: 12px; top: 12px; width: 30px; height: 30px; border: none; border-radius: 50%; background: var(--bg-hi); color: var(--muted); font-size: 14px; line-height: 1; cursor: pointer; display: flex; align-items: center; justify-content: center; z-index: 2; }
.career-x:active { transform: scale(.92); }
.career-top { display: flex; align-items: center; gap: 13px; padding-right: 26px; }
.career-top img { width: 52px; height: 52px; border-radius: 50%; object-fit: cover; flex: none; background: var(--bg-hi); border: 1px solid var(--border); }
.career-av-fb { width: 52px; height: 52px; border-radius: 50%; flex: none; display: grid; place-items: center; font-size: 20px; font-weight: 700; color: var(--muted); background: linear-gradient(135deg, var(--bg-hi), var(--bg-lo)); border: 1px solid var(--border); }
.career-top-info { min-width: 0; flex: 1; }
.career-name { font-size: 16px; font-weight: 700; color: var(--foreground); }
.career-tags { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 5px; }
.career-tags .t-lv { font-size: 10.5px; font-weight: 600; padding: 2px 8px; border-radius: 999px; background: var(--primary-soft); color: var(--primary); }
.career-tags .t-exp { font-size: 10.5px; font-weight: 500; padding: 2px 8px; border-radius: 999px; background: color-mix(in oklch, var(--primary) 10%, transparent); color: var(--primary); }
.career-gid { margin-top: 4px; font-size: 11px; color: var(--muted); word-break: break-all; }
.career-block { margin: 14px 0; padding: 12px; border-radius: 16px; background: var(--card); border: 1px solid var(--border); }
.cb-title { text-align: center; font-size: 14px; font-weight: 600; color: var(--foreground); margin-bottom: 10px; }
.cb-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
.cb-cell { min-width: 0; padding: 10px; text-align: center; border-radius: 12px; background: var(--card-strong); border: 1px solid var(--border); }
.cb-cell-h { display: flex; align-items: center; justify-content: center; gap: 5px; font-size: 11px; font-weight: 500; color: var(--muted); }
.cb-cell-h img { width: 18px; height: 18px; object-fit: contain; flex: none; }
.cb-val { margin-top: 3px; font-size: 17px; font-weight: 800; line-height: 1.2; white-space: nowrap; }
.cb-val.orange { color: #ea580c; }
.cb-val.rose { color: #e11d48; }
.cb-podium { display: grid; grid-template-columns: repeat(3, 1fr); align-items: end; gap: 8px; margin-top: 12px; padding-top: 10px; border-top: 1px dashed var(--border); }
.pd-item { min-width: 0; text-align: center; }
.pd-rank { width: 24px; height: 24px; margin: 0 auto 6px; border-radius: 50%; display: grid; place-items: center; font-size: 12px; font-weight: 800; color: #fff; }
.pd-rank.rank-gold { background: linear-gradient(135deg, #fbbf24, #d97706); }
.pd-rank.rank-silver { background: linear-gradient(135deg, #9ca3af, #6b7280); }
.pd-rank.rank-bronze { background: linear-gradient(135deg, #fb923c, #ea580c); }
.pd-item img { width: 50px; height: 50px; object-fit: contain; display: block; margin: 0 auto; }
.pd-fb { width: 50px; height: 50px; margin: 0 auto; border-radius: 50%; display: grid; place-items: center; font-size: 16px; font-weight: 700; color: var(--primary); background: var(--primary-soft); }
.pd-name { margin-top: 4px; font-size: 11px; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.pd-cnt { font-size: 14px; font-weight: 800; color: var(--foreground); }
.cd-title { display: flex; align-items: baseline; gap: 6px; font-size: 14px; font-weight: 600; color: var(--foreground); margin: 4px 0 8px; }
.cd-sub { font-size: 10.5px; font-weight: 400; color: var(--muted); }
.cd-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; }
.cd-item { min-width: 0; padding: 7px 4px; text-align: center; border-radius: 10px; background: var(--card); border: 1px solid var(--border); }
.cd-item img { width: 40px; height: 40px; object-fit: contain; display: block; margin: 0 auto; }
.cd-fb { width: 40px; height: 40px; margin: 0 auto; border-radius: 8px; display: grid; place-items: center; font-size: 14px; font-weight: 700; color: var(--primary); background: var(--primary-soft); }
.cd-name { margin-top: 4px; font-size: 10.5px; font-weight: 500; color: var(--foreground); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.cd-cnt { font-size: 11.5px; font-weight: 700; color: var(--muted); }
.cd-more { width: 100%; margin-top: 10px; padding: 9px; border: none; border-radius: 10px; background: var(--bg-hi); color: var(--muted); font-size: 12px; cursor: pointer; }
.cd-more:active { opacity: .8; }
.cd-empty { padding: 26px 0; text-align: center; color: var(--muted); font-size: 13px; }
</style>
