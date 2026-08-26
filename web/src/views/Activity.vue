<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
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
const QIXI_END = new Date('2026-08-22T23:59:59+08:00').getTime() // 活动截止（北京时间）；到期后前端自动隐藏，不再硬塞进活动列表
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

/* ---------- 雨落成诗（YuLu / WeatherBottleUI） ---------- */
const YULU_ROOT_ID = 2026070300
const YULU_OPEN = 1787709600 * 1000                 // 2026-08-26 10:00 北京时间
const YULU_END = new Date('2026-09-08T23:59:59+08:00').getTime()
const yulu = reactive({
  badge: null, badgeNote: '', badgeImage: '',
  weather: null,                                   // {id,name,active} 当前农场天气
  items: {},                                   // {id: {count,name,image}}  全部来自背包实时
  research: { tiers: [], claimedAll: false, note: '', claimed: new Set() }, // claimed 记录已领取的 nodeId
  exchangedOn: null, dayTick: 0, // 最近一次兑换天气瓶的日期字符串 / 跨0点刷新触发
  friends: [], allFriends: [], friendsDisplayCount: 0, friendsPerPage: 5, friendsLoading: false,
  oneClickRunning: false, oneClickTotal: 0, oneClickDone: 0, oneClickOk: 0,
  err: '',
})
// 天气瓶分组（id 取自 ItemInfo 实锤，非猜）
const YULU_SELF = [5002, 5003, 5007]              // 给自己用：召唤/变异/开箱
const YULU_FRIEND = [5001, 5004, 5005, 5006]      // 好友向：采集/引雷/青蛙/乌云
const YULU_PRODUCT = [5008, 5009, 5010]           // 产出与奖励
const YULU_TOP = [5001, 5002, 5003, 5004, 5005, 5006, 5007] // 顶部 7 瓶统计
// 一键 tab → 物品 id
const YULU_ONECLICK = { collect: 5001, frog: 5005, cloud: 5006, thunder: 5004 }

function yuluImg(id) { const it = yulu.items[id]; return it ? it.image : '' }
function yuluCount(id) { const it = yulu.items[id]; return it ? n(it.count) : 0 }
// 活动物品中文名（活动物品不在游戏 itemInfoMap 内，后端也兜底返回，这里再兜一层确保不显示纯 ID）
const YULU_NAMES = { 5001:'天气采集瓶', 5002:'雷雨召唤瓶', 5003:'闪电变异瓶', 5004:'霹雳引雷瓶', 5005:'青蛙使坏瓶', 5006:'乌云使坏瓶', 5007:'百宝惊喜瓶', 5008:'雷纹礼盒', 5009:'雷击木', 5010:'黄金雷击木' }
function yuluName(id) { const it = yulu.items[id]; return (it && it.name) || YULU_NAMES[id] || ('物品' + id) }

