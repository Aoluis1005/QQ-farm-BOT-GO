<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import api from '@/api'
import { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const acc = () => getAccountId()

const groups = ref([])      // 活动组（含 group 标志的 item）
const groupIdx = ref(0)
const panels = ref([])      // [{key,title,icon}]
const panelIdx = ref(0)
const loading = ref(true)
const err = ref('')
const refreshing = ref(false)

// 树中找出的节点引用（供子面板使用）
let giftNode = null
let shopNode = null

// 面板数据
const view = reactive({ season: null, solar: null })
const shopState = reactive({ items: [], bal: 0, cur: '星砂', err: '' })
const giftState = reactive({ nodes: [], summary: {}, day: 0, total: 0, err: '' })
const qmState = reactive({ activity: {}, reward: {}, material: {}, err: '' })

/* ---------- 鹊桥寄情（QiXi） ---------- */
const QIXI_ROOT_ID = 2026081800
const QIXI_INFO_ID = 2026081801
const qixi = reactive({
  tips: null, err: '',
  // 数据芯片（TODO: 08-18 接口活后从 GetGroup 子树动态获取）
  feather: 0, luStock: 0, bridgeDone: 0, bridgeMax: 3, bridgeTarget: 77, sachet: 0, tiers: [],
  // 灵露
  luUsed: 0, luLimit: null, // null=待接口确认
  // 好友列表（手动刷新，避免进tab阻塞线程）
  friends: [], allFriends: [], friendsDisplayCount: 0, friendsPerPage: 10, friendsLoading: false,
  // 被动
  passiveTriggered: 0, passiveLimit: 3
})
// --- 鹊桥：倒计时 ---
const qixiCd = ref('')
let qixiCdTimer = null
function qixiTick() {
  const OPEN = new Date('2026-08-18T00:00:00+08:00').getTime()
  const diff = OPEN - Date.now()
  if (diff <= 0) { qixiCd.value = '🟢 活动已开启'; if (qixiCdTimer) { clearInterval(qixiCdTimer); qixiCdTimer = null } return }
  const s = Math.floor(diff / 1000)
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
  qixiCd.value = `⏳ 距开启 ${String(d).padStart(2, '0')}:${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}
// --- 鹊桥：刷新好友列表 ---
async function refreshQiXiFriends() {
  qixi.friendsLoading = true
    // 获取好友列表（scrollable pagination, 首次加载 10 条）
  try {
    const fd = (await api.get('/api/friends/list')).data
    const list = (fd && fd.ok && fd.data && fd.data.friends) || []
    qixi.allFriends = list.filter(f => f.gid).map(f => ({
      gid: f.gid,
      name: f.nickname || f.name || String(f.gid),
      lands: '?',
      hasCrops: true,
    }))
    qixi.friendsDisplayCount = Math.min(qixi.friendsPerPage, qixi.allFriends.length)
    qixi.friends = qixi.allFriends.slice(0, qixi.friendsDisplayCount)
  } catch (e) {
    console.error('刷新鹊桥好友列表失败', e)
    app.error('好友列表加载失败')
  }
  qixi.friendsLoading = false
}
function loadMoreQiXiFriends() {
  if (qixi.friendsDisplayCount >= qixi.allFriends.length) return
  qixi.friendsDisplayCount = Math.min(qixi.friendsDisplayCount + qixi.friendsPerPage, qixi.allFriends.length)
  qixi.friends = qixi.allFriends.slice(0, qixi.friendsDisplayCount)
}
function onQiXiFriendScroll(e) {
  const el = e.target
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 50) {
    loadMoreQiXiFriends()
  }
}
// --- 鹊桥：Operate 桩（cmd 待 08-18 抓号回填） ---
function qixiOperate(cmdName, cmdConst, payload) {
  const call = { svc: 'ActivityService.Operate', cmd: cmdConst, payload }
  console.log(`[${cmdName}]`, JSON.stringify(call))
  return call
}
async function sprayLu(friend) {
  if (qixi.luStock <= 0) { app.error('灵露已空'); return false }
  const a = acc(); if (!a) return false
  try {
    const { data } = await api.post('/api/activity/qixi/spray', { accountId: a.gid, hostGid: friend.gid })
    if (data && data.ok) {
      const n = data.data.sprayCount || 0
      if (n > 0) {
        app.success(`向 ${friend.name} 喷洒灵露 ×${n} → 鹊羽 +${n}`)
        loadQiXi()
        return true
      }
      app.error(data.data.msg || '该好友无可喷洒地块')
      return false
    }
    app.error((data && data.error) || '喷洒失败')
    return false
  } catch (e) { app.error('喷洒失败'); return false }
}
async function sprayAllLu() {
  if (qixi.luStock <= 0) { app.error('灵露已空'); return }
  const a = acc(); if (!a) return
  try {
    const { data } = await api.post('/api/activity/qixi/spray', { accountId: a.gid })
    if (data && data.ok) {
      const n = data.data.sprayCount || 0
      if (n > 0) app.success(`喷洒成功 ${n} 块地 → 鹊羽 +${n}`)
      else app.error(data.data.msg || '无可喷洒地块')
      loadQiXi()
    } else app.error((data && data.error) || '喷洒失败')
  } catch (e) { app.error('喷洒失败') }
}
// 下一可筑档（档位独立门槛：档1=30/档2=50/档3=77，非累计）
function qixiNextTier() { return (qixi.tiers || []).find(t => !t.claimed) || null }
async function buildBridge() {
  const nt = qixiNextTier()
  if (!nt) { app.error('三档奖励已全部领取'); return }
  if (qixi.feather < nt.consume) { app.error(`鹊羽不足（${qixi.feather}/${nt.consume}）`); return }
  const a = acc(); if (!a) return
  try {
    const { data } = await api.post('/api/activity/qixi/bridge', { accountId: a.gid })
    if (data && data.ok) {
      const rw = data.data.rewards || []
      const names = rw.length ? rw.map(x => `${x.name}×${x.count}`).join('、') : ''
      app.success(`筑桥成功！${names ? '获得：' + names : ''}`)
      loadQiXi()
    } else app.error((data && data.error) || '筑桥失败')
  } catch (e) { app.error('筑桥失败') }
}
async function giftSachetTo(friend) {
  if (qixi.sachet <= 0) { app.error('香囊库存为空'); return }
  const a = acc(); if (!a) return
  try {
    const { data } = await api.post('/api/activity/qixi/gift', { accountId: a.gid, hostGid: friend.gid })
    if (data && data.ok) {
      app.success(`已向 ${friend.name} 赠送香囊 ×1`)
      loadQiXi()
    } else app.error((data && data.error) || '赠送失败')
  } catch (e) { app.error('赠送失败') }
}
function giftSachet() {
  // 兜底：无好友列表时提示从好友列表选择
  app.error('请从好友列表中选择赠送对象')
}
// 去标签
function stripTags(s) { return String(s || '').replace(/<[^>]+>/g, '') }
// 解析 payload.tips：按【标题】分段，含 <br/> 拆条
function parseQiXiTips(payload) {
  try {
    const obj = typeof payload === 'string' ? JSON.parse(payload) : (payload || {})
    const tips = obj.tips
    if (!tips || !Array.isArray(tips.txt)) return null
    let sec = null
    const out = []
    ;(tips.txt || []).forEach(line => {
      if (/【[^】]+】/.test(line)) {
        sec = { title: stripTags(line).replace(/^【|】$/g, ''), items: [] }
        out.push(sec)
      } else if (sec) {
        ;(line.split(/<br\s*\/?>/i)).forEach(p => {
          const t = stripTags(p).trim()
          if (t) sec.items.push(t)
        })
      }
    })
    qixi.tips = { title: tips.title, sections: out }
    qixi.err = ''
    return true
  } catch (e) { return false }
}
async function loadQiXi() {
  const a = acc(); if (!a) return
  qixi.tips = null; qixi.err = ''
  // 首页数据芯片从 /api/activity/qixi 实时获取（鹊羽/灵露=背包301103）
  try {
    const sd = await api.get('/api/activity/qixi', { params: { accountId: a.gid } })
    if (sd.data && sd.data.ok && sd.data.data) {
      const d = sd.data.data
      qixi.feather = n(d.feather)
      qixi.luStock = n(d.luStock)
      qixi.bridgeDone = n(d.bridgeDone)
      qixi.bridgeMax = n(d.bridgeMax) || 3
      qixi.bridgeTarget = n(d.bridgeTarget) || 77
      qixi.sachet = n(d.sachet)
      qixi.luLimit = d.luLimit
      if (d.tiers && d.tiers.length) qixi.tiers = d.tiers
    }
  } catch (e) { /* 数据接口失败不阻塞玩法加载 */ }
  try {
    const { data } = await api.get('/api/activity/group', { params: { id: QIXI_INFO_ID } })
    if (!(data && data.ok)) { qixi.err = (data && data.error) || '加载失败'; return }
    let pl = null
    ;(function walk(x) { if (!x || pl) return; const inf = x.info || {}; if (inf.payload) pl = inf.payload; (x.children || []).forEach(walk) })(data.tree)
    if (!parseQiXiTips(pl)) qixi.err = '玩法数据未就绪（活动 8/18 上线）'
  } catch (e) { qixi.err = '玩法加载失败' }
}

const curPanel = computed(() => panels.value[panelIdx.value] || null)

function n(v) { return v == null ? 0 : (Number(v) || 0) }
// 大数友好缩写：≥1亿→X.XX亿，≥1万→X.X万，否则千分位（避免上亿余额文字过长）
function fmtBig(n) {
  if (n === undefined || n === null || n === '') return 0
  const v = Number(String(n).replace(/,/g, ''))
  if (isNaN(v)) return 0
  if (v >= 1e8) return (v / 1e8).toFixed(2).replace(/\.0+$/, '') + '亿'
  if (v >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return v.toLocaleString()
}

/* ---------- 树遍历：找商店节点（有 exchange_shop）与观星节点（type===13） ---------- */
function findNodes(node) {
  const out = { giftNode: null, shopNode: null }
  ;(function walk(x) {
    if (!x) return
    const inf = x.info || {}
    if (n(inf.type) === 13 && !out.giftNode) out.giftNode = x
    if ((x.exchange_shop && x.exchange_shop.length) && !out.shopNode) out.shopNode = x
    ;(x.children || []).forEach(walk)
  })(node)
  return out
}

/* ---------- 加载活动列表 + 选中组 ---------- */
async function loadActivity() {
  loading.value = true
  err.value = ''
  const a = acc()
  if (!a) { err.value = '请先选择账号'; groups.value = []; panels.value = []; loading.value = false; return }
  try {
    const { data } = await api.get('/api/activity/list', { params: { scope: 'ongoing' } })
    if (!(data && data.ok)) { err.value = (data && data.error) || '加载失败'; groups.value = []; panels.value = []; loading.value = false; return }
    const gs = (data.items || []).filter(i => i.group)
    // 鹊桥寄情作为「活动子tab」常驻（未上线也可查看玩法框架；上线后正常由接口返回，避免重复）
    if (!gs.some(g => String(g.id).indexOf('20260818') === 0)) {
      gs.unshift({ id: QIXI_ROOT_ID, title: '🌉 鹊桥寄情', group: true })
    }
    groups.value = gs
    if (!gs.length) { err.value = '当前没有进行中的活动'; panels.value = []; loading.value = false; return }
    if (groupIdx.value < 0 || groupIdx.value >= gs.length) groupIdx.value = 0
    await loadGroup(gs[groupIdx.value])
    loading.value = false
  } catch (e) {
    err.value = '加载失败'; groups.value = []; panels.value = []; loading.value = false
  }
}

async function selectGroup(i) {
  if (i < 0 || i >= groups.value.length) return
  groupIdx.value = i
  panels.value = []
  panelIdx.value = 0
  await loadGroup(groups.value[i])
}

async function loadGroup(group) {
  loading.value = true
  // 鹊桥寄情：只加载玩法 tips（group?id=1801），不参与 season/shop/gift/solar 常规解析
  if (String(group.id || '').indexOf('20260818') === 0 || (group.title || '').indexOf('鹊') >= 0) {
    try {
      const { data } = await api.get('/api/activity/group', { params: { id: QIXI_INFO_ID } })
      if (!(data && data.ok)) { err.value = (data && data.error) || '加载失败'; panels.value = []; loading.value = false; return }
      let pl = null
      ;(function walk(x) { if (!x || pl) return; const inf = x.info || {}; if (inf.payload) pl = inf.payload; (x.children || []).forEach(walk) })(data.tree)
      if (!parseQiXiTips(pl)) qixi.err = '玩法数据未就绪（活动 8/18 上线）'
      panels.value = [{ key: 'qixi', title: '鹊桥寄情', icon: '🌉' }]
      panelIdx.value = 0
    } catch (e) { err.value = '加载失败'; panels.value = [] }
    loading.value = false
    return
  }
  try {
    const [g, s, o] = await Promise.all([
      api.get('/api/activity/group', { params: { id: group.id } }),
      api.get('/api/activity/season'),
      api.get('/api/activity/solar'),
    ])
    const gd = g.data, sd = s.data, od = o.data
    const tree = (gd && gd.tree) || null
    const season = (sd && sd.ok) ? (sd.data || null) : null
    const solar = (od && od.ok) ? (od.data || null) : null
    view.season = season
    view.solar = solar

    const found = findNodes(tree)
    giftNode = found.giftNode
    shopNode = found.shopNode

    const title = group.title || ''
    const isQingmei = title.indexOf('青酿') >= 0 || title.indexOf('青梅') >= 0
    let pl = []
    if (isQingmei) {
      pl.push({ key: 'qingmei', title: '青梅酿', icon: '🍶' })
    } else {
      if (season && season.passport) pl.push({ key: 'season', title: '千星游记', icon: '🗺️' })
      if (shopNode) pl.push({ key: 'shop', title: '星砂商店', icon: '🛍️' })
      if (giftNode) pl.push({ key: 'gift', title: '观星礼录', icon: '🌟' })
      if (solar && solar.terms && solar.terms.length) pl.push({ key: 'solar', title: '节令小礼', icon: '🌿' })
    }
    panels.value = pl
    if (panelIdx.value < 0 || panelIdx.value >= pl.length) panelIdx.value = 0
    await renderPanel(pl[panelIdx.value])
    loading.value = false
  } catch (e) {
    err.value = '加载失败'; panels.value = []; loading.value = false
  }
}

async function switchPanel(i) {
  if (i < 0 || i >= panels.value.length) return
  panelIdx.value = i
  await renderPanel(panels.value[i])
}

async function renderPanel(p) {
  if (!p) return
  if (p.key === 'shop') await loadShop()
  else if (p.key === 'gift') await loadGift()
  else if (p.key === 'qingmei') await loadQingmei()
}

/* ---------- 刷新获取新活动 ---------- */
async function refresh() {
  if (refreshing.value) return
  refreshing.value = true
  groupIdx.value = 0
  await loadActivity()
  setTimeout(() => { refreshing.value = false }, 1500)
}

/* ---------- 星砂商店 ---------- */
async function loadShop() {
  const id = (shopNode && shopNode.info && shopNode.info.id) || 2026072702
  shopState.items = []; shopState.bal = 0; shopState.err = ''
  try {
    const { data } = await api.get('/api/activity/shop', { params: { id } })
    if (!(data && data.ok)) { shopState.err = (data && data.error) || '加载失败'; return }
    shopState.items = data.items || []
    shopState.bal = n((data.balance || {}).count)
    shopState.cur = (data.balance || {}).currency_name || '星砂'
    shopState.items.forEach(it => { if (it.__qty === undefined) it.__qty = 1 })
  } catch (e) { shopState.err = '商店加载失败' }
}
function shopQty(it, step) {
  const max = Math.min(n(it.exchange_limit), 99)
  let v = Math.max(1, Math.min(max || 99, (n(it.__qty) || 1) + step))
  it.__qty = v
}
async function shopExchange(it) {
  const id = (shopNode && shopNode.info && shopNode.info.id) || 2026072702
  const cnt = n(it.__qty) || 1
  const price = n(it.price)
  if (price * cnt > shopState.bal) { app.error('星砂不足：需 ' + (price * cnt) + '，当前 ' + shopState.bal + ''); return }
  try {
    const { data } = await api.post('/api/activity/shop/exchange', null, { params: { id, slotId: it.id, count: cnt } })
    if (!(data && data.ok)) { app.error('兑换失败：' + ((data && data.error) || '未知错误')); return }
    await loadShop()
  } catch (e) { app.error('兑换失败') }
}

/* ---------- 观星礼录 ---------- */
async function loadGift() {
  const id = (giftNode && giftNode.info && giftNode.info.id) || 2026072701
  giftState.nodes = []; giftState.summary = {}; giftState.err = ''
  try {
    const { data } = await api.get('/api/activity/guanxing', { params: { id } })
    if (!(data && data.ok)) { giftState.err = (data && data.error) || '加载失败'; return }
    const gg = data.data || {}
    giftState.nodes = gg.nodes || []
    giftState.summary = gg.summary || {}
    giftState.day = n(gg.current_day); giftState.total = n(gg.total_days)
  } catch (e) { giftState.err = '观星数据加载失败' }
}
async function claimGift() {
  const id = (giftNode && giftNode.info && giftNode.info.id) || 2026072701
  loading.value = true
  try {
    const { data } = await api.post('/api/activity/guanxing/claim', null, { params: { id } })
    loading.value = false
    if (!(data && data.ok)) { app.error((data && data.error) || '领取失败'); return }
    await loadGift()
  } catch (e) { loading.value = false; app.error('领取失败') }
}

/* ---------- 青梅酿 ---------- */
async function loadQingmei() {
  qmState.activity = {}; qmState.err = ''
  try {
    const { data } = await api.get('/api/activity/qingmei')
    if (!(data && data.ok)) { qmState.err = (data && data.error) || '加载失败'; return }
    qmState.activity = data.activity || {}
    qmState.reward = data.reward || {}
    qmState.material = data.material || {}
  } catch (e) { qmState.err = '青梅活动加载失败' }
}
async function qmClaim() {
  loading.value = true
  try {
    const { data } = await api.post('/api/activity/qingmei/claim')
    loading.value = false
    if (!(data && data.ok)) { app.error((data && data.error) || '领取失败'); return }
    app.success('青梅种子 ×' + n(data.claimed_count) + ' 领取成功')
    await loadQingmei()
  } catch (e) { loading.value = false; app.error('领取失败') }
}
async function qmBrew() {
  loading.value = true
  try {
    const { data } = await api.post('/api/activity/qingmei/wine')
    loading.value = false
    if (!(data && data.ok)) { app.error((data && data.error) || '酿制失败'); return }
    app.success('青梅酿出售成功，金币 +' + n((data.sell || {}).gold) + '')
    await loadQingmei()
  } catch (e) { loading.value = false; app.error('酿制失败') }
}

/* ---------- 千星游记 领取 ---------- */
async function claimSeason() {
  const pp = (view.season && view.season.passport) || {}
  if (n(pp.claimable_levels) <= 0) { app.error('暂无奖励可领取'); return }
  loading.value = true
  try {
    const { data } = await api.post('/api/activity/season/claim')
    if (!(data && data.ok)) { loading.value = false; app.error((data && data.error) || '领取失败'); return }
    const s = (await api.get('/api/activity/season')).data
    view.season = (s && s.ok) ? (s.data || null) : null
    loading.value = false
  } catch (e) { loading.value = false; app.error('领取失败') }
}

/* ---------- 节令小礼 领取 ---------- */
async function claimSolar(t) {
  loading.value = true
  try {
    const { data } = await api.post('/api/activity/solar/claim', null, { params: { termId: t.id } })
    if (!(data && data.ok)) { loading.value = false; app.error((data && data.error) || '领取失败'); return }
    const s = (await api.get('/api/activity/solar')).data
    view.solar = (s && s.ok) ? (s.data || null) : null
    loading.value = false
  } catch (e) { loading.value = false; app.error('领取失败') }
}

/* ---------- 日期格式化（青梅） ---------- */
function fmtDay(s) {
  if (!s) return ''
  const x = new Date(s * 1000)
  return (x.getMonth() + 1) + '月' + x.getDate() + '日'
}

onMounted(() => { loadActivity(); loadQiXi(); qixiTick(); qixiCdTimer = setInterval(qixiTick, 1000) })
</script>

<template>
  <div>
    <h3 style="font-size:20px;font-weight:700;margin:2px 2px 14px">活动中心</h3>

    <!-- 活动组 bar -->
    <div v-if="groups.length" class="dtab-bar" style="display:flex;flex-wrap:wrap;gap:6px;margin-bottom:12px">
      <button class="dtab act-refresh" @click="refresh" :disabled="refreshing">{{ refreshing ? '⏳ 获取中…' : '🔄 获取新活动' }}</button>
      <button
        v-for="(g, i) in groups"
        :key="i"
        class="dtab"
        :class="{ active: i === groupIdx }"
        @click="selectGroup(i)"
      >{{ g.title }}</button>
    </div>

    <!-- 面板 tab -->
    <div v-if="panels.length > 1" class="seg" style="margin-top:12px">
      <button
        v-for="(p, i) in panels"
        :key="p.key"
        class="seg-btn"
        :class="{ active: i === panelIdx }"
        @click="switchPanel(i)"
      >{{ p.icon }} {{ p.title }}</button>
    </div>

    <!-- loading -->
    <div v-if="loading && !panels.length" style="margin-top:12px">
      <div class="placeholder"><div class="big">🌟</div><h3>正在加载活动数据...</h3></div>
    </div>
    <!-- 无活动 -->
    <div v-else-if="!groups.length && err" style="margin-top:12px">
      <div class="act-empty">{{ err }}</div>
    </div>

    <!-- 加载遮罩（领取中） -->
    <div v-if="loading && panels.length" style="margin-top:12px">
      <div class="placeholder"><div class="big">🌟</div><h3>加载中...</h3></div>
    </div>

    <!-- ===== 千星游记 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'season'">
      <template v-if="view.season && view.season.passport">
        <div class="act-card">
          <div class="act-card-hd">
            <h4>🗺️ {{ view.season.passport.title || '千星游记' }}</h4>
            <span class="act-badge">等级 {{ n(view.season.passport.current_level) }}/{{ n(view.season.passport.max_level) }}</span>
          </div>
          <div class="act-stats">
            <span>积分 <b>{{ n(view.season.passport.score) }}</b></span>
            <span>可领 <b>{{ n(view.season.passport.claimable_levels) }} 档</b></span>
            <span>进度 <b>{{ n(view.season.passport.current_progress) }}</b></span>
          </div>
          <div class="bar-track"><div class="bar-fill" :style="{ width: (n(view.season.passport.max_level) > 0 ? Math.min(100, Math.round(n(view.season.passport.current_level) / n(view.season.passport.max_level) * 100)) : 0) + '%' }"></div></div>
          <div class="act-hint">距下一级还需 {{ n(view.season.passport.next_level_need) }} 积分</div>
        </div>
        <div class="act-actions">
          <button class="act-btn" :class="{ disabled: n(view.season.passport.claimable_levels) <= 0 }" :disabled="n(view.season.passport.claimable_levels) <= 0" @click="claimSeason">领取可领奖励 ({{ n(view.season.passport.claimable_levels) }} 档)</button>
        </div>
        <div v-if="(view.season.passport.reward_tiers || []).length" class="act-card">
          <div class="act-card-hd"><h4>🎁 奖励梯度（共 {{ view.season.passport.reward_tiers.length }} 档）</h4></div>
          <div v-for="(t, i) in view.season.passport.reward_tiers" :key="i" class="act-tier">
            <span class="lv">Lv.{{ n(t.level) }}</span>
            <span class="rw">
              <span v-if="!t.free_rewards || !t.free_rewards.length" class="act-rw-empty">—</span>
              <span v-for="(rw, j) in (t.free_rewards || [])" :key="j" class="act-rw">
                <img v-if="rw.image" class="act-ic-sm" :src="rw.image" alt="" loading="lazy" @error="$event.target.remove()">
                <i v-else>🎁</i><em>{{ rw.name || '' }}</em>
              </span>
            </span>
          </div>
        </div>
      </template>
      <div v-else class="act-empty">该活动暂无可展示的面板</div>
    </div>

    <!-- ===== 节令小礼 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'solar'">
      <div v-if="view.solar" class="act-card">
        <div class="act-card-hd"><h4>🌿 节令小礼</h4><span class="act-badge">{{ n(view.solar.claimable_count) }} 可领</span></div>
        <div v-for="(t, i) in (view.solar.terms || [])" :key="i" class="act-term">
          <div class="t-info">
            <b>{{ t.title || ('节气' + t.id) }}</b>
            <span class="act-badge" :class="{ off: n(t.status) !== 2 }">{{ t.status_label || '' }}</span>
          </div>
          <div class="t-rw">
            <span v-if="!t.rewards || !t.rewards.length" class="act-rw-empty">—</span>
            <span v-for="(rw, j) in (t.rewards || [])" :key="j" class="act-rw">
              <img v-if="rw.image" class="act-ic-sm" :src="rw.image" alt="" loading="lazy" @error="$event.target.remove()">
              <i v-else>🎁</i><em>{{ rw.name || '' }}</em>
            </span>
          </div>
        </div>
        <template v-for="(t, i) in (view.solar.terms || [])" :key="'sc' + i">
          <div v-if="n(t.status) === 2" class="act-actions">
            <button class="act-btn" @click="claimSolar(t)">领取 {{ t.title || '' }}</button>
          </div>
        </template>
      </div>
      <div v-else class="act-empty">该活动暂无可展示的面板</div>
    </div>

    <!-- ===== 星砂商店 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'shop'">
      <template v-if="shopState.items.length || shopState.err">
        <div class="act-card">
          <div class="act-card-hd"><h4>🛍️ 星砂商店</h4><span class="act-badge">{{ shopState.items.length }} 件</span></div>
          <div class="act-stats"><span>💰 {{ shopState.cur }}余额 <b>{{ fmtBig(shopState.bal) }}</b></span></div>
        </div>
        <div v-if="shopState.err" class="act-empty">{{ shopState.err }}</div>
        <div v-else-if="!shopState.items.length" class="act-empty">暂无可兑换商品</div>
        <div v-else class="act-grid">
          <div v-for="it in shopState.items" :key="it.id" class="act-item" :class="{ 'act-done': !it.is_repeatable && !!it.owned }">
            <img v-if="it.image" class="act-ic" :src="it.image" alt="" loading="lazy" @error="$event.target.remove()">
            <div v-else class="ic">🌾</div>
            <div class="nm">{{ it.name || ('商品' + it.id) }}</div>
            <div class="ct">💰 {{ n(it.price) }} {{ shopState.cur }}</div>
            <template v-if="it.is_repeatable">
              <div v-if="Math.min(n(it.exchange_limit), 99) <= 0" class="act-badge off">已兑完</div>
              <template v-else>
                <div class="act-qty">
                  <button class="act-btn act-sm" @click="shopQty(it, -1)">−</button>
                  <input class="act-num" :value="it.__qty" min="1" :max="Math.min(n(it.exchange_limit), 99)" inputmode="numeric" @input="it.__qty = Math.max(1, Math.min(Math.min(n(it.exchange_limit), 99), Number($event.target.value) || 1))">
                  <button class="act-btn act-sm" @click="shopQty(it, 1)">＋</button>
                </div>
                <button class="act-btn act-sm" :class="{ disabled: n(it.price) > 0 && shopState.bal < n(it.price) }" :disabled="n(it.price) > 0 && shopState.bal < n(it.price)" @click="shopExchange(it)">兑换</button>
                <div class="act-limit">限购 {{ n(it.exchange_limit) }} 个 · 单价 {{ n(it.price) }} {{ shopState.cur }}</div>
              </template>
            </template>
            <template v-else>
              <span v-if="it.owned" class="act-badge off">已拥有</span>
              <button v-else class="act-btn act-sm" :class="{ disabled: !((!it.owned && n(it.status) !== 3) && (n(it.price) > 0 ? shopState.bal >= n(it.price) : true)) }" :disabled="!((!it.owned && n(it.status) !== 3) && (n(it.price) > 0 ? shopState.bal >= n(it.price) : true))" @click="shopExchange(it)">{{ n(it.status) === 3 ? '已售' : (n(it.price) > 0 ? (shopState.bal >= n(it.price) ? '兑换' : '余额不足') : '兑换') }}</button>
            </template>
          </div>
        </div>
      </template>
      <div v-else class="act-empty">正在加载商品...</div>
    </div>

    <!-- ===== 观星礼录 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'gift'">
      <template v-if="giftState.nodes.length">
        <div class="act-card">
          <div class="act-card-hd"><h4>🌟 观星礼录</h4><span class="act-badge">第 {{ n(giftState.day) }}/{{ n(giftState.total) }} 宿</span></div>
          <div class="act-stats">
            <span>已解锁 <b>{{ n(giftState.summary.unlocked_count) }}</b></span>
            <span>已领取 <b>{{ n(giftState.summary.claimed_count) }}</b></span>
            <span>可领 <b>{{ n(giftState.summary.claimable_count) }}</b></span>
          </div>
        </div>
        <div v-if="n(giftState.summary.claimable_count) > 0" class="act-actions">
          <button class="act-btn" @click="claimGift">✨ 一键领取全部已解锁星宿 ({{ n(giftState.summary.claimable_count) }})</button>
        </div>
        <div class="act-grid">
          <div v-for="nd in giftState.nodes" :key="nd.id" class="act-item" :class="'act-' + (n(nd.claimed) === 1 ? 'done' : (n(nd.claimable) === 1 ? 'go' : (n(nd.unlocked) === 1 ? 'open' : 'lock')))">
            <div class="ic">{{ n(nd.claimed) === 1 ? '✅' : (n(nd.claimable) === 1 ? '⭐' : (n(nd.unlocked) === 1 ? '🔓' : '🔒')) }}</div>
            <div class="nm">{{ nd.name || ('第' + nd.id + '宿') }}</div>
            <div v-if="nd.rewards && nd.rewards.length" class="act-rwbox">
              <span v-for="(rw, j) in nd.rewards" :key="j" class="act-rw">
                <img v-if="rw.image" class="act-ic-sm" :src="rw.image" alt="" loading="lazy" @error="$event.target.remove()">
                <i v-else>🎁</i><em>{{ rw.name || '' }}</em>
              </span>
            </div>
            <span class="act-badge">{{ nd.status_label || '' }}</span>
          </div>
        </div>
      </template>
      <div v-else-if="giftState.err" class="act-empty">{{ giftState.err }}</div>
      <div v-else class="act-empty">加载中...</div>
    </div>

    <!-- ===== 青梅酿 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'qingmei'">
      <template v-if="qmState.activity && (qmState.activity.claimable !== undefined || qmState.err)">
        <div class="act-card">
          <div class="act-card-hd"><h4>🍶 青酿换万金</h4><span class="act-badge">{{ (n(qmState.activity.start_time) && n(qmState.activity.end_time)) ? (fmtDay(qmState.activity.start_time) + ' — ' + fmtDay(qmState.activity.end_time)) : '活动进行中' }}</span></div>
          <div class="act-hint">每日领青梅种子 → 种植/偷菜得青梅 → 酿制并出售青梅酿，可触发价格翻倍暴击</div>
        </div>
        <div class="act-card">
          <div class="act-card-hd"><h4>🌱 每日领种子</h4><span class="act-badge">{{ n(qmState.reward.item_count) }} 颗</span></div>
          <div class="act-stats"><span>奖品 <b>{{ qmState.reward.item_name || '青梅种子' }} ×{{ n(qmState.reward.item_count) }}</b></span></div>
          <div class="act-actions">
            <button class="act-btn" :class="{ disabled: !qmState.activity.claimable }" :disabled="!qmState.activity.claimable" @click="qmClaim">{{ qmState.activity.claimed ? '今日已领取' : (qmState.activity.claimable ? '领取青梅种子' : '今日不可领') }}</button>
          </div>
        </div>
        <div class="act-card">
          <div class="act-card-hd"><h4>🍶 酿制出售</h4><span class="act-badge">青梅 {{ n(qmState.material.item_count) }}</span></div>
          <div class="act-hint">一次消耗现有全部青梅进行多段精酿并出售，获得金币收益</div>
          <div class="act-actions">
            <button class="act-btn" :class="{ disabled: n(qmState.material.item_count) <= 0 }" :disabled="n(qmState.material.item_count) <= 0" @click="qmBrew">酿制并出售青梅酿</button>
          </div>
        </div>
      </template>
      <div v-else-if="qmState.err" class="act-empty">{{ qmState.err }}</div>
      <div v-else class="act-empty">正在加载青梅活动...</div>
    </div>

    <!-- ===== 鹊桥寄情 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'qixi'">
      <!-- Hero -->
      <div class="qixi-hero">
        <h1>🌉 鹊桥寄情</h1>
        <div class="qixi-sub">七夕限定活动 · 活动时间 2026-08-18 ~ 08-22</div>
        <span class="qixi-cd">{{ qixiCd }}</span>
      </div>

      <!-- 数据芯片 -->
      <div class="qixi-chips">
        <div class="qixi-chip gold"><div class="v">{{ qixi.feather }}</div><div class="k">鹊羽</div></div>
        <div class="qixi-chip green"><div class="v">{{ qixi.luStock }}</div><div class="k">鹊羽灵露</div></div>
        <div class="qixi-chip rose"><div class="v">{{ qixi.feather }}/{{ qixiNextTier() ? qixiNextTier().consume : qixi.bridgeTarget }}</div><div class="k">鹊羽收集</div></div>
        <div class="qixi-chip blue"><div class="v">{{ qixi.sachet }}</div><div class="k">鹊羽香囊</div></div>
      </div>

      <!-- 筑鹊桥 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>筑鹊桥 <span class="pill">共 {{ qixi.bridgeMax }} 档</span></div>
        <div class="qixi-bar"><i :style="{ width: (qixiNextTier() ? qixiNextTier().consume : qixi.bridgeTarget) > 0 ? Math.min(100, Math.round(qixi.feather / (qixiNextTier() ? qixiNextTier().consume : qixi.bridgeTarget) * 100)) : 0 + '%' }"></i></div>
        <div class="muted">当前进度 <b style="color:var(--good)">{{ qixi.feather }}/{{ qixiNextTier() ? qixiNextTier().consume : qixi.bridgeTarget }}</b> 鹊羽 · 集满可领取对应档奖励{{ qixiNextTier() ? '（第 ' + (qixi.tiers.indexOf(qixiNextTier()) + 1) + ' 档）' : '（已全部领取）' }}</div>
        <div class="qixi-rewards">
          <div v-if="!qixi.tiers.length" class="qixi-tier qixi-tier-empty">档位奖励加载中…</div>
          <div v-for="(t, i) in qixi.tiers" :key="i" class="qixi-tier">
            <div class="qixi-tier-hd">第 {{ i + 1 }} 档 <span class="pill">消耗 {{ t.consume }} 鹊羽</span><span v-if="t.claimed" class="pill" style="background:var(--good-soft)">已领取</span></div>
            <div class="qixi-tier-rw">
              <span v-for="(rw, j) in (t.rewards || [])" :key="j" class="qixi-rw"><b>{{ rw.name }}</b> ×{{ rw.count }}</span>
            </div>
          </div>
        </div>
        <button class="btn primary block" :disabled="!qixiNextTier() || qixi.feather < qixiNextTier().consume" @click="buildBridge">{{ !qixiNextTier() ? '三档奖励已全部领取' : (qixi.feather >= qixiNextTier().consume ? '筑建鹊桥（第 ' + (qixi.tiers.indexOf(qixiNextTier()) + 1) + ' 档 · 消耗 ' + qixiNextTier().consume + ' 鹊羽）' : '鹊羽不足（' + qixi.feather + '/' + qixiNextTier().consume + '）') }}</button>
      </div>

      <!-- 鹊羽灵露 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>鹊羽灵露 · 主动触发</div>
        <div class="banner">💡 在任意好友/自家地块主动喷洒，<b>恒得 1 根鹊羽</b>，不受变异·成熟度影响。无需挑地，随机用即可。</div>
        <div class="row" style="margin-top:12px">
          <span class="muted">灵露库存 <b style="color:var(--good)">{{ qixi.luStock }}</b> · 今日已用 {{ qixi.luUsed }}/{{ qixi.luLimit !== null ? qixi.luLimit : '?' }} <span class="pill warn" v-if="qixi.luLimit === null">日限待接口确认</span></span>
          <button class="btn ghost" @click="sprayAllLu">🎲 一键随机喷洒</button>
        </div>
        <div class="row" style="margin:4px 0">
          <span class="muted">好友列表（手动刷新，避免进tab阻塞线程）</span>
          <button class="btn small" @click="refreshQiXiFriends" :disabled="qixi.friendsLoading">{{ qixi.friendsLoading ? '⏳ 刷新中…' : '🔄 刷新好友' }}</button>
        </div>
        <div class="qixi-flist" v-if="qixi.friends.length" @scroll="onQiXiFriendScroll">
          <div v-for="(f, i) in qixi.friends" :key="i" class="qixi-frow">
            <div class="qixi-av">{{ f.name[0] }}</div>
            <div style="flex:1"><div class="qixi-fnm">{{ f.name }}</div><div class="st">有作物地块 ×{{ f.lands }}</div></div>
            <button class="btn gold small" @click="sprayLu(f)">用灵露</button>
            <button class="btn primary small" @click="giftSachetTo(f)">送香囊</button>
          </div>
          <div v-if="qixi.friendsDisplayCount < qixi.allFriends.length" class="qixi-loadmore" @click="loadMoreQiXiFriends">
            📜 下滑加载更多（{{ qixi.friendsDisplayCount }}/{{ qixi.allFriends.length }}）
          </div>
        </div>
        <div v-else class="empty" style="text-align:center;padding:14px;color:var(--muted);font-size:12.5px">👥 点击「🔄 刷新好友」加载有可作物地块的好友</div>
      </div>

      <!-- 被动触发 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>收菜被动触发</div>
        <div class="banner">🌾 自家收菜概率出「鹊羽」，<b>每日自动封顶 {{ qixi.passiveLimit }} 次</b>，无需任何操作。今日已触发 <span class="pill">{{ qixi.passiveTriggered }}/{{ qixi.passiveLimit }}</span></div>
      </div>

      <!-- 活动说明 -->
      <details style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px 16px">
        <summary style="cursor:pointer;font-weight:700;font-size:14px;list-style:none">📜 活动说明（接口已放出）</summary>
        <ol style="margin:10px 0 0 18px;font-size:12.5px;color:var(--foreground)">
          <li v-for="(sec, i) in (qixi.tips && qixi.tips.sections || [])" :key="i" style="margin:4px 0">
            <b>{{ sec.title }}</b>
            <ul style="margin:2px 0 2px 14px;padding:0;list-style:disc;color:var(--muted)">
              <li v-for="(it, j) in sec.items" :key="j" style="margin:花粉2px 0">{{ it }}</li>
            </ul>
          </li>
        </ol>
        <div v-if="!(qixi.tips && qixi.tips.sections && qixi.tips.sections.length)" style="color:var(--muted);font-size:12.5px">
          {{ qixi.tips ? '玩法细则待 8/18 更新' : (qixi.err || '正在加载玩法...') }}
        </div>
      </details>

      <div class="foot" style="text-align:center;font-size:11px;color:var(--muted);margin-top:4px">数据为占位示意 · 协议桩 cmd 待 08-18 抓号回填</div>
    </div>

    <div v-else-if="curPanel" class="act-empty">该活动暂无可展示的面板</div>
  </div>
</template>

<style scoped>
/* ===== 鹊桥寄情 ===== */
.qixi-hero {
  background: linear-gradient(135deg, #ff7eb3 0%, #e23a8a 100%);
  color: #fff;
  border-radius: 14px;
  padding: 20px;
  margin-bottom: 14px;
}
[data-theme="dark"] .qixi-hero {
  background: linear-gradient(135deg, #c44d7a 0%, #a82868 100%);
}
.qixi-hero h1 { font-size: 22px; display: flex; align-items: center; gap: 8px; }
.qixi-sub { opacity: 0.92; font-size: 13px; margin-top: 6px; }
.qixi-cd {
  display: inline-block;
  margin-top: 10px;
  background: rgba(255,255,255,.22);
  padding: 3px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 600;
}

/* 数据芯片 */
.qixi-chips {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
  margin-bottom: 4px;
}
.qixi-chip {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 12px;
  text-align: center;
}
.qixi-chip .v { font-size: 20px; font-weight: 700; }
.qixi-chip .k { font-size: 11px; color: var(--muted); margin-top: 2px; }
.qixi-chip.gold .v { color: var(--warn); }
.qixi-chip.rose .v { color: var(--danger); }
.qixi-chip.green .v { color: var(--good); }
.qixi-chip.blue .v { color: var(--primary); }
.qixi-chip-hint {
  text-align: center;
  font-size: 11px;
  color: var(--muted);
  margin-bottom: 14px;
}

/* 筑桥 */
.qixi-bar {
  height: 10px;
  background: var(--primary-soft);
  border-radius: 999px;
  overflow: hidden;
  margin: 10px 0;
}
.qixi-bar i {
  display: block;
  height: 100%;
  background: linear-gradient(90deg, var(--primary), var(--primary-2));
  border-radius: 999px;
}
.qixi-rewards {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 10px 0;
}
.qixi-tier {
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 8px 10px;
}
.qixi-tier-empty {
  color: var(--muted);
  text-align: center;
  padding: 14px 10px;
  font-size: 12.5px;
}
.qixi-tier-hd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  font-weight: 600;
  margin-bottom: 6px;
}
.qixi-tier-rw {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.qixi-rw {
  background: var(--primary-soft);
  border-radius: 8px;
  padding: 5px 9px;
  font-size: 12px;
}
.qixi-rw b { font-weight: 600; margin-right: 2px; }

/* 好友列表 */
.qixi-flist { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; max-height: 360px; overflow-y: auto; -webkit-overflow-scrolling: touch; }
.qixi-loadmore { text-align: center; padding: 10px; font-size: 12px; color: var(--muted); cursor: pointer; border-top: 1px solid var(--border); }
.qixi-frow {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border: 1px solid var(--border);
  border-radius: 10px;
}
.qixi-av {
  width: 34px; height: 34px;
  border-radius: 50%;
  background: linear-gradient(135deg, var(--primary-2), var(--primary));
  color: var(--on-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 14px;
  flex: none;
}
.qixi-fnm { font-size: 13.5px; font-weight: 600; }

/* 通用元素（在鹊桥面板作用域内定义） */
.ttl { font-size: 15px; font-weight: 700; display: flex; align-items: center; gap: 7px; margin-bottom: 12px; }
.ttl .dot { width: 8px; height: 8px; border-radius: 50%; background: var(--primary); }
.banner {
  background: var(--primary-soft);
  border-radius: 10px;
  padding: 11px 13px;
  font-size: 12.5px;
  color: var(--primary);
  display: flex;
  align-items: center;
  gap: 8px;
}
.pill {
  display: inline-block;
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 999px;
  padding: 2px 9px;
  font-size: 11px;
  font-weight: 700;
  color: var(--good);
}
.pill.warn { background: none; color: var(--warn); border-color: var(--warn); }
.card {
  background: var(--card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 16px;
  margin-bottom: 14px;
  box-shadow: 0 1px 3px rgba(17,24,39,.05);
}
.btn {
  border: none;
  border-radius: 10px;
  padding: 9px 14px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
}
.btn.primary { background: var(--primary); color: var(--on-primary); }
.btn.primary:disabled { opacity: 0.5; cursor: not-allowed; }
.btn.ghost { background: var(--primary-soft); color: var(--primary); }
.btn.gold { background: var(--warn); color: #fff; }
.btn.block { width: 100%; }
.btn.small { padding: 6px 12px; font-size: 11px; }
.row { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
.muted { color: var(--muted); font-size: 12.5px; }
.st { font-size: 11px; color: var(--muted); }
.empty { text-align: center; padding: 14px; color: var(--muted); font-size: 12.5px; }
.foot { text-align: center; font-size: 11px; color: var(--muted); margin-top: 4px; }
</style>
