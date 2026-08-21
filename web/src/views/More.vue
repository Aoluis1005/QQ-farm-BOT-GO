<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import api from '@/api'
import { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'
import { useRouter } from 'vue-router'

const app = useAppStore()
const router = useRouter()
const acc = () => getAccountId()

/* ================= 主 tab ================= */
const active = ref('m-auto')     // m-auto / m-strategy / m-default / m-codex / m-analysis
const defaultPane = ref('d-strategy')  // d-strategy / d-auto
const anTab = ref('crops')       // crops / blacklist / strategy

const ALL_LANDS = ['purple', 'gold', 'black', 'red', 'normal']
const hourOpts = Array.from({ length: 24 }, (_, i) => String(i).padStart(2, '0') + ':00')

/* ================= 自动控制开关（m-auto 与 d-auto 共用结构） ================= */
const CORE_AUTO = ['farm', 'task', 'sell', 'friend', 'farm_push', 'land_upgrade', 'fertilizer_gift', 'fertilizer_buy_normal', 'fertilizer_buy_organic', 'skip_own_weed_bug', 'golden_bug_clear']
const FRIEND_AUTO = ['friend_steal', 'friend_help', 'friend_bad', 'friend_golden_bug', 'friend_help_exp_limit']
const AUTO_LABELS = {
  farm: '自动种植收获', task: '自动做任务', sell: '自动卖果实', friend: '自动好友互动',
  farm_push: '推送触发巡田', land_upgrade: '自动升级土地', fertilizer_gift: '自动填充化肥',
  fertilizer_buy_normal: '自动购买无机化肥', fertilizer_buy_organic: '自动购买有机化肥',
  skip_own_weed_bug: '不除自己草虫', golden_bug_clear: '自动清除黄金虫',
  friend_steal: '自动偷菜', friend_help: '自动帮忙', friend_bad: '自动捣乱', friend_golden_bug: '自动放黄金虫',
  friend_help_exp_limit: '经验满只帮护主犬', friend_turbo_mode: '极速务农(只帮护主犬)', fertilizer_multi_season: '多季补肥',
}

function freshAut() {
  // 默认开关（多数自动化默认开启）
  return {
    farm: true, task: true, sell: true, friend: true, farm_push: true, land_upgrade: true,
    fertilizer_gift: false, fertilizer_buy_normal: false, fertilizer_buy_organic: false,
    skip_own_weed_bug: true, golden_bug_clear: true,
    friend_steal: true, friend_help: true, friend_bad: false, friend_golden_bug: false,
    friend_help_exp_limit: true, friend_turbo_mode: false, fertilizer_multi_season: true,
  }
}
const autCfg = reactive(freshAut())    // m-auto
const dAutCfg = reactive(freshAut())   // d-auto

function toggleSwitch(cfg, key) { cfg[key] = !cfg[key] }

/* ================= 施肥策略（账号级 m-auto） ================= */
const mA = reactive({ friendMinLevel: 0, fertLandTypes: [], fertStrategy: 'smart_normal', fertSeconds: 300 })
/* ================= 施肥策略（默认方案 d-auto） ================= */
const dA = reactive({ friendMinLevel: 0, fertLandTypes: [], fertStrategy: 'smart_normal', fertSeconds: 300 })
/* ================= 神秘商人状态（账号级，align legacy __curMystery） ================= */
const myst = reactive({ autoBuy: false, currencies: [] })

function landOn(arr, land) { return !arr.length || arr.includes(land) }
function pushLand(arr, land) {
  let cur = arr.length ? arr.slice() : ALL_LANDS.slice()
  if (cur.includes(land)) cur = cur.filter(x => x !== land)
  else cur.push(land)
  arr.splice(0, arr.length, ...cur)
}

/* ================= 种植策略状态（m-strategy 与 d-strategy 共用） ================= */
function freshStrategy() {
  return {
    strategy: 'max_exp', fallback: 'level', preferredSeed: 0, bagLandTypes: [],
    twoX2: false, farmMin: 5, farmMax: 10, helpMin: 3, helpMax: 5, stealMin: 3, stealMax: 10,
    quietEnabled: false, quietStart: '01:00', quietEnd: '07:30', plantRandom: false, plantDelay: 2, stealDelay: 1,
  }
}
const mS = reactive(freshStrategy())
const dS = reactive(freshStrategy())
const mPreview = ref('加载中…')
const dPreview = ref('加载中…')
// 极速务农定时时段（start/end 用 "HH:mm"，保存时组 "HH:mm-HH:mm" 下发后端 friend_turbo_schedule_time）
const mTurbo = reactive({ scheduled: false, start: '08:00', end: '10:00' })
const dTurbo = reactive({ scheduled: false, start: '08:00', end: '10:00' })

/* ================= 背包种子（/api/bag/seeds）顺序 ================= */
const mSeeds = ref([])           // 背包实际种子 [{seedId,name,count,requiredLevel,plantSize}]
const seedsCache = ref([])       // /api/seeds（全部种子，用于优选下拉）
const userLevel = ref(0)
const mPriority = ref([])        // 背包种子优先顺序（seedId 数组）
const dPriority = ref([])

/* ================= 默认方案 ================= */
const dEn = ref(true)
const dUpdatedAt = ref(0)
const dExists = ref(false)
const dSavedText = computed(() => {
  if (!dExists.value) return '尚未保存默认方案'
  const ts = Number(dUpdatedAt.value) || 0
  return ts ? '最后保存：' + new Date(ts).toLocaleString('zh-CN', { hour12: false }) : '最后保存：--'
})

/* ================= 保存按钮 loading ================= */
const saving = ref('')
// 加载标志：自动控制开关在真实配置加载完成前不渲染，避免默认值先渲染成"关"、
// 加载完再跳"开"的过渡动画（切页回来时开关从关跳开的闪烁问题）
const autLoaded = ref(false)
const dAutLoaded = ref(false)

/* ================= 数据加载：账号级设置 ================= */
function applyAut(cfg, obj) {
  Object.keys(obj).forEach(k => { if (k in cfg) cfg[k] = !!obj[k] })
}
async function loadAccountMore() {
  if (!acc()) return
  try {
    const { data } = await api.get('/api/settings')
    const d = data && data.data
    if (!d) return
    const aut = d.automation || {}
    applyAut(autCfg, aut)
    const ts = aut.friend_turbo_schedule_time || ''
    mTurbo.scheduled = !!aut.friend_turbo_scheduled
    if (ts.includes('-')) {
      const [s, e] = ts.split('-')
      mTurbo.start = s || '08:00'
      mTurbo.end = e || '10:00'
    }
    mA.friendMinLevel = d.autoAcceptFriendMinLevel ?? 0
    mA.fertLandTypes = Array.isArray(aut.fertilizer_land_types) ? aut.fertilizer_land_types.slice() : []
    mA.fertStrategy = aut.fertilizer || 'smart_normal'
    mA.fertSeconds = aut.fertilizer_smart_seconds ?? 300
    myst.autoBuy = !!aut.mystery_auto_buy
    myst.currencies = (d.mysteryAutoBuyCurrencies || []).map(Number)

    mS.strategy = d.plantingStrategy || 'max_exp'
    mS.fallback = d.bagSeedFallbackStrategy || 'level'
    mS.bagLandTypes = Array.isArray(d.bagPriorityLandTypes) ? d.bagPriorityLandTypes.slice() : []
    mS.twoX2 = !!d.prioritize2x2Crops
    const iv = d.intervals || {}
    mS.farmMin = iv.farmMin ?? 5; mS.farmMax = iv.farmMax ?? 10
    mS.helpMin = iv.helpMin ?? 3; mS.helpMax = iv.helpMax ?? 5
    mS.stealMin = iv.stealMin ?? 3; mS.stealMax = iv.stealMax ?? 10
    const qh = d.friendQuietHours || {}
    mS.quietEnabled = !!qh.enabled
    mS.quietStart = qh.start || '01:00'
    mS.quietEnd = qh.end || '07:30'
    mS.plantRandom = !!d.plantOrderRandom
    mS.plantDelay = d.plantDelaySeconds ?? 2
    mS.stealDelay = d.stealDelaySeconds ?? 1
    mPriority.value = Array.isArray(d.bagSeedPriority) ? d.bagSeedPriority.slice() : []
    await fetchUserLevel()
    computePreview('m')
    autLoaded.value = true
  } catch (e) { autLoaded.value = true }
}

async function loadDefaultPlan() {
  if (!acc()) return
  try {
    const { data } = await api.get('/api/settings/default-plan')
    if (data && data.ok) {
      const dd = data.data
      const cfg = (dd && dd.config) || {}
      dEn.value = dd ? dd.enabled !== false : true
      dUpdatedAt.value = Number(dd && dd.updatedAt) || 0
      dExists.value = !!(dd && dd.exists)
      dS.strategy = cfg.plantingStrategy || 'max_exp'
      dS.fallback = cfg.bagSeedFallbackStrategy || 'level'
      dS.bagLandTypes = Array.isArray(cfg.bagPriorityLandTypes) ? cfg.bagPriorityLandTypes.slice() : []
      dS.twoX2 = !!cfg.prioritize2x2Crops
      const iv = cfg.intervals || {}
      dS.farmMin = iv.farmMin ?? 5; dS.farmMax = iv.farmMax ?? 10
      dS.helpMin = iv.helpMin ?? 3; dS.helpMax = iv.helpMax ?? 5
      dS.stealMin = iv.stealMin ?? 3; dS.stealMax = iv.stealMax ?? 10
      const qh = cfg.friendQuietHours || {}
      dS.quietEnabled = !!qh.enabled
      dS.quietStart = qh.start || '01:00'
      dS.quietEnd = qh.end || '07:30'
      dS.plantRandom = !!cfg.plantOrderRandom
      dS.plantDelay = cfg.plantDelaySeconds ?? 2
      dS.stealDelay = cfg.stealDelaySeconds ?? 1
      const aut = cfg.automation || {}
      applyAut(dAutCfg, aut)
      const dts = aut.friend_turbo_schedule_time || ''
      dTurbo.scheduled = !!aut.friend_turbo_scheduled
      if (dts.includes('-')) {
        const [s, e] = dts.split('-')
        dTurbo.start = s || '08:00'
        dTurbo.end = e || '10:00'
      }
      dAutLoaded.value = true
      dA.friendMinLevel = cfg.autoAcceptFriendMinLevel ?? 0
      dA.fertLandTypes = Array.isArray(aut.fertilizer_land_types) ? aut.fertilizer_land_types.slice() : []
      dA.fertStrategy = aut.fertilizer || 'smart_normal'
      dA.fertSeconds = aut.fertilizer_smart_seconds ?? 300
      dPriority.value = Array.isArray(cfg.bagSeedPriority) ? cfg.bagSeedPriority.slice() : []
      if (!userLevel.value) await fetchUserLevel()
      computePreview('d')
    }
  } catch (e) { /* silent */ }
}

/* ================= 种子 & 等级 ================= */
async function loadSeeds() {
  if (!acc()) return
  try {
    const { data } = await api.get('/api/bag/seeds')
    mSeeds.value = (data && data.ok && Array.isArray(data.data)) ? data.data : []
  } catch (e) { mSeeds.value = [] }
}
async function getSeedsCache() {
  if (!acc()) return []
  if (!seedsCache.value.length) {
    try {
      const { data } = await api.get('/api/seeds')
      seedsCache.value = (data && data.ok && Array.isArray(data.data)) ? data.data : []
    } catch (e) { seedsCache.value = [] }
  }
  return seedsCache.value
}
async function fetchUserLevel() {
  if (!acc()) { userLevel.value = 0; return 0 }
  try {
    const { data } = await api.get('/api/home/profile')
    userLevel.value = Number((data.data && data.data.level) || 0)
  } catch (e) { userLevel.value = 0 }
  return userLevel.value
}

/* ================= 策略预览（m/d 通用） ================= */
function computePreview(p) {
  const s = p === 'm' ? mS : dS
  const ref_ = p === 'm' ? mPreview : dPreview
  ref_.value = '加载中…'
  const effective = s.strategy === 'bag_priority' ? (s.fallback || 'level') : s.strategy
  if (effective === 'preferred') {
    getSeedsCache().then(seeds => {
      const seed = s.preferredSeed ? seeds.find(x => x.seedId === s.preferredSeed) : null
      ref_.value = seed ? ((seed.requiredLevel || 0) + '级 ' + (seed.name || ('种子' + seed.seedId))) : '未选择优先种子'
    })
    return
  }
  getSeedsCache().then(seeds => {
    const lvl = userLevel.value
    const available = seeds.filter(x => (x.requiredLevel || 0) <= lvl)
    if (!available.length) { ref_.value = '暂无可用种子'; return }
    if (effective === 'level') {
      const best = available.slice().sort((a, b) => (b.requiredLevel || 0) - (a.requiredLevel || 0))[0]
      ref_.value = best ? ((best.requiredLevel || 0) + '级 ' + (best.name || ('种子' + best.seedId))) : '暂无可用种子'
      return
    }
    const sortKey = { max_exp: 'exp', max_fert_exp: 'fert', max_profit: 'profit', max_fert_profit: 'fert_profit' }[effective]
    if (!sortKey) { ref_.value = '暂无可用种子'; return }
    api.get('/api/analytics', { params: { sort: sortKey } }).then(({ data }) => {
      const ranks = (data && data.ok && Array.isArray(data.data)) ? data.data : []
      const ids = new Set(available.map(x => x.seedId))
      const match = ranks.find(r => ids.has(Number(r.seedId || 0)))
      if (match) {
        const seed = available.find(x => x.seedId === Number(match.seedId || 0))
        ref_.value = seed ? ((seed.requiredLevel || 0) + '级 ' + (seed.name || (''))) : '暂无匹配种子'
      } else ref_.value = '暂无匹配种子'
    }).catch(() => { ref_.value = '暂无匹配种子' })
  })
}

watch(() => [mS.strategy, mS.fallback, mS.preferredSeed], () => computePreview('m'))
watch(() => [dS.strategy, dS.fallback, dS.preferredSeed], () => computePreview('d'))

/* ================= 背包种子顺序列表 ================= */
function seedRows(priority) {
  const byId = new Map(mSeeds.value.map(s => [s.seedId, s]))
  const ordered = (priority).map(id => byId.get(id)).filter(Boolean)
  const rest = mSeeds.value
    .filter(s => !ordered.includes(s))
    .slice()
    .sort((a, b) => (b.requiredLevel || 0) - (a.requiredLevel || 0)) // 未设置优先级的按等级降序，列表不再乱
  const items = [...ordered, ...rest]
  return items.map((s, i) => ({
    s, i, first: i === 0,
    last: i === items.length - 1,
    twoX2: n(s.plantSize) === 2,
  }))
}
function n(v) { return v == null ? 0 : (Number(v) || 0) }
function moveSeed(which, seedId, dir) {
  const list = which === 'm' ? mPriority.value : dPriority.value
  const order = list.length ? list.slice() : mSeeds.value.map(s => s.seedId)
  const idx = order.indexOf(seedId)
  const to = idx + dir
  if (idx < 0 || to < 0 || to >= order.length) return
  const tmp = order[idx]; order[idx] = order[to]; order[to] = tmp
  if (which === 'm') mPriority.value = order
  else dPriority.value = order
}
function resetSeeds(which) {
  const order = mSeeds.value.map(s => s.seedId)
  if (which === 'm') mPriority.value = order
  else dPriority.value = order
}

/* ================= 收集 + 保存 ================= */
function collectAuto() {
  const aut = { ...autCfg }
  aut.friend_turbo_scheduled = mTurbo.scheduled
  aut.friend_turbo_schedule_time = mTurbo.scheduled ? mTurbo.start + '-' + mTurbo.end : ''
  aut.fertilizer_land_types = mA.fertLandTypes.slice()
  aut.fertilizer = mA.fertStrategy
  aut.fertilizer_smart_seconds = mA.fertSeconds
  aut.mystery_auto_buy = myst.autoBuy
  return aut
}
function collectStrategy() {
  return {
    plantingStrategy: mS.strategy,
    bagSeedFallbackStrategy: mS.fallback,
    bagPriorityLandTypes: mS.bagLandTypes.length ? mS.bagLandTypes.slice() : ALL_LANDS.slice(),
    prioritize2x2Crops: mS.twoX2,
    intervals: { farmMin: mS.farmMin, farmMax: mS.farmMax, helpMin: mS.helpMin, helpMax: mS.helpMax, stealMin: mS.stealMin, stealMax: mS.stealMax },
    friendQuietHours: { enabled: mS.quietEnabled, start: mS.quietStart, end: mS.quietEnd },
    plantOrderRandom: mS.plantRandom,
    plantDelaySeconds: mS.plantDelay,
    stealDelaySeconds: mS.stealDelay,
    bagSeedPriority: mPriority.value.slice(),
    preferredSeedId: mS.preferredSeed,
  }
}
function collectDefault() {
  const aut = { ...dAutCfg }
  aut.friend_turbo_scheduled = dTurbo.scheduled
  aut.friend_turbo_schedule_time = dTurbo.scheduled ? dTurbo.start + '-' + dTurbo.end : ''
  aut.fertilizer_land_types = dA.fertLandTypes.slice()
  aut.fertilizer = dA.fertStrategy
  aut.fertilizer_smart_seconds = dA.fertSeconds
  aut.mystery_auto_buy = myst.autoBuy
  return {
    plantingStrategy: dS.strategy,
    bagSeedFallbackStrategy: dS.fallback,
    bagPriorityLandTypes: dS.bagLandTypes.length ? dS.bagLandTypes.slice() : ALL_LANDS.slice(),
    prioritize2x2Crops: dS.twoX2,
    mysteryAutoBuyCurrencies: myst.currencies.slice(),
    intervals: { farmMin: dS.farmMin, farmMax: dS.farmMax, helpMin: dS.helpMin, helpMax: dS.helpMax, stealMin: dS.stealMin, stealMax: dS.stealMax },
    friendQuietHours: { enabled: dS.quietEnabled, start: dS.quietStart, end: dS.quietEnd },
    plantOrderRandom: dS.plantRandom,
    plantDelaySeconds: dS.plantDelay,
    stealDelaySeconds: dS.stealDelay,
    bagSeedPriority: dPriority.value.slice(),
    preferredSeedId: dS.preferredSeed,
    autoAcceptFriendMinLevel: dA.friendMinLevel,
    automation: aut,
  }
}

async function saveAuto() {
  saving.value = 'auto'
  try {
    const { data } = await api.post('/api/automation', collectAuto())
    if (data && data.ok) { app.success('已保存'); loadAccountMore() }
    else app.error('保存失败：' + ((data && data.error) || '未知错误'))
  } catch (e) { app.error('请求失败') }
  saving.value = ''
}
async function saveStrategy() {
  saving.value = 'strategy'
  try {
    const { data } = await api.post('/api/settings/save', collectStrategy())
    if (data && data.ok) { app.success('已保存'); loadAccountMore() }
    else app.error('保存失败：' + ((data && data.error) || '未知错误'))
  } catch (e) { app.error('请求失败') }
  saving.value = ''
}
async function saveDefault() {
  saving.value = 'default'
  try {
    const { data } = await api.put('/api/settings/default-plan', { enabled: dEn.value, config: collectDefault() })
    if (data && data.ok) { app.success('已保存'); loadDefaultPlan() }
    else app.error('保存失败：' + ((data && data.error) || '未知错误'))
  } catch (e) { app.error('请求失败') }
  saving.value = ''
}
async function toggleApply() {
  dEn.value = !dEn.value
  try {
    await api.put('/api/settings/default-plan', { enabled: dEn.value, config: collectDefault() })
  } catch (e) { /* silent */ }
}
async function importDefault() {
  saving.value = 'import'
  try {
    const { data } = await api.post('/api/settings/default-plan/import', {})
    if (data && data.ok) { app.success('已从当前账号导入默认方案'); loadDefaultPlan() }
    else app.error('导入失败：' + ((data && data.error) || '未知'))
  } catch (e) { app.error('请求失败') }
  saving.value = ''
}
async function resetDefault() {
  if (!confirm('确定要用系统默认设置覆盖当前默认方案吗？')) return
  saving.value = 'reset'
  try {
    const { data } = await api.post('/api/settings/default-plan/reset', {})
    if (data && data.ok) { app.success('默认方案已恢复为系统默认'); loadDefaultPlan() }
    else app.error('恢复失败：' + ((data && data.error) || '未知'))
  } catch (e) { app.error('请求失败') }
  saving.value = ''
}

/* ================= 图鉴 (m-codex) ================= */
const cxType = ref(1)
const cxFilter = ref('all')
const cxItems = ref([])
const cxSummary = reactive({ total: 0, unlocked: 0, locked: 0, canBuy: 0 })
const cxLevel = ref(0)
const cxLoading = ref(false)
const cxFiltered = computed(() => {
  if (cxFilter.value === 'unlocked') return cxItems.value.filter(it => it.unlocked)
  if (cxFilter.value === 'locked') return cxItems.value.filter(it => !it.unlocked)
  return cxItems.value
})
function cxTag(it) {
  if (it.unlocked) return { t: '已收录', c: 'un' }
  if (it.canBuy && it.goodsId) return { t: '可购买补录', c: 'pend' }
  if (Number(it.level) > cxLevel.value) return { t: '等级不足', c: 'lock' }
  return { t: '待解锁', c: 'lock' }
}
function cxNeedLv(it) {
  return (it.seedLevel && Number(it.seedLevel) !== Number(it.level)) ? (' · 需' + it.seedLevel) : ''
}
async function loadCodex(force) {
  if (!acc()) { cxItems.value = []; return }
  cxLoading.value = true
  try {
    const { data } = await api.get('/api/illustrated', { params: Object.assign({ illustrated_type: cxType.value }, force ? { refresh: true } : {}) })
    if (!(data && data.ok && data.data)) { cxItems.value = []; return }
    cxItems.value = data.data.items || []
    cxSummary.total = (data.data.summary && data.data.summary.total) || 0
    cxSummary.unlocked = (data.data.summary && data.data.summary.unlocked) || 0
    cxSummary.locked = (data.data.summary && data.data.summary.locked) || 0
    cxSummary.canBuy = (data.data.summary && data.data.summary.canBuy) || 0
    cxLevel.value = Number(data.data.userLevel) || 0
  } catch (e) { cxItems.value = [] }
  cxLoading.value = false
}
function switchCxType(t) {
  if (t === cxType.value) return
  cxType.value = t
  loadCodex(false)
}
async function cxBuy(gid, price) {
  try {
    const { data } = await api.post('/api/illustrated/buy', { goodsId: Number(gid), price: Number(price) || 0 })
    if (data && data.ok) { app.success('购买成功'); loadCodex(true) }
    else app.error('购买失败：' + ((data && data.error) || '未知'))
  } catch (e) { app.error('购买失败') }
}
async function cxBuyAll() {
  if (!confirm('将尝试购买当前图鉴中所有可购买项目。\n可购买数量：' + (cxSummary.canBuy || 0))) return
  try {
    const { data } = await api.post('/api/illustrated/buy-all', { illustrated_type: cxType.value })
    if (data && data.ok) { app.success('一键购买完成：成功 ' + data.data.successCount + '，失败 ' + data.data.failCount); loadCodex(true) }
    else app.error('一键购买失败：' + ((data && data.error) || '未知'))
  } catch (e) { app.error('一键购买失败') }
}

/* ================= 分析 (m-analysis) ================= */
const anList = ref([])
const anSort = ref('exp')
const anSearch = ref('')
const blk = ref([])
const anLevel = ref(0)
const anFiltered = computed(() => {
  const kw = anSearch.value.trim().toLowerCase()
  if (!kw) return anList.value
  return anList.value.filter(it => String(it.name || '').toLowerCase().includes(kw) || String(it.seedId || '').includes(kw))
})
const anCountTxt = computed(() => '共 ' + anList.value.length + ' 种作物 · 显示 ' + anFiltered.value.length)
const blkIds = computed(() => new Set(blk.value.map(Number)))
function inBlk(id) { return blkIds.value.has(Number(id)) }

async function loadAnalysis() {
  anList.value = []; blk.value = []; anLevel.value = 0
  if (!acc()) return
  try {
    const h = (await api.get('/api/home/profile')).data
    anLevel.value = Number((h.data && h.data.level) || 0)
  } catch (e) { /* silent */ }
  try {
    const a = (await api.get('/api/analytics', { params: { sort: anSort.value } })).data
    anList.value = (a.ok && Array.isArray(a.data)) ? a.data : []
  } catch (e) { anList.value = [] }
  try {
    const b = (await api.get('/api/plant-blacklist')).data
    blk.value = (b.ok && Array.isArray(b.data)) ? b.data.map(Number) : []
  } catch (e) { blk.value = [] }
}
async function refetchAnalytics() {
  if (!acc()) return
  try {
    const a = (await api.get('/api/analytics', { params: { sort: anSort.value } })).data
    anList.value = (a.ok && Array.isArray(a.data)) ? a.data : []
  } catch (e) { anList.value = [] }
}
async function toggleBlk(seedId) {
  seedId = Number(seedId)
  const inB = blk.value.includes(seedId)
  try {
    let d
    if (inB) { d = (await api.delete('/api/plant-blacklist/' + seedId)).data; if (d.ok) blk.value = d.data.map(Number) }
    else { d = (await api.post('/api/plant-blacklist', { seedId })).data; if (d.ok) blk.value = d.data.map(Number) }
  } catch (e) { /* silent */ }
}
async function blkAddAll() {
  if (!acc()) return
  const ids = anList.value.map(it => Number(it.seedId))
  try {
    const { data } = await api.post('/api/plant-blacklist/batch', { seedIds: ids })
    if (data && data.ok) blk.value = data.data.map(Number)
  } catch (e) { /* silent */ }
}
async function blkClear() {
  if (!acc()) return
  try {
    const { data } = await api.delete('/api/plant-blacklist')
    if (data && data.ok) blk.value = []
  } catch (e) { /* silent */ }
}
const strategyCards = computed(() => {
  const defs = [
    { key: 'max_exp', label: '经验/时', metric: 'expPerHour', color: '#a855f7', unit: 'EXP', desc: '每小时经验收益最高' },
    { key: 'max_profit', label: '利润/时', metric: 'profitPerHour', color: '#f59e0b', unit: '金币', desc: '每小时净利润最高' },
    { key: 'max_fert_exp', label: '普肥经验/时', metric: 'normalFertilizerExpPerHour', color: '#3b82f6', unit: 'EXP', desc: '使用普通化肥后经验最高' },
    { key: 'max_fert_profit', label: '普肥利润/时', metric: 'normalFertilizerProfitPerHour', color: '#22c55e', unit: '金币', desc: '使用普通化肥后利润最高' },
  ]
  const plantable = anList.value.filter(it => (it.level == null || it.level === '') || Number(it.level) <= anLevel.value)
  return { cards: defs.map(s => ({ ...s, best: getBestFor(s.metric) })), plantable }
})
function getBestFor(metric) {
  if (!anList.value.length) return null
  const filtered = anList.value.filter(it => (it.level == null || it.level === '') || Number(it.level) <= anLevel.value)
  if (!filtered.length) return null
  return filtered.slice().sort((a, b) => {
    const av = Number(a[metric]), bv = Number(b[metric])
    if (!isFinite(av) && !isFinite(bv)) return 0
    if (!isFinite(av)) return 1
    if (!isFinite(bv)) return -1
    return bv - av
  })[0]
}

watch(active, (v) => {
  if (v === 'm-codex' && !cxItems.value.length) loadCodex(false)
  else if (v === 'm-analysis' && !anList.value.length) loadAnalysis()
})

// 切号事件：用新账号重拉账号资料/默认方案/种子数据（热切换）
const onSwitched = () => { loadAccountMore(); loadDefaultPlan(); loadSeeds(); getSeedsCache() }
onMounted(() => {
  loadAccountMore()
  loadDefaultPlan()
  loadSeeds()
  getSeedsCache()
  window.addEventListener('account-switched', onSwitched)
})
onUnmounted(() => { window.removeEventListener('account-switched', onSwitched) })
</script>

<template>
  <div>
    <h3 style="font-size:20px;font-weight:700;margin:2px 2px 14px">更多</h3>
    <div class="seg seg-5">
      <button class="seg-btn" :class="{ active: active === 'm-auto' }" @click="active = 'm-auto'">自动控制</button>
      <button class="seg-btn" :class="{ active: active === 'm-strategy' }" @click="active = 'm-strategy'">种植策略</button>
      <button class="seg-btn" :class="{ active: active === 'm-default' }" @click="active = 'm-default'">默认方案</button>
      <button class="seg-btn" :class="{ active: active === 'm-codex' }" @click="active = 'm-codex'">图鉴</button>
      <button class="seg-btn" :class="{ active: active === 'm-analysis' }" @click="active = 'm-analysis'">分析</button>
    </div>

    <!-- ============ 自动控制 ============ -->
    <div v-show="active === 'm-auto'">
      <div class="sub-head">🎛️ 自动控制 <small>核心自动化开关</small></div>
      <div v-if="!autLoaded" style="padding:14px;color:var(--muted)">加载中…</div>
      <div class="auto-grid" style="margin-top:0" v-if="autLoaded">
        <div v-for="k in CORE_AUTO" :key="k" class="auto-item">
          <span>{{ AUTO_LABELS[k] }}</span>
          <div class="switch" :class="{ on: autCfg[k] }" @click="toggleSwitch(autCfg, k)"></div>
        </div>
      </div>

      <div class="sub-head">👥 好友互动 <small>好友相关自动化</small></div>
      <div class="auto-grid" v-if="autLoaded">
        <div v-for="k in FRIEND_AUTO" :key="k" class="auto-item">
          <span>{{ AUTO_LABELS[k] }}</span>
          <div class="switch" :class="{ on: autCfg[k] }" @click="toggleSwitch(autCfg, k)"></div>
        </div>
      </div>
      <div class="panel" style="margin-top:10px">
        <div class="auto-item">
          <span>极速务农</span>
          <div class="switch" :class="{ on: autCfg.friend_turbo_mode }" @click="toggleSwitch(autCfg, 'friend_turbo_mode')"></div>
        </div>
        <div style="color:var(--muted);font-size:12px;padding:0 2px 6px">暂停其它巡查，定时只帮护主犬好友；开启后连接专注抢帮，不受 farm/买肥抢占</div>
        <div class="strategy-row" v-if="autCfg.friend_turbo_mode">
          <div class="mini-toggle"><span>定时分段</span><div class="switch" :class="{ on: mTurbo.scheduled }" @click="mTurbo.scheduled = !mTurbo.scheduled"></div></div>
        </div>
        <div class="strategy-row" style="margin-top:8px" v-if="autCfg.friend_turbo_mode && mTurbo.scheduled">
          <div class="sel-wrap"><select class="field" v-model="mTurbo.start"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
          <span style="color:var(--muted);padding:0 6px">—</span>
          <div class="sel-wrap"><select class="field" v-model="mTurbo.end"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
        </div>
      </div>

      <div class="sub-head">💊 施肥策略 <small>按地块与阶段自动施肥</small></div>
      <div class="panel">
        <div class="auto-item col">
          <div class="f-label">自动通过好友最低等级<small>设为 0 表示不限制等级；启用好友相关自动化后，系统会按这里的最低等级自动通过好友申请</small></div>
          <input type="number" class="field w-90" v-model.number="mA.friendMinLevel">
        </div>
        <div class="auto-item col">
          <div class="f-label">施肥范围<small>施肥前会优先按土地类型过滤，仅对命中范围的地块执行施肥策略</small></div>
          <div class="chips">
            <button v-for="land in ALL_LANDS" :key="land" class="chip" :class="{ on: landOn(mA.fertLandTypes, land) }" @click="pushLand(mA.fertLandTypes, land)">{{ { purple: '紫土地', gold: '金土地', black: '黑土地', red: '红土地', normal: '普通土地' }[land] }}</button>
          </div>
        </div>
        <div class="auto-item col">
          <div class="f-label">施肥策略</div>
          <div class="strategy-row">
            <div class="sel-wrap">
              <select class="field" v-model="mA.fertStrategy">
                <option value="both">普通 + 有机</option>
                <option value="smart">普通 + 快成熟有机</option>
                <option value="smart_only">快成熟有机</option>
                <option value="smart_normal">快成熟普通</option>
                <option value="final_normal">最终阶段普通肥</option>
                <option value="final_organic">最终阶段有机肥</option>
                <option value="normal">仅普通化肥</option>
                <option value="organic">仅有机化肥</option>
                <option value="none">不施肥</option>
              </select>
            </div>
            <div class="mini-toggle">
              <span>多季补肥</span>
              <div class="switch" :class="{ on: autCfg.fertilizer_multi_season }" @click="toggleSwitch(autCfg, 'fertilizer_multi_season')"></div>
            </div>
          </div>
        </div>
        <div class="auto-item col">
          <div class="f-label">快成熟判定秒数</div>
          <div class="sec-field">
            <input type="number" class="field w-90" v-model.number="mA.fertSeconds">
            <span class="unit">秒数</span>
          </div>
        </div>
      </div>
      <div style="display:flex;gap:8px;margin-top:12px">
        <button class="fa-btn" style="flex:1" :disabled="saving === 'auto'" @click="saveAuto">{{ saving === 'auto' ? '保存中…' : '💾 保存自动控制' }}</button>
      </div>
    </div>

    <!-- ============ 种植策略 ============ -->
    <div v-show="active === 'm-strategy'">
      <div class="sec-title" style="margin-top:18px"><span>种植策略</span></div>

      <div class="auto-item col" style="border:none;padding:12px 9px">
        <div class="f-label">种植策略</div>
        <div class="strategy-row">
          <div class="sel-wrap">
            <select class="field" v-model="mS.strategy">
              <option value="preferred">优先种植种子</option>
              <option value="level">最高等级作物</option>
              <option value="max_exp">最大经验/时</option>
              <option value="max_fert_exp">最大普通肥经验/时</option>
              <option value="max_profit">最大净利润/时</option>
              <option value="max_fert_profit">最大普通肥净利润/时</option>
              <option value="bag_priority">背包种子优先</option>
            </select>
          </div>
        </div>
      </div>

      <!-- 策略内容（三选一互斥） -->
      <div class="auto-item col" style="border:none;padding:12px 9px">
        <div v-if="mS.strategy === 'preferred' || (mS.strategy === 'bag_priority' && mS.fallback === 'preferred')">
          <div class="f-label">优先种植种子<small v-if="mS.strategy === 'bag_priority'" style="color:var(--muted);font-weight:500;margin-left:6px">第二优先预览</small></div>
          <div class="strategy-row">
            <div class="sel-wrap">
              <select class="field" v-model.number="mS.preferredSeed">
                <option :value="0">（不指定）</option>
                <option v-for="s in seedsCache" :key="s.seedId" :value="s.seedId">{{ s.name || ('种子' + s.seedId) }}（{{ s.requiredLevel || 0 }}级）</option>
              </select>
            </div>
          </div>
        </div>
        <div v-else>
          <div class="f-label">{{ mS.strategy === 'bag_priority' ? '第二优先预览' : '策略选种预览' }}</div>
          <div class="strategy-row">
            <div class="sel-wrap" style="pointer-events:none"><div class="field" style="display:flex;align-items:center;justify-content:space-between;opacity:.82">{{ mPreview }} <span style="opacity:.6;margin-left:6px">▾</span></div></div>
          </div>
        </div>
      </div>

      <!-- bag_priority 三块 -->
      <div v-show="mS.strategy === 'bag_priority'">
        <div class="auto-item col" style="border:none;padding:12px 9px">
          <div class="f-label">第二优先策略</div>
          <div class="strategy-row">
            <div class="sel-wrap">
              <select class="field" v-model="mS.fallback">
                <option value="level">最高等级作物</option>
                <option value="max_exp">最大经验/时</option>
                <option value="max_fert_exp">最大普通肥经验/时</option>
                <option value="max_profit">最大净利润/时</option>
                <option value="max_fert_profit">最大普通肥净利润/时</option>
                <option value="preferred">优先种植种子</option>
              </select>
            </div>
          </div>
        </div>

        <div class="auto-item col" style="border:none;padding:12px 9px">
          <div class="f-label">背包种子优先地块<small>仅在勾选的品质地块上按背包种子优先级种植；未勾选地块、以及背包种子用完后剩余的空地，都会按上方“第二优先策略”种植。默认全选表示不限制。</small></div>
          <div class="chips">
            <button v-for="land in ALL_LANDS" :key="land" class="chip" :class="{ on: landOn(mS.bagLandTypes, land) }" @click="pushLand(mS.bagLandTypes, land)">{{ { purple: '紫土地', gold: '金土地', black: '黑土地', red: '红土地', normal: '普通地' }[land] }}</button>
          </div>
        </div>

        <div class="auto-item col" style="border:none;padding:12px 9px">
          <div class="f-label">优先种植 2×2 作物<small>开启后会根据背包中的四格种子预留完整 2×2 区域；预留区收获后暂不补种普通作物，四块全部空闲时自动种植。四格种子不会从商城购买。</small></div>
          <div style="display:flex;justify-content:flex-end;margin-top:4px"><div class="switch" :class="{ on: mS.twoX2 }" @click="mS.twoX2 = !mS.twoX2"></div></div>
        </div>

        <div class="auto-item col" style="border:none;padding:12px 9px 14px">
          <div class="f-label">背包种子优先顺序<small>先按下方顺序消耗背包种子；开启 2×2 优先时，四格种子会先用于预留区域，其余空地再按第二优先策略补种。</small></div>
          <div style="display:flex;justify-content:flex-end;margin-bottom:8px">
            <button class="chip" @click="resetSeeds('m')">↺ 重置顺序</button>
          </div>
          <div class="menu">
            <p v-if="!mSeeds.length" style="font-size:11px;color:var(--muted);text-align:center;padding:8px 0">背包中暂无种子</p>
            <div v-for="row in seedRows(mPriority)" :key="row.s.seedId" class="seed-row" style="display:flex;align-items:center;gap:6px;padding:5px 2px;border-bottom:1px solid var(--border);font-size:12px">
              <span style="color:var(--muted-2);font-size:10px;width:16px;flex:none">{{ row.i + 1 }}</span>
              <span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ row.s.name || ('种子' + row.s.seedId) }}<span v-if="row.twoX2" style="color:var(--accent,#10b981);font-size:10px"> 2×2</span></span>
              <span style="flex:none;color:var(--muted);font-size:10px">×{{ row.s.count || 0 }} · {{ row.s.requiredLevel || 0 }}级</span>
              <button class="seed-up" :disabled="row.first" style="flex:none;border:1px solid var(--primary-soft);background:color-mix(in oklch,var(--primary) 12%,transparent);color:var(--foreground);border-radius:5px;font-size:11px;line-height:1;font-weight:700;cursor:pointer;padding:3px 8px" @click="moveSeed('m', row.s.seedId, -1)">↑</button>
              <button class="seed-dn" :disabled="row.last" style="flex:none;border:1px solid var(--primary-soft);background:color-mix(in oklch,var(--primary) 12%,transparent);color:var(--foreground);border-radius:5px;font-size:11px;line-height:1;font-weight:700;cursor:pointer;padding:3px 8px" @click="moveSeed('m', row.s.seedId, 1)">↓</button>
            </div>
          </div>
        </div>
      </div>

      <!-- 巡查与延迟 -->
      <div class="sub-head" style="margin-top:22px">⏱️ 巡查与延迟</div>
      <div class="panel">
        <div class="auto-item col" style="border:none;padding:12px 9px">
          <div class="f-label">巡查时间 <small>为各巡查动作设置随机秒数范围</small></div>
          <div class="strategy-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:13px">🕵️ 农场巡查</span>
            <input type="number" class="field w-90" v-model.number="mS.farmMin">
            <span style="color:var(--muted)">~</span>
            <input type="number" class="field w-90" v-model.number="mS.farmMax">
          </div>
          <div class="strategy-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:13px">🤝 帮助巡查</span>
            <input type="number" class="field w-90" v-model.number="mS.helpMin">
            <span style="color:var(--muted)">~</span>
            <input type="number" class="field w-90" v-model.number="mS.helpMax">
          </div>
          <div class="strategy-row">
            <span style="flex:1;font-size:13px">🕵️ 偷菜巡查</span>
            <input type="number" class="field w-90" v-model.number="mS.stealMin">
            <span style="color:var(--muted)">~</span>
            <input type="number" class="field w-90" v-model.number="mS.stealMax">
          </div>
        </div>

        <div class="auto-item col" style="border:none;padding:12px 9px">
          <div class="f-label">静默时段</div>
          <div class="strategy-row" style="margin-bottom:10px">
            <span style="flex:1;font-size:13px">🔕 启用静默时段</span>
            <div class="switch" :class="{ on: mS.quietEnabled }" @click="mS.quietEnabled = !mS.quietEnabled"></div>
          </div>
          <div class="strategy-row">
            <div class="sel-wrap"><select class="field" v-model="mS.quietStart"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
            <span style="color:var(--muted)">起</span>
            <div class="sel-wrap"><select class="field" v-model="mS.quietEnd"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
            <span style="color:var(--muted)">止</span>
          </div>
        </div>

        <div class="auto-item col" style="border:none;padding:12px 9px 14px">
          <div class="f-label">种植与偷菜延迟</div>
          <div class="strategy-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:13px">🔀 种植顺序随机</span>
            <div class="switch" :class="{ on: mS.plantRandom }" @click="mS.plantRandom = !mS.plantRandom"></div>
          </div>
          <div class="strategy-row" style="margin-bottom:8px">
            <span style="flex:1;font-size:13px">种植延迟</span>
            <input type="number" class="field w-90" v-model.number="mS.plantDelay"><span class="unit">秒</span>
          </div>
          <div class="strategy-row">
            <span style="flex:1;font-size:13px">偷菜延迟</span>
            <input type="number" class="field w-90" v-model.number="mS.stealDelay"><span class="unit">秒</span>
          </div>
        </div>
      </div>
      <div style="display:flex;gap:8px;margin-top:12px">
        <button class="fa-btn" style="flex:1" :disabled="saving === 'strategy'" @click="saveStrategy">{{ saving === 'strategy' ? '保存中…' : '💾 保存种植策略' }}</button>
      </div>
    </div>

    <!-- ============ 默认方案 ============ -->
    <div v-show="active === 'm-default'">
      <div class="sec-title" style="margin-top:16px">
        <span><span style="font-size:15px">🎛️</span> 默认方案</span>
        <span class="chip" :class="{ on: dEn }" style="cursor:pointer;background:color-mix(in oklch,var(--good) 15%,transparent);color:var(--good)" @click="toggleApply">{{ dEn ? '新账号自动应用' : '新账号不应用' }}</span>
      </div>
      <p style="font-size:11px;color:var(--muted);margin:2px 2px 4px">{{ dSavedText }}</p>
      <div style="display:flex;gap:8px;margin:8px 0 2px">
        <button class="fa-btn" style="flex:1" :disabled="saving === 'import'" @click="importDefault">{{ saving === 'import' ? '导入中…' : '📥 从当前账号导入' }}</button>
        <button class="fa-btn" style="flex:1" :disabled="saving === 'reset'" @click="resetDefault">{{ saving === 'reset' ? '恢复中…' : '🔄 恢复系统默认' }}</button>
      </div>
      <p style="font-size:11px;color:var(--muted);margin:6px 2px">为<b>新账号</b>一键套用此方案，快速完成种植策略与自动控制参数。</p>

      <div class="dtab-bar">
        <button class="dtab" :class="{ active: defaultPane === 'd-strategy' }" @click="defaultPane = 'd-strategy'">⚙️ 策略设置</button>
        <button class="dtab" :class="{ active: defaultPane === 'd-auto' }" @click="defaultPane = 'd-auto'">🔁 自动控制</button>
      </div>

      <!-- 默认策略 -->
      <div v-show="defaultPane === 'd-strategy'">
        <div class="sub-head" style="margin-top:16px">⚙️ 默认策略 <small>Agoni</small></div>
        <div class="panel">
          <div class="auto-item col" style="border:none;padding:12px 9px">
            <div class="f-label">种植策略</div>
            <div class="strategy-row">
              <div class="sel-wrap"><select class="field" v-model="dS.strategy">
                <option value="preferred">优先种植种子</option><option value="level">最高等级作物</option><option value="max_exp">最大经验/时</option>
                <option value="max_fert_exp">最大普通肥经验/时</option><option value="max_profit">最大净利润/时</option><option value="max_fert_profit">最大普通肥净利润/时</option>
                <option value="bag_priority">背包种子优先</option>
              </select></div>
            </div>
          </div>
          <div class="auto-item col" style="border:none;padding:12px 9px">
            <div v-if="dS.strategy === 'preferred' || (dS.strategy === 'bag_priority' && dS.fallback === 'preferred')">
              <div class="f-label">优先种植种子<small v-if="dS.strategy === 'bag_priority'" style="color:var(--muted);font-weight:500;margin-left:6px">第二优先预览</small></div>
              <div class="strategy-row">
                <div class="sel-wrap"><select class="field" v-model.number="dS.preferredSeed">
                  <option :value="0">（不指定）</option>
                  <option v-for="s in seedsCache" :key="s.seedId" :value="s.seedId">{{ s.name || ('种子' + s.seedId) }}（{{ s.requiredLevel || 0 }}级）</option>
                </select></div>
              </div>
            </div>
            <div v-else>
              <div class="f-label">{{ dS.strategy === 'bag_priority' ? '第二优先预览' : '策略选种预览' }}</div>
              <div class="strategy-row"><div class="sel-wrap" style="pointer-events:none"><div class="field" style="display:flex;align-items:center;justify-content:space-between;opacity:.82">{{ dPreview }} <span style="opacity:.6;margin-left:6px">▾</span></div></div></div>
            </div>
          </div>
          <div v-show="dS.strategy === 'bag_priority'">
            <div class="auto-item col" style="border:none;padding:12px 9px">
              <div class="f-label">第二优先策略</div>
              <div class="strategy-row">
                <div class="sel-wrap"><select class="field" v-model="dS.fallback">
                  <option value="level">最高等级作物</option><option value="max_exp">最大经验/时</option>
                  <option value="max_fert_exp">最大普通肥经验/时</option><option value="max_profit">最大净利润/时</option><option value="max_fert_profit">最大普通肥净利润/时</option>
                  <option value="preferred">优先种植种子</option>
                </select></div>
              </div>
            </div>
            <div class="auto-item col" style="border:none;padding:12px 9px">
              <div class="f-label">背包种子优先地块<small>仅在勾选的品质地块上按背包种子优先级种植；未勾选地块、以及背包种子用完后剩余的空地，都会按上方“第二优先策略”种植。默认全选表示不限制。</small></div>
              <div class="chips">
                <button v-for="land in ALL_LANDS" :key="land" class="chip" :class="{ on: landOn(dS.bagLandTypes, land) }" @click="pushLand(dS.bagLandTypes, land)">{{ { purple: '紫土地', gold: '金土地', black: '黑土地', red: '红土地', normal: '普通地' }[land] }}</button>
              </div>
            </div>
            <div class="auto-item col" style="border:none;padding:12px 9px">
              <div class="f-label">优先种植 2×2 作物<small>开启后会根据背包中的四格种子预留完整 2×2 区域；预留区收获后暂不补种普通作物，四块全部空闲时自动种植。四格种子不会从商城购买。</small></div>
              <div style="display:flex;justify-content:flex-end;margin-top:4px"><div class="switch" :class="{ on: dS.twoX2 }" @click="dS.twoX2 = !dS.twoX2"></div></div>
            </div>
            <div class="auto-item col" style="border:none;padding:12px 9px 14px">
              <div class="f-label">背包种子优先顺序<small>先按下方顺序消耗背包种子；开启 2×2 优先时，四格种子会先用于预留区域，其余空地再按第二优先策略补种。</small></div>
              <div style="display:flex;justify-content:flex-end;margin-bottom:8px">
                <button class="chip" @click="resetSeeds('d')">↺ 重置顺序</button>
              </div>
              <div class="menu">
                <p v-if="!mSeeds.length" style="font-size:11px;color:var(--muted);text-align:center;padding:8px 0">背包中暂无种子</p>
                <div v-for="row in seedRows(dPriority)" :key="row.s.seedId" class="seed-row" style="display:flex;align-items:center;gap:6px;padding:5px 2px;border-bottom:1px solid var(--border);font-size:12px">
                  <span style="color:var(--muted-2);font-size:10px;width:16px;flex:none">{{ row.i + 1 }}</span>
                  <span style="flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap">{{ row.s.name || ('种子' + row.s.seedId) }}<span v-if="row.twoX2" style="color:var(--accent,#10b981);font-size:10px"> 2×2</span></span>
                  <span style="flex:none;color:var(--muted);font-size:10px">×{{ row.s.count || 0 }} · {{ row.s.requiredLevel || 0 }}级</span>
                  <button class="seed-up" :disabled="row.first" style="flex:none;border:1px solid var(--primary-soft);background:color-mix(in oklch,var(--primary) 12%,transparent);color:var(--foreground);border-radius:5px;font-size:11px;line-height:1;font-weight:700;cursor:pointer;padding:3px 8px" @click="moveSeed('d', row.s.seedId, -1)">↑</button>
                  <button class="seed-dn" :disabled="row.last" style="flex:none;border:1px solid var(--primary-soft);background:color-mix(in oklch,var(--primary) 12%,transparent);color:var(--foreground);border-radius:5px;font-size:11px;line-height:1;font-weight:700;cursor:pointer;padding:3px 8px" @click="moveSeed('d', row.s.seedId, 1)">↓</button>
                </div>
              </div>
            </div>
          </div>
        </div>

        <div class="sub-head" style="margin-top:6px">⏱️ 巡查与延迟</div>
        <div class="panel">
          <div class="auto-item col" style="border:none;padding:12px 9px">
            <div class="f-label">巡查时间 <small>为各巡查动作设置随机秒数范围</small></div>
            <div class="strategy-row" style="margin-bottom:8px">
              <span style="flex:1;font-size:13px">🕵️ 农场巡查</span>
              <input type="number" class="field w-90" v-model.number="dS.farmMin"><span style="color:var(--muted)">~</span>
              <input type="number" class="field w-90" v-model.number="dS.farmMax">
            </div>
            <div class="strategy-row" style="margin-bottom:8px">
              <span style="flex:1;font-size:13px">🤝 帮助巡查</span>
              <input type="number" class="field w-90" v-model.number="dS.helpMin"><span style="color:var(--muted)">~</span>
              <input type="number" class="field w-90" v-model.number="dS.helpMax">
            </div>
            <div class="strategy-row">
              <span style="flex:1;font-size:13px">🕵️ 偷菜巡查</span>
              <input type="number" class="field w-90" v-model.number="dS.stealMin"><span style="color:var(--muted)">~</span>
              <input type="number" class="field w-90" v-model.number="dS.stealMax">
            </div>
          </div>
          <div class="auto-item col" style="border:none;padding:12px 9px">
            <div class="f-label">静默时段</div>
            <div class="strategy-row" style="margin-bottom:10px">
              <span style="flex:1;font-size:13px">🔕 启用静默时段</span>
              <div class="switch" :class="{ on: dS.quietEnabled }" @click="dS.quietEnabled = !dS.quietEnabled"></div>
            </div>
            <div class="strategy-row">
              <div class="sel-wrap"><select class="field" v-model="dS.quietStart"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
              <span style="color:var(--muted)">起</span>
              <div class="sel-wrap"><select class="field" v-model="dS.quietEnd"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
              <span style="color:var(--muted)">止</span>
            </div>
          </div>
          <div class="auto-item col" style="border:none;padding:12px 9px 14px">
            <div class="f-label">种植与偷菜延迟</div>
            <div class="strategy-row" style="margin-bottom:8px">
              <span style="flex:1;font-size:13px">🔀 种植顺序随机</span>
              <div class="switch" :class="{ on: dS.plantRandom }" @click="dS.plantRandom = !dS.plantRandom"></div>
            </div>
            <div class="strategy-row" style="margin-bottom:8px">
              <span style="flex:1;font-size:13px">种植延迟</span>
              <input type="number" class="field w-90" v-model.number="dS.plantDelay"><span class="unit">秒</span>
            </div>
            <div class="strategy-row">
              <span style="flex:1;font-size:13px">偷菜延迟</span>
              <input type="number" class="field w-90" v-model.number="dS.stealDelay"><span class="unit">秒</span>
            </div>
          </div>
        </div>
        <div style="display:flex;gap:8px;margin-top:12px">
          <button class="fa-btn" style="flex:1" :disabled="saving === 'default'" @click="saveDefault">{{ saving === 'default' ? '保存中…' : '💾 保存默认方案' }}</button>
        </div>
      </div>

      <!-- 默认自动 -->
      <div v-show="defaultPane === 'd-auto'">
        <div class="sub-head" style="margin-top:16px">🎛️ 自动控制 <small>核心自动化开关</small></div>
        <div v-if="!dAutLoaded" style="padding:14px;color:var(--muted)">加载中…</div>
        <div class="auto-grid" v-if="dAutLoaded">
          <div v-for="k in CORE_AUTO" :key="k" class="auto-item">
            <span>{{ AUTO_LABELS[k] }}</span>
            <div class="switch" :class="{ on: dAutCfg[k] }" @click="toggleSwitch(dAutCfg, k)"></div>
          </div>
        </div>
        <div class="sub-head">👥 好友互动 <small>好友相关自动化</small></div>
        <div class="auto-grid" v-if="dAutLoaded">
          <div v-for="k in FRIEND_AUTO" :key="k" class="auto-item">
            <span>{{ AUTO_LABELS[k] }}</span>
            <div class="switch" :class="{ on: dAutCfg[k] }" @click="toggleSwitch(dAutCfg, k)"></div>
          </div>
        </div>
        <div class="panel" style="margin-top:10px">
          <div class="auto-item">
            <span>极速务农</span>
            <div class="switch" :class="{ on: dAutCfg.friend_turbo_mode }" @click="toggleSwitch(dAutCfg, 'friend_turbo_mode')"></div>
          </div>
          <div style="color:var(--muted);font-size:12px;padding:0 2px 6px">暂停其它巡查，定时只帮护主犬好友；开启后连接专注抢帮，不受 farm/买肥抢占</div>
          <div class="strategy-row" v-if="dAutCfg.friend_turbo_mode">
            <div class="mini-toggle"><span>定时分段</span><div class="switch" :class="{ on: dTurbo.scheduled }" @click="dTurbo.scheduled = !dTurbo.scheduled"></div></div>
          </div>
          <div class="strategy-row" style="margin-top:8px" v-if="dAutCfg.friend_turbo_mode && dTurbo.scheduled">
            <div class="sel-wrap"><select class="field" v-model="dTurbo.start"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
            <span style="color:var(--muted);padding:0 6px">—</span>
            <div class="sel-wrap"><select class="field" v-model="dTurbo.end"><option v-for="h in hourOpts" :key="h" :value="h">{{ h }}</option></select></div>
          </div>
        </div>
        <div class="sub-head">💊 施肥策略 <small>按地块与阶段自动施肥</small></div>
        <div class="panel">
          <div class="auto-item col">
            <div class="f-label">自动通过好友最低等级<small>设为 0 表示不限制等级；启用好友相关自动化后，系统会按这里的最低等级自动通过好友申请</small></div>
            <input type="number" class="field w-90" v-model.number="dA.friendMinLevel">
          </div>
          <div class="auto-item col">
            <div class="f-label">施肥范围<small>施肥前会优先按土地类型过滤，仅对命中范围的地块执行施肥策略</small></div>
            <div class="chips">
              <button v-for="land in ALL_LANDS" :key="land" class="chip" :class="{ on: landOn(dA.fertLandTypes, land) }" @click="pushLand(dA.fertLandTypes, land)">{{ { purple: '紫土地', gold: '金土地', black: '黑土地', red: '红土地', normal: '普通土地' }[land] }}</button>
            </div>
          </div>
          <div class="auto-item col">
            <div class="f-label">施肥策略</div>
            <div class="strategy-row">
              <div class="sel-wrap"><select class="field" v-model="dA.fertStrategy">
                <option value="both">普通 + 有机</option><option value="smart">普通 + 快成熟有机</option><option value="smart_only">快成熟有机</option>
                <option value="smart_normal">快成熟普通</option><option value="final_normal">最终阶段普通肥</option><option value="final_organic">最终阶段有机肥</option>
                <option value="normal">仅普通化肥</option><option value="organic">仅有机化肥</option><option value="none">不施肥</option>
              </select></div>
              <div class="mini-toggle"><span>多季补肥</span><div class="switch" :class="{ on: dAutCfg.fertilizer_multi_season }" @click="toggleSwitch(dAutCfg, 'fertilizer_multi_season')"></div></div>
            </div>
          </div>
          <div class="auto-item col">
            <div class="f-label">快成熟判定秒数</div>
            <div class="sec-field">
              <input type="number" class="field w-90" v-model.number="dA.fertSeconds"><span class="unit">秒数</span>
            </div>
          </div>
        </div>
        <div style="display:flex;gap:8px;margin-top:12px">
          <button class="fa-btn" style="flex:1" :disabled="saving === 'default'" @click="saveDefault">{{ saving === 'default' ? '保存中…' : '💾 保存默认方案' }}</button>
        </div>
      </div>
    </div>

    <!-- ============ 图鉴 ============ -->
    <div v-show="active === 'm-codex'">
      <div class="sec-title" style="margin-top:18px"><span>作物图鉴</span></div>
      <div class="seg" style="margin-bottom:10px">
        <button class="seg-btn" :class="{ active: cxType === 1 }" @click="switchCxType(1)">作物图鉴</button>
        <button class="seg-btn" :class="{ active: cxType === 2 }" @click="switchCxType(2)">变异图鉴</button>
      </div>
      <div class="seg" style="margin-bottom:10px">
        <button class="seg-btn" :class="{ active: cxFilter === 'all' }" @click="cxFilter = 'all'">全部</button>
        <button class="seg-btn" :class="{ active: cxFilter === 'unlocked' }" @click="cxFilter = 'unlocked'">已解锁</button>
        <button class="seg-btn" :class="{ active: cxFilter === 'locked' }" @click="cxFilter = 'locked'">未解锁</button>
      </div>
      <div class="cx-badges">
        <span class="cx-badge ok"><b>{{ cxSummary.unlocked }}</b>/{{ cxSummary.total }} 已解锁</span>
        <span class="cx-badge"><b>{{ cxSummary.locked }}</b>/{{ cxSummary.total }} 未解锁</span>
        <span class="cx-badge warn"><b>{{ cxSummary.canBuy }}</b> 可购买</span>
        <span class="cx-badge info">Lv.{{ cxLevel }}</span>
      </div>
      <div style="display:flex;gap:8px;margin:12px 0">
        <button class="fa-btn" style="flex:1" @click="cxBuyAll">🛒 一键购买</button>
        <button class="fa-btn" style="flex:1" @click="loadCodex(true)">🔄 刷新</button>
      </div>
      <div class="bag-grid" style="grid-template-columns:repeat(auto-fill,minmax(104px,1fr));gap:10px">
        <p v-if="cxLoading" class="cx-empty" style="grid-column:1/-1">正在加载图鉴...</p>
        <template v-else>
          <div v-for="it in cxFiltered" :key="it.id || it.seedId" class="cx-item" :class="{ locked: !it.unlocked }">
            <div class="ic">
              <img v-if="it.image" :src="it.image" alt="" @error="$event.target.remove()">
              <span v-else style="font-size:20px">{{ it.unlocked ? '🌾' : '🌱' }}</span>
            </div>
            <div class="nm">{{ it.name || '' }}</div>
            <div class="lv">Lv.{{ it.level || '-' }}{{ cxNeedLv(it) }}</div>
            <span class="tag" :class="cxTag(it).c">{{ cxTag(it).t }}</span>
            <button v-if="!it.unlocked && it.canBuy && it.goodsId" class="buy" @click="cxBuy(it.goodsId, it.price)">购买</button>
          </div>
          <p v-if="!cxFiltered.length" class="cx-empty" style="grid-column:1/-1">暂无图鉴项目</p>
        </template>
      </div>
      <p style="font-size:10.5px;color:var(--muted);text-align:center;margin-top:10px">图鉴数据说明 · 未解锁且满足等级可购买补录</p>
    </div>

    <!-- ============ 分析 ============ -->
    <div v-show="active === 'm-analysis'">
      <div class="sec-title" style="margin-top:18px"><span>数据统计</span></div>
      <div class="seg" style="margin-bottom:10px">
        <button class="seg-btn" :class="{ active: anTab === 'crops' }" @click="anTab = 'crops'">全部作物</button>
        <button class="seg-btn" :class="{ active: anTab === 'blacklist' }" @click="anTab = 'blacklist'">偷菜黑名单</button>
        <button class="seg-btn" :class="{ active: anTab === 'strategy' }" @click="anTab = 'strategy'">种植策略</button>
      </div>

      <div v-show="anTab === 'crops'">
        <div style="display:flex;gap:8px;align-items:center;margin-bottom:10px">
          <input class="field" v-model="anSearch" placeholder="🔍 搜索作物..." style="flex:1;min-width:0">
          <select class="field" v-model="anSort" @change="refetchAnalytics" style="width:auto">
            <option value="exp">经验/时</option>
            <option value="fert">普肥经验/时</option>
            <option value="profit">利润/时</option>
            <option value="fert_profit">普肥利润/时</option>
            <option value="level">等级</option>
          </select>
        </div>
        <p v-if="!anList.length" class="cx-empty">{{ acc() ? '暂无数据' : '请先选择账号' }}</p>
        <div v-else>
          <div v-for="it in anFiltered" :key="it.seedId" class="an-card">
            <div class="aic">
              <img v-if="it.image" :src="it.image" alt="" @error="$event.target.remove()">
              <span v-else style="font-size:18px">🥕</span>
            </div>
            <div class="ainfo">
              <div class="anm">{{ it.name || '' }}<span v-if="inBlk(it.seedId)" class="blk-tag">黑名单</span></div>
              <div class="amz"><span>Lv.{{ it.level == null ? '未知' : it.level }} · {{ it.seasons }}季 · {{ it.growTimeStr || '' }}</span></div>
              <div class="amz"><span class="c-purple">经验 <b>{{ it.expPerHour }}</b>/时</span> · <span class="c-blue">普肥经验 <b>{{ it.normalFertilizerExpPerHour }}</b>/时</span></div>
              <div class="amz"><span class="c-amber">利润 <b>{{ it.profitPerHour ?? '-' }}</b>/时</span> · <span class="c-green">普肥利润 <b>{{ it.normalFertilizerProfitPerHour ?? '-' }}</b>/时</span></div>
            </div>
            <div class="ablk">
              <button v-if="inBlk(it.seedId)" class="rm" @click="toggleBlk(it.seedId)">移出黑名单</button>
              <button v-else class="add" @click="toggleBlk(it.seedId)">加入黑名单</button>
            </div>
          </div>
          <p style="font-size:11px;color:var(--muted);margin:8px 2px 0">{{ anCountTxt }}</p>
        </div>
      </div>

      <div v-show="anTab === 'blacklist'">
        <p style="font-size:11px;color:var(--muted);margin:8px 2px 10px">加入后自动偷菜会跳过这些蔬菜，不影响自己种植</p>
        <div style="display:flex;gap:8px;margin-bottom:10px">
          <button class="fa-btn" style="flex:1" @click="blkAddAll">一键全部加入</button>
          <button class="fa-btn" style="flex:1" @click="blkClear">清空黑名单</button>
        </div>
        <p v-if="!blk.length" class="cx-empty">暂无黑名单蔬菜</p>
        <div v-else>
          <div v-for="seedId in blk" :key="seedId" class="blk-row">
            <div class="bic">
              <img v-if="(anList.find(x => Number(x.seedId) === Number(seedId)) || {}).image" :src="(anList.find(x => Number(x.seedId) === Number(seedId)) || {}).image" alt="" @error="$event.target.remove()">
              <span v-else style="font-size:18px">🥬</span>
            </div>
            <div class="binfo">
              <div class="bname">{{ (anList.find(x => Number(x.seedId) === Number(seedId)) || {}).name || ('蔬菜' + seedId) }}</div>
              <div class="bid">ID: {{ seedId }}</div>
            </div>
            <button @click="toggleBlk(seedId)">移出</button>
          </div>
        </div>
      </div>

      <div v-show="anTab === 'strategy'">
        <p style="font-size:11px;color:var(--muted);margin:8px 2px 10px">按当前等级推荐最优种植策略</p>
        <div v-for="sc in strategyCards.cards" :key="sc.key" class="strat-card">
          <div class="sh">
            <div class="ic" :style="{ background: sc.color }">✦</div>
            <div>
              <div class="st">{{ sc.label }}</div>
              <div class="su">{{ sc.desc }}</div>
            </div>
          </div>
          <template v-if="sc.best">
            <div class="best">
              <div class="bic">
                <img v-if="sc.best.image" :src="sc.best.image" width="34" height="34" style="object-fit:contain" alt="" @error="$event.target.remove()">
                <span v-else style="font-size:18px">🥕</span>
              </div>
              <div class="bcnm">{{ sc.best.name || '' }}</div>
              <div class="bcmeta">Lv.{{ sc.best.level == null ? '-' : sc.best.level }}</div>
            </div>
            <div class="val"><span>{{ sc.unit }}/时</span><b :style="{ color: sc.color }">{{ sc.best[sc.metric] }}</b></div>
          </template>
          <p v-else class="cx-empty" style="padding:10px 0">暂无可种植作物</p>
        </div>
        <p style="font-size:11px;color:var(--muted);text-align:center;margin-top:6px">当前等级 Lv.{{ anLevel }} · 可种植 {{ strategyCards.plantable.length }}/{{ anList.length }} 种作物</p>
      </div>
    </div>

    <!-- 通用入口 -->
    <div class="menu-title">通用</div>
    <div class="menu">
      <button class="sub-item" @click="router.push('/settings')"><span class="mi">⚙️</span>设置<span class="arr">›</span></button>
      <button class="sub-item" @click="router.push('/backend')"><span class="mi">🖥️</span>后台<span class="arr">›</span></button>
    </div>
    <p style="margin-top:20px;text-align:center;font-size:11.5px;color:var(--muted-2)">qq farm bot go v1.0.1</p>
  </div>
</template>