// 倒计时
const yuluCd = ref('')
let yuluCdTimer = null
function yuluTick() {
  const diff = YULU_OPEN - Date.now()
  if (diff <= 0) { yuluCd.value = '🟢 活动已开启'; if (yuluCdTimer) { clearInterval(yuluCdTimer); yuluCdTimer = null } return }
  const s = Math.floor(diff / 1000)
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
  yuluCd.value = `⏳ 距开启 ${String(d).padStart(2, '0')}:${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}

// 加载状态（顶部 8 统计 + 气象研究占位）
async function loadYulu() {
  const a = acc(); if (!a) return
  yulu.err = ''
  try {
    const { data } = await api.get('/api/activity/yulu', { params: { accountId: a.gid } })
    if (data && data.ok && data.data) {
      const d = data.data
      yulu.badge = d.badge
      yulu.badgeNote = d.badgeNote || ''
      yulu.badgeImage = d.badgeImage || ''
      yulu.weather = d.weather || null
      yulu.items = d.items || {}
      yulu.research = Object.assign({ claimed: new Set() }, d.research || {})
    }
  } catch (e) { yulu.err = '加载失败' }
}

// 好友列表（手动刷新，避免进 tab 阻塞）
async function refreshYuluFriends() {
  yulu.friendsLoading = true
  try {
    const fd = (await api.get('/api/friends/list')).data
    const list = (fd && fd.ok && fd.data && fd.data.friends) || []
    yulu.allFriends = list.filter(f => f.gid).map(f => ({
      gid: f.gid,
      name: f.nickname || f.name || String(f.gid),
      lands: '?',
      hasCrops: true,
    }))
    yulu.friendsDisplayCount = Math.min(yulu.friendsPerPage, yulu.allFriends.length)
    yulu.friends = yulu.allFriends.slice(0, yulu.friendsDisplayCount)
  } catch (e) { app.error('好友列表加载失败') }
  yulu.friendsLoading = false
}
function loadMoreYuluFriends() {
  if (yulu.friendsDisplayCount >= yulu.allFriends.length) return
  yulu.friendsDisplayCount = Math.min(yulu.friendsDisplayCount + yulu.friendsPerPage, yulu.allFriends.length)
  yulu.friends = yulu.allFriends.slice(0, yulu.friendsDisplayCount)
}
function onYuluFriendScroll(e) {
  const el = e.target
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 50) loadMoreYuluFriends()
}

// 动作
async function yuluOpen(itemId) {
  const a = acc(); if (!a) return
  if (yuluCount(itemId) <= 0) { app.error(`${yuluName(itemId)} 库存为空`); return }
  try {
    const { data } = await api.post('/api/activity/yulu/open', { accountId: a.gid, itemId })
    if (data && data.ok) { app.success(`打开成功：${yuluName(itemId)}`); loadYulu() }
    else app.error((data && data.error) || '打开失败')
  } catch (e) { app.error('打开失败') }
}
async function yuluMutate() {
  const a = acc(); if (!a) return
  if (yuluCount(5003) <= 0) { app.error('闪电变异瓶库存为空'); return }
  try {
    const { data } = await api.post('/api/activity/yulu/mutate', { accountId: a.gid })
    if (data && data.ok) {
      const cnt = (data.data && data.data.mutateCount) || 0
      if (cnt > 0) app.success(`闪电变异 ${cnt} 块地`)
      else app.error((data.data && data.data.msg) || '无可变异地块')
      loadYulu()
    } else app.error((data && data.error) || '变异失败')
  } catch (e) { app.error('变异失败') }
}
async function yuluUseOnce(itemId, friend) {
  const a = acc(); if (!a) return null
  try {
    const body = { accountId: a.gid, itemId }
    if (friend) body.hostGid = friend.gid
    const { data } = await api.post('/api/activity/yulu/use', body)
    return data
  } catch (e) { return { ok: false, error: '网络错误' } }
}
async function yuluUse(itemId, friend) {
  const a = acc(); if (!a) return
  if (yuluCount(itemId) <= 0) { app.error(`${yuluName(itemId)} 库存为空`); return }
  const data = await yuluUseOnce(itemId, friend)
  if (!data) { app.error('使用失败'); return }
  if (data.ok) {
    const cnt = (data.data && data.data.useCount) || 0
    if (cnt > 0) app.success(`${yuluName(itemId)} 使用成功 ${cnt} 块地`)
    else app.error((data.data && data.data.msg) || '无可作用地块')
    loadYulu()
  } else app.error((data && data.error) || '使用失败')
}
// 气象研究：研究节点图标（按 nodeId）
const YULU_RES_ICON = { 1000:'🌦️', 1001:'🎁', 1002:'🐸', 1003:'☁️', 1004:'🌩️', 1005:'🧪', 1006:'⚡', 1007:'⚡', 1008:'🖼️' }
// 研究树分层（按前置 prevs 计算层级，起点 1000 = 深度0）
function rsLevels() {
  const tiers = yulu.research.tiers || []
  const depth = {}
  depth[1000] = 0
  let changed = true, guard = 0
  while (changed && guard++ < 20) {
    changed = false
    tiers.forEach(t => {
      const ps = t.prevs || []
      if (ps.every(p => depth[p] !== undefined)) {
        const d = Math.max(0, ...ps.map(p => depth[p] + 1))
        if (depth[t.nodeId] === undefined || d > depth[t.nodeId]) { depth[t.nodeId] = d; changed = true }
      }
    })
  }
  const max = Math.max(0, ...Object.values(depth))
  const levels = Array.from({ length: max + 1 }, () => [])
  tiers.forEach(t => { levels[depth[t.nodeId] ?? 0].push(t) })
  return levels
}
function rsClaimed(nodeId) { return !!(yulu.research.claimed && yulu.research.claimed.has(nodeId)) }
function rsUnlockable(nodeId) {
  const t = (yulu.research.tiers || []).find(x => x.nodeId === nodeId)
  if (!t) return false
  return (t.prevs || []).every(p => rsClaimed(p))
}
function rsClick(t) {
  if (rsClaimed(t.nodeId)) return
  if (!rsUnlockable(t.nodeId)) { app.error('需先领取前置档位'); return }
  yuluResearch(t.nodeId)
}
function nodeStyle(t) {
  if (rsClaimed(t.nodeId)) return { borderColor: '#4caf7a', opacity: 1 }
  if (!rsUnlockable(t.nodeId)) return { opacity: .5 }
  return { borderColor: '#6ea8ff' }
}
// 兑换收集天气瓶：金豆(1005)×200 → 天气采集瓶(5001)×1，每自然日限 1 个
function yuluToday() { const d = new Date(); return d.getFullYear() + '-' + (d.getMonth() + 1) + '-' + d.getDate() }
function yuluExchangedToday() { void yulu.dayTick; return yulu.exchangedOn === yuluToday() }
async function yuluExchange() {
  const a = acc(); if (!a) return
  try {
    const { data } = await api.post('/api/activity/yulu/exchange', null, { params: { accountId: a.gid } })
    if (data && data.ok) { yulu.exchangedOn = yuluToday(); app.success('兑换成功：天气采集瓶 ×1'); loadYulu() }
    else {
      const e = (data && data.error) || '兑换失败'
      if (e.includes('今日已兑换')) yulu.exchangedOn = yuluToday()
      app.error(e)
    }
  } catch (e) { app.error('兑换失败') }
}
async function yuluResearch(nodeId) {
  const a = acc(); if (!a) return
  try {
    const { data } = await api.post('/api/activity/yulu/research', { accountId: a.gid, nodeId })
    if (data && data.ok) {
      const nm = data.data && data.data.reward ? `${data.data.reward}×${data.data.count}` : '研究奖励'
      app.success('领取成功：' + nm)
      if (!yulu.research.claimed) yulu.research.claimed = new Set()
      yulu.research.claimed.add(nodeId)
      loadYulu()
    } else {
      app.error((data && data.error) || '领取失败')
    }
  } catch (e) { app.error('领取失败') }
}
// 一键：对该账号全部好友【顺序】逐个使用（单并发，避开同账号并发 Enter 多农场冲突）。
// 全程 async await，不阻塞 UI 线程；按钮禁用 + 进度显示。
const sleep = ms => new Promise(r => setTimeout(r, ms))
const YULU_ONECLICK_MAX = 5    // 一次最多进好友农场数（防掉线）
const YULU_ONECLICK_GAP = 400  // 好友间间隔 ms（顺序降频，对齐 Node 顺延时）
// 一键：每次最多处理 5 位好友，好友间加间隔防掉线，道具使用完立即停止。
async function yuluOneClick(kind) {
  if (yulu.oneClickRunning) { app.error('上一次一键尚未完成'); return }
  if (!yulu.allFriends.length) { app.error('请先点击「🔄 刷新好友」加载好友'); return }
  const itemId = YULU_ONECLICK[kind]
  if (!itemId) return
  if (yuluCount(itemId) <= 0) { app.error(`${yuluName(itemId)} 库存为空`); return }
  yulu.oneClickRunning = true
  const target = yulu.allFriends.slice(0, YULU_ONECLICK_MAX)
  yulu.oneClickTotal = target.length
  yulu.oneClickDone = 0
  yulu.oneClickOk = 0
  let stopped = ''
  for (const f of target) {
    if (yuluCount(itemId) <= 0) { stopped = `${yuluName(itemId)} 已用完，停止`; break }
    const data = await yuluUseOnce(itemId, f)
    yulu.oneClickDone++
    if (data && data.ok) {
      const cnt = (data.data && data.data.useCount) || 0
      if (cnt > 0) yulu.oneClickOk++
    }
    await loadYulu()                       // 刷新背包，判断道具剩余
    await sleep(YULU_ONECLICK_GAP)         // 好友间间隔防掉线
    if (yuluCount(itemId) <= 0) { stopped = `${yuluName(itemId)} 已用完，停止`; break }
  }
  yulu.oneClickRunning = false
  if (stopped) app.error(stopped)
  else if (yulu.oneClickOk === 0) app.error('一键完成：无可作用地块或使用失败')
  else app.success(`一键完成：成功 ${yulu.oneClickOk}/${yulu.oneClickTotal} 位好友`)
}

/* ---------- 公益小红花（占位面板，后端待 9/1 开服实现） ---------- */
const HONGHUA_OPEN = 1788192000 * 1000                 // 2026-09-01 00:00 北京时间
const HONGHUA_ROOT_ID = 2026090900
const HONGHUA_END = 1788969599 * 1000                  // 2026-09-09 23:59:59 北京时间
const honghuaCd = ref('')
let honghuaCdTimer = null
function honghuaTick() {
  const diff = HONGHUA_OPEN - Date.now()
  if (diff <= 0) { honghuaCd.value = '🟢 活动已开启'; if (honghuaCdTimer) { clearInterval(honghuaCdTimer); honghuaCdTimer = null } return }
  const s = Math.floor(diff / 1000)
  const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60
  honghuaCd.value = `⏳ 距开启 ${String(d).padStart(2, '0')}:${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(sec).padStart(2, '0')}`
}
const honghua = reactive({
  seeds: null, fruits: null, love: null, fund: null,
  serverFund: null, claimed: false, note: '', err: '',
  tiers: [
    { i: 1, rw: [{ t: '🧪 有机化肥（8小时）×1' }] },
    { i: 2, rw: [{ t: '🪙 点券 ×50' }] },
    { i: 3, rw: [{ t: '🧪 有机化肥（8小时）×2' }] },
    { i: 4, rw: [{ t: '🪙 点券 ×100', n: 2 }] },
    { i: 5, rw: [{ t: '🖼️ 公益小红花做好事头像框 ×1' }] },
  ],
})
// 占位：后端 /api/activity/honghua 未实现，成功后回填；失败仅记 note 不弹错
async function loadHonghua() {
  const a = acc(); if (!a) return
  honghua.err = ''; honghua.note = ''
  try {
    const { data } = await api.get('/api/activity/honghua', { params: { accountId: a.gid } })
    if (data && data.ok && data.data) {
      const d = data.data
      honghua.seeds = d.seeds; honghua.fruits = d.fruits
      honghua.love = d.love; honghua.fund = d.fund
      if (d.tiers && d.tiers.length) honghua.tiers = d.tiers
      honghua.serverFund = d.serverFund; honghua.claimed = !!d.claimed
    } else honghua.note = (data && data.error) || ''
  } catch (e) { honghua.note = '后端接口待实现（9/1 开服后上线）' }
}
function honghuaTodo() { app.info('活动 9月1日 开启后接入真实协议') }

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
    let gs = (data.items || []).filter(i => i.group)
    // 鹊桥寄情兜底：仅活动期内，若接口未返回（开发期/接口缺失）才硬塞进列表；活动结束后不再插入，由接口决定生命周期
    if (Date.now() < QIXI_END && !gs.some(g => String(g.id).indexOf('20260818') === 0)) {
      gs.unshift({ id: QIXI_ROOT_ID, title: '🌉 鹊桥寄情', group: true })
    }
    // 雨落成诗：列表返回 根+5子（均 20260703xx），折叠成单条入口；开服前则兜底插入便于预览
    const yuluMatched = gs.filter(g => String(g.id).indexOf('20260703') === 0 || (g.title || '').indexOf('雨落') >= 0)
    if (yuluMatched.length) {
      const rep = yuluMatched.find(g => String(g.id) === '2026070300') || yuluMatched[0]
      gs = gs.filter(g => !(String(g.id).indexOf('20260703') === 0 || (g.title || '').indexOf('雨落') >= 0))
      gs.unshift({ id: rep.id, title: '🌧️ 雨落成诗', group: true })
    } else if (Date.now() < YULU_END) {
      gs.unshift({ id: YULU_ROOT_ID, title: '🌧️ 雨落成诗', group: true })
    }
    // 公益小红花：未开始（upcoming）不进 ongoing 列表，开服前兜底插入便于预览占位面板
    const honghuaMatched = gs.filter(g => String(g.id).indexOf('20260909') === 0 || (g.title || '').indexOf('小红花') >= 0)
    if (honghuaMatched.length) {
      const rep = honghuaMatched.find(g => String(g.id) === String(HONGHUA_ROOT_ID)) || honghuaMatched[0]
      gs = gs.filter(g => !(String(g.id).indexOf('20260909') === 0 || (g.title || '').indexOf('小红花') >= 0))
      gs.unshift({ id: rep.id, title: '🌸 公益小红花', group: true })
    } else if (Date.now() < HONGHUA_END) {
      gs.unshift({ id: HONGHUA_ROOT_ID, title: '🌸 公益小红花', group: true })
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
  // 雨落成诗：只加载状态（不参与常规 season/shop/gift/solar 解析）
  if (String(group.id || '').indexOf('20260703') === 0 || (group.title || '').indexOf('雨落') >= 0) {
    panels.value = [{ key: 'yulu', title: '雨落成诗', icon: '🌧️' }]
    panelIdx.value = 0
    loading.value = false
    return
  }
  // 公益小红花：占位面板（后端待 9/1 开服实现）
  if (String(group.id || '').indexOf('20260909') === 0 || (group.title || '').indexOf('小红花') >= 0 || (group.title || '').indexOf('公益') >= 0) {
    panels.value = [{ key: 'honghua', title: '公益小红花', icon: '🌸' }]
    panelIdx.value = 0
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
  else if (p.key === 'yulu') { await loadYulu() }
  else if (p.key === 'honghua') { await loadHonghua() }
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

// 切号事件：用新账号重拉活动列表与鹊桥/雨落面板（热切换）
const onSwitched = () => { loadActivity(); loadQiXi(); loadYulu() }
let yuluDayTimer = null
onMounted(() => { loadActivity(); loadQiXi(); loadYulu(); qixiTick(); qixiCdTimer = setInterval(qixiTick, 1000); yuluTick(); yuluCdTimer = setInterval(yuluTick, 1000); honghuaTick(); honghuaCdTimer = setInterval(honghuaTick, 1000); yuluDayTimer = setInterval(() => { yulu.dayTick = Date.now() }, 60000); window.addEventListener('account-switched', onSwitched) })
onUnmounted(() => { if (qixiCdTimer) { clearInterval(qixiCdTimer); qixiCdTimer = null }; if (yuluCdTimer) { clearInterval(yuluCdTimer); yuluCdTimer = null }; if (honghuaCdTimer) { clearInterval(honghuaCdTimer); honghuaCdTimer = null }; if (yuluDayTimer) { clearInterval(yuluDayTimer); yuluDayTimer = null }; window.removeEventListener('account-switched', onSwitched) })
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

    <!-- ===== 雨落成诗 ===== -->
    <div v-else-if="curPanel && curPanel.key === 'yulu'">
      <!-- Hero -->
      <div class="yulu-hero">
        <h1>🌧️ 雨落成诗</h1>
        <div class="yulu-sub">雷雨限定活动 · 2026-08-26 ~ 09-08 · 当前天气：{{ (yulu.weather && yulu.weather.name) || '--' }}</div>
        <span class="yulu-cd">{{ yuluCd }}</span>
      </div>

      <!-- 顶部 8 统计 chip（雷电徽章 + 5001~5007） -->
      <div class="yulu-chips">
        <div class="yulu-chip badge">
          <img class="yulu-chip-ico" v-if="yulu.badgeImage" :src="yulu.badgeImage" alt="" @error="$event.target.remove()">
          <div class="v">{{ yulu.badge == null ? '—' : yulu.badge }}</div>
          <div class="k">雷电徽章</div>
        </div>
        <div class="yulu-chip" v-for="id in YULU_TOP" :key="id">
          <img class="yulu-chip-ico" v-if="yuluImg(id)" :src="yuluImg(id)" alt="" @error="$event.target.remove()">
          <div class="v">{{ yuluCount(id) }}</div>
          <div class="k">{{ yuluName(id) }}</div>
        </div>
      </div>

      <!-- 给自己用的天气瓶 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>给自己用的天气瓶</div>
        <div class="yulu-self" v-for="id in YULU_SELF" :key="id">
          <img class="yulu-s-ico" v-if="yuluImg(id)" :src="yuluImg(id)" alt="" @error="$event.target.remove()">
          <div class="yulu-s-body">
            <div class="yulu-s-name">{{ yuluName(id) }}</div>
            <div class="muted">库存 <b style="color:var(--good)">{{ yuluCount(id) }}</b></div>
            <div class="row" style="margin-top:8px">
              <button v-if="id===5002" class="btn primary small" @click="yuluUse(5002)">雷雨召唤</button>
              <button v-else-if="id===5003" class="btn primary small" @click="yuluMutate">闪电变异（自家）</button>
              <button v-else-if="id===5007" class="btn gold small" @click="yuluOpen(5007)">打开</button>
            </div>
          </div>
        </div>
      </div>

      <!-- 好友互动 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>好友互动 <span class="pill">雷雨好友农场</span></div>
        <div class="yulu-onetabs">
          <div class="yulu-onetab collect" :class="{ busy: yulu.oneClickRunning }" @click="yuluOneClick('collect')"><div class="oi">🫧</div>一键采集</div>
          <div class="yulu-onetab frog" :class="{ busy: yulu.oneClickRunning }" @click="yuluOneClick('frog')"><div class="oi">🐸</div>一键青蛙</div>
          <div class="yulu-onetab cloud" :class="{ busy: yulu.oneClickRunning }" @click="yuluOneClick('cloud')"><div class="oi">☁️</div>一键乌云</div>
          <div class="yulu-onetab light" :class="{ busy: yulu.oneClickRunning }" @click="yuluOneClick('thunder')"><div class="oi">⚡</div>一键引雷</div>
        </div>
        <div v-if="yulu.oneClickRunning" class="yulu-oc-progress">
          ⏳ 一键进行中… {{ yulu.oneClickDone }}/{{ yulu.oneClickTotal }}（成功 {{ yulu.oneClickOk }}）
        </div>
        <div class="row" style="margin:4px 0">
          <span class="muted">好友列表（手动刷新；一键按钮对全部好友生效）</span>
          <button class="btn small" @click="refreshYuluFriends" :disabled="yulu.friendsLoading">{{ yulu.friendsLoading ? '⏳ 刷新中…' : '🔄 刷新好友' }}</button>
        </div>
        <div class="yulu-flist" v-if="yulu.friends.length" @scroll="onYuluFriendScroll">
          <div v-for="(f, i) in yulu.friends" :key="i" class="yulu-frow">
            <div class="yulu-av">{{ f.name[0] }}</div>
            <div style="flex:1;min-width:0"><div class="yulu-fnm">{{ f.name }}</div><div class="st">好友农场</div></div>
            <div class="yulu-fbtns">
              <button class="btn ghost small" :disabled="yulu.oneClickRunning" @click="yuluUse(5001, f)">采集</button>
              <button class="btn ghost small" :disabled="yulu.oneClickRunning" @click="yuluUse(5004, f)">引雷</button>
              <button class="btn ghost small" :disabled="yulu.oneClickRunning" @click="yuluUse(5005, f)">青蛙</button>
              <button class="btn ghost small" :disabled="yulu.oneClickRunning" @click="yuluUse(5006, f)">乌云</button>
            </div>
          </div>
          <div v-if="yulu.friendsDisplayCount < yulu.allFriends.length" class="yulu-loadmore" @click="loadMoreYuluFriends">
            📜 下滑加载更多（{{ yulu.friendsDisplayCount }}/{{ yulu.allFriends.length }}）
          </div>
        </div>
        <div v-else class="empty" style="text-align:center;padding:14px;color:var(--muted);font-size:12.5px">👥 点击「🔄 刷新好友」加载好友</div>
      </div>

      <!-- 产出与奖励 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>产出与奖励</div>
        <div class="yulu-self" v-for="id in YULU_PRODUCT" :key="id">
          <img class="yulu-s-ico" v-if="yuluImg(id)" :src="yuluImg(id)" alt="" @error="$event.target.remove()">
          <div class="yulu-s-body">
            <div class="yulu-s-name">{{ yuluName(id) }}</div>
            <div class="muted">库存 <b style="color:var(--good)">{{ yuluCount(id) }}</b></div>
            <div class="row" style="margin-top:8px">
              <button v-if="id===5008" class="btn gold small" @click="yuluOpen(5008)">打开</button>
              <span v-else class="pill warn">产物 · 暂不支持一键出售</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 气象研究 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>气象研究
          <span class="pill" style="float:right;margin-top:2px">⚡ 雷电徽章 {{ yulu.badge ?? 0 }}</span>
        </div>
        <div class="muted" style="margin:0 0 8px">每档需先把前置档位领取后才解锁，点击档位解锁兑换奖励。</div>
        <div style="display:flex;flex-direction:row;align-items:center;overflow-x:auto;padding-bottom:2px">
          <template v-for="(lv, li) in rsLevels()" :key="li">
            <div v-if="li>0" style="color:var(--muted,#888);padding:0 4px;font-size:13px">➜</div>
            <div style="display:flex;flex-direction:column;gap:6px;align-items:center">
              <button v-for="t in lv" :key="t.nodeId"
                :style="Object.assign({display:'flex',flexDirection:'column',alignItems:'center',width:'76px',padding:'7px 4px',borderRadius:'12px',border:'1.5px solid var(--border,#3a3f55)',background:'var(--card,#1c2238)',cursor:'pointer'}, nodeStyle(t))"
                @click="rsClick(t)">
                <span style="font-size:22px;line-height:1">{{ YULU_RES_ICON[t.nodeId] || '🎁' }}</span>
                <b style="font-size:10px;color:var(--foreground,#eee);margin-top:3px">{{ t.reward }}</b>
                <span style="font-size:9px;color:var(--good,#5ad18a);margin-top:2px">×{{ t.count }}</span>
                <span v-if="rsClaimed(t.nodeId)" style="font-size:9px;color:var(--good,#5ad18a);margin-top:3px">✅ 已领取</span>
                <span v-else-if="!rsUnlockable(t.nodeId)" style="font-size:9px;color:var(--muted,#999);margin-top:3px">🔒 未解锁</span>
                <span v-else style="font-size:9px;color:var(--primary,#6ea8ff);margin-top:3px">⚡{{ t.cost }} 解锁</span>
              </button>
            </div>
          </template>
        </div>
        <div class="muted" style="margin:8px 0 0;font-size:11.5px">使用天气瓶 / 收获闪电变异作物得雷电徽章，推进研究领奖；天气瓶活动结束后可出售换金币。</div>
      </div>

      <!-- 兑换收集天气瓶 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>兑换收集天气瓶</div>
        <div style="display:flex;align-items:center;gap:10px;margin:2px 0 8px">
          <div style="display:flex;flex-direction:column;align-items:center;justify-content:center;width:64px;height:64px;border-radius:14px;background:var(--card,#1c2238);border:1.5px solid var(--border,#3a3f55)">
            <span style="font-size:26px;line-height:1">🌦️</span>
            <b style="font-size:10px;color:var(--foreground,#eee);margin-top:4px">天气采集瓶</b>
          </div>
          <div style="display:flex;flex-direction:column;gap:4px;flex:1">
            <div style="font-size:12.5px;color:var(--foreground,#eee)">消耗 <b style="color:var(--warn,#ffb547)">金豆 ×200</b> → 天气采集瓶 ×1</div>
            <div class="muted" style="font-size:11px">每自然日限兑 1 个，兑换后可在好友雷雨农场使用。</div>
          </div>
        </div>
        <div class="act-actions">
          <button class="btn primary block" :disabled="yuluExchangedToday()"
            :style="yuluExchangedToday() ? { opacity: .6 } : {}"
            @click="yuluExchange">{{ yuluExchangedToday() ? '✅ 今日已兑换 · 0点后恢复' : '💰 兑换天气采集瓶（金豆×200）' }}</button>
        </div>
      </div>

      <!-- 活动说明 -->
      <details style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px 16px">
        <summary style="cursor:pointer;font-weight:700;font-size:14px;list-style:none">📜 活动说明</summary>
        <ol style="margin:10px 0 0 18px;font-size:12.5px;color:var(--foreground)">
          <li>雷雨天气下，作物可<b>闪电变异</b>（1/2 品除外），变异后售价 ×4。</li>
          <li>完成「使用天气采集瓶 / 雷雨召唤瓶 / 收获闪电变异作物」得<b>雷电徽章</b>，推进气象研究领奖。</li>
          <li>天气采集瓶去<b>雷雨好友农场</b>使用，必得雷雨召唤瓶 ×1。</li>
          <li>雷雨召唤瓶：自己农场召唤 20 分钟雷雨。</li>
          <li>霹雳引雷瓶 / 青蛙使坏瓶 / 乌云使坏瓶：在<b>好友农场</b>使用触发互动（引雷双方得雷纹礼盒、使坏得经验）。</li>
          <li>天气瓶限时，活动结束后可出售换金币。</li>
        </ol>
        <div class="muted" style="margin-top:8px;font-size:11.5px">数据芯片实时读背包；雷电徽章与气象研究档位待 8/26 开服抓包回填。</div>
      </details>
    </div>

    <!-- ===== 公益小红花（占位，后端待 9/1 开服实现） ===== -->
    <div v-else-if="curPanel && curPanel.key === 'honghua'">
      <!-- Hero -->
      <div class="honghua-hero">
        <div class="hh-flower">🌸</div>
        <h1>公益小红花</h1>
        <div class="honghua-sub">种下一朵小红花，帮助乡村学童吃上热腾腾的免费午餐。</div>
        <div class="honghua-meta">
          <span class="honghua-pill">活动时间</span>
          <span class="honghua-date">2026.09.01 — 09.09</span>
        </div>
        <span class="honghua-cd">{{ honghuaCd }}</span>
      </div>

      <!-- 玩法链路 -->
      <div class="hh-flow">
        <div class="hh-step"><div class="fi">📋</div><div class="ft">每日任务/分享</div><div class="fd">获取小红花种子</div></div>
        <div class="hh-arr">›</div>
        <div class="hh-step"><div class="fi">🌱</div><div class="ft">种植收获</div><div class="fd">种下并收获果实</div></div>
        <div class="hh-arr">›</div>
        <div class="hh-step"><div class="fi">💖</div><div class="ft">获得爱心值</div><div class="fd">捐赠助力公益</div></div>
        <div class="hh-arr">›</div>
        <div class="hh-step"><div class="fi">🤝</div><div class="ft">送出公益金</div><div class="fd">1元公益助力</div></div>
      </div>

      <!-- 顶部数据 -->
      <div class="hh-chips">
        <div class="hh-chip"><div class="ci">🌱</div><div class="v">{{ honghua.seeds == null ? '—' : honghua.seeds }}</div><div class="k">小红花种子</div></div>
        <div class="hh-chip"><div class="ci">🌸</div><div class="v">{{ honghua.fruits == null ? '—' : honghua.fruits }}</div><div class="k">小红花果实</div></div>
        <div class="hh-chip"><div class="ci">💖</div><div class="v">{{ honghua.love == null ? '—' : honghua.love }}</div><div class="k">爱心值</div></div>
        <div class="hh-chip"><div class="ci">🤝</div><div class="v">{{ honghua.fund == null ? '—' : honghua.fund }}</div><div class="k">公益金</div></div>
      </div>

      <!-- 爱心值 + 送出公益金 -->
      <div class="hh-love">
        <div class="row">
          <div><div class="hh-lv">{{ honghua.love == null ? 0 : honghua.love }}<small> 爱心值</small></div></div>
          <span class="pill warn" v-if="honghua.love == null">待开服</span>
        </div>
        <div class="bar"><i style="width:0%"></i></div>
        <div class="muted" style="margin-bottom:10px">累计爱心值达到档位即可解锁领取奖励</div>
        <button class="btn gold" @click="honghuaTodo">💛 送出公益金 · 1元助力</button>
        <div class="note">单用户活动期仅可获得 1 次公益金资格；不支持提现、兑换、转让、售卖；全服公益金拨付总额上限 200 万元。</div>
      </div>

      <!-- 每日任务 + 公益礼包 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>每日任务 · 公益礼包</div>
        <div class="banner">完成<b>每日任务</b>或<b>每日分享</b>即可获得<b>小红花种子</b>；种植并收获<b>小红花果实</b>可获得对应爱心值。</div>
        <div class="hh-tier" style="margin-top:10px">
          <div class="hh-tier-hd">🎁 公益礼包 <span class="pill">每日限领 1 次</span></div>
          <div class="hh-tier-rw"><span class="hh-rw">🧪 化肥（1小时）×2</span></div>
          <div class="row" style="margin-top:9px">
            <span class="muted">每日收获小红花后可领取</span>
            <button class="btn primary small" @click="honghuaTodo">领取</button>
          </div>
        </div>
      </div>

      <!-- 个人爱心值档位 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>个人爱心值档位奖励</div>
        <div class="hh-tier" v-for="t in honghua.tiers" :key="t.i">
          <div class="hh-tier-hd">档位 {{ t.i }} <template v-if="t.i === 5">🏆</template></div>
          <div class="hh-tier-rw">
            <span class="hh-rw" v-for="(r, ri) in t.rw" :key="ri">{{ r.t }}<template v-if="r.n > 1"> ×{{ r.n }}</template></span>
          </div>
          <div class="row" style="margin-top:8px">
            <span class="muted">爱心值达标</span>
            <button class="btn small" :class="t.i === 5 ? 'primary' : 'ghost'" @click="honghuaTodo">领取</button>
          </div>
        </div>
      </div>

      <!-- 全服公益目标 -->
      <div class="card">
        <div class="ttl"><span class="dot"></span>全服公益目标</div>
        <div class="banner">全服玩家共同达成公益目标后，满足参与条件的玩家即可领取<b>全服公益结算礼包</b>（单角色限领 1 次）。</div>
        <div class="bar" style="margin-top:12px"><i style="width:0%"></i></div>
        <div class="muted" style="margin:0 0 10px">全服进度 · 待开服统计</div>
        <div class="hh-tier" style="margin-top:0">
          <div class="hh-tier-hd">🎁 全服公益结算礼包</div>
          <div class="hh-tier-rw">
            <span class="hh-rw">📦 化肥礼包 ×20</span>
            <span class="hh-rw">🫘 金豆豆 ×20</span>
            <span class="hh-rw">🪙 点券 ×300</span>
          </div>
          <div class="row" style="margin-top:9px">
            <span class="muted">全服目标达成后开放</span>
            <button class="btn gold small" @click="honghuaTodo">领取</button>
          </div>
        </div>
      </div>

      <!-- 活动说明 -->
      <details style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px 16px">
        <summary style="cursor:pointer;font-weight:700;font-size:14px;list-style:none">📜 活动说明</summary>
        <ol style="margin:10px 0 0 18px;font-size:12.5px;color:var(--foreground)">
          <li>QQ经典农场联合<b>腾讯公益</b>（运营方：腾讯公益慈善基金会）、腾讯成长守护、免费午餐基金推出，帮助乡村学童吃上免费午餐。</li>
          <li>完成每日任务/每日分享获得<b>小红花种子</b>，种植收获<b>小红花果实</b>获得爱心值。</li>
          <li>捐赠爱心值助力公益，获得<b>公益金资格</b>，点击「送出公益金」完成 1 元助力，公益金全额拨付至公益机构。</li>
          <li>公益金不支持提现/兑换/转让/售卖，单用户仅 1 次资格，全服拨付总额上限 200 万元。</li>
          <li>奖励分<b>公益礼包</b>、<b>个人爱心值档位</b>、<b>全服公益结算礼包</b>三类。</li>
        </ol>
        <div class="muted" style="margin-top:8px;font-size:11.5px" v-if="honghua.note">{{ honghua.note }}</div>
        <div class="muted" style="margin-top:8px;font-size:11.5px">占位面板 · 后端接口待 9/1 开服实现，届时接入真实数据。</div>
      </details>
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

/* ===== 雨落成诗 ===== */
.yulu-hero {
  background: linear-gradient(135deg, #3194CB 0%, #1f5e9e 55%, #2a3f8f 100%);
  color: #fff;
  border-radius: 14px;
  padding: 20px;
  margin-bottom: 14px;
}
[data-theme="dark"] .yulu-hero {
  background: linear-gradient(135deg, #1c5a82 0%, #143f6e 55%, #1d2c63 100%);
}
.yulu-hero h1 { font-size: 22px; display: flex; align-items: center; gap: 8px; }
.yulu-sub { opacity: 0.92; font-size: 13px; margin-top: 6px; }
.yulu-cd {
  display: inline-block; margin-top: 10px;
  background: rgba(255,255,255,.22); padding: 3px 10px;
  border-radius: 999px; font-size: 12px; font-weight: 600;
}
/* 顶部 chip */
.yulu-chips { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin-bottom: 14px; }
.yulu-chip { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 10px 4px; text-align: center; }
.yulu-chip.badge { background: linear-gradient(135deg, rgba(49,148,203,.22), rgba(207,255,0,.10)); border-color: rgba(49,148,203,.4); }
.yulu-chip .v { font-size: 17px; font-weight: 800; color: var(--primary-2); font-variant-numeric: tabular-nums; }
.yulu-chip .k { font-size: 10.5px; color: var(--muted); margin-top: 3px; line-height: 1.2; }
.yulu-chip-ico { width: 24px; height: 24px; object-fit: contain; margin: 0 auto 4px; display: block; }
/* 给自己用 / 产出 */
.yulu-self { display: flex; gap: 10px; align-items: flex-start; padding: 11px 0; border-bottom: 1px solid var(--border); }
.yulu-self:last-child { border-bottom: none; }
.yulu-s-ico { width: 38px; height: 38px; border-radius: 10px; background: rgba(127,127,127,.14); object-fit: contain; flex-shrink: 0; }
.yulu-s-body { flex: 1; min-width: 0; }
.yulu-s-name { font-size: 13.5px; font-weight: 700; }
/* 一键 tab */
.yulu-onetabs { display: grid; grid-template-columns: repeat(4, 1fr); gap: 7px; margin: 4px 0 12px; }
.yulu-onetab { border: 1px solid var(--border); border-radius: 11px; padding: 9px 2px; text-align: center; cursor: pointer; background: var(--card); color: var(--foreground); font-size: 12px; font-weight: 700; }
.yulu-onetab .oi { width: 22px; height: 22px; line-height: 22px; font-size: 16px; margin: 0 auto 2px; }
.yulu-onetab.collect { background: linear-gradient(135deg, rgba(49,148,203,.22), rgba(49,148,203,.08)); border-color: rgba(49,148,203,.35); }
.yulu-onetab.frog { background: linear-gradient(135deg, rgba(79,208,127,.20), rgba(79,208,127,.07)); border-color: rgba(79,208,127,.35); }
.yulu-onetab.cloud { background: linear-gradient(135deg, rgba(142,162,200,.22), rgba(142,162,200,.07)); border-color: rgba(142,162,200,.35); }
.yulu-onetab.light { background: linear-gradient(135deg, rgba(207,255,0,.18), rgba(207,255,0,.06)); border-color: rgba(207,255,0,.3); }
.yulu-onetab.busy { opacity: .55; cursor: progress; pointer-events: none; }
.yulu-oc-progress { font-size: 12px; color: var(--primary); margin: 0 0 8px; font-weight: 600; }
/* 好友列表 */
.yulu-flist { display: flex; flex-direction: column; gap: 8px; margin-top: 8px; max-height: 360px; overflow-y: auto; -webkit-overflow-scrolling: touch; }
.yulu-loadmore { text-align: center; padding: 10px; font-size: 12px; color: var(--muted); cursor: pointer; border-top: 1px solid var(--border); }
.yulu-frow { display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: 1px solid var(--border); border-radius: 10px; }
.yulu-av { width: 34px; height: 34px; border-radius: 50%; background: linear-gradient(135deg, var(--primary-2), var(--primary)); color: var(--on-primary); display: flex; align-items: center; justify-content: center; font-weight: 700; font-size: 14px; flex: none; }
.yulu-fnm { font-size: 13.5px; font-weight: 600; }
.yulu-fbtns { display: flex; gap: 5px; flex-shrink: 0; }
.yulu-fbtns .btn { padding: 5px 8px; font-size: 11px; }
/* ===== 公益小红花 ===== */
.honghua-hero { border-radius: 18px; padding: 20px 18px; margin-bottom: 14px; position: relative; overflow: hidden;
  background: linear-gradient(135deg, #f04e56 0%, #e8484f 45%, #c62f48 100%); color: #fff; }
.honghua-hero .hh-flower { font-size: 30px; filter: drop-shadow(0 3px 6px rgba(0,0,0,.18)); }
.honghua-hero h1 { font-size: 23px; margin: 6px 0 0; letter-spacing: 1px; }
.honghua-sub { opacity: .95; font-size: 12.5px; margin-top: 7px; line-height: 1.5; }
.honghua-meta { display: flex; gap: 8px; align-items: center; margin-top: 12px; }
.honghua-pill { font-size: 11px; padding: 3px 10px; border-radius: 999px; background: rgba(255,255,255,.2); font-weight: 700; }
.honghua-date { font-size: 12.5px; font-weight: 700; }
.honghua-cd { display: inline-block; margin-top: 10px; background: rgba(255,255,255,.22); padding: 5px 12px;
  border-radius: 999px; font-size: 12px; font-weight: 700; font-variant-numeric: tabular-nums; }
.hh-flow { display: flex; align-items: stretch; gap: 4px; margin-bottom: 14px; }
.hh-step { flex: 1; background: var(--card); border: 1px solid var(--border); border-radius: 13px; padding: 10px 3px; text-align: center; }
.hh-step .fi { font-size: 20px; }
.hh-step .ft { font-size: 11px; font-weight: 700; margin-top: 4px; }
.hh-step .fd { font-size: 9.5px; color: var(--muted); margin-top: 2px; line-height: 1.3; }
.hh-arr { align-self: center; color: var(--primary); font-weight: 800; font-size: 14px; padding: 0 1px; }
.hh-chips { display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; margin-bottom: 14px; }
.hh-chip { border-radius: 13px; padding: 11px 4px; text-align: center; background: var(--card); border: 1px solid var(--border); }
.hh-chip .ci { font-size: 20px; }
.hh-chip .v { font-size: 16px; font-weight: 800; color: var(--primary); font-variant-numeric: tabular-nums; margin-top: 2px; }
.hh-chip .k { font-size: 10.5px; color: var(--muted); margin-top: 3px; }
.hh-love { background: linear-gradient(135deg, rgba(232,72,79,.10), rgba(255,180,0,.08));
  border: 1px solid rgba(232,72,79,.35); border-radius: 16px; padding: 16px; margin-bottom: 13px; }
.hh-lv { font-size: 34px; font-weight: 900; color: var(--primary); font-variant-numeric: tabular-nums; line-height: 1; }
.hh-lv small { font-size: 13px; font-weight: 700; color: var(--muted); }
.note { font-size: 11px; color: var(--muted); margin-top: 8px; line-height: 1.5; }
.hh-tier { border: 1px solid var(--border); border-radius: 13px; padding: 11px 12px; margin-top: 9px; background: var(--card); }
.hh-tier-hd { font-size: 13px; font-weight: 700; display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
.hh-tier-rw { margin-top: 7px; display: flex; gap: 7px; flex-wrap: wrap; }
.hh-rw { font-size: 12px; background: rgba(127,127,127,.14); border-radius: 8px; padding: 3px 9px; color: var(--foreground); }
</style>
