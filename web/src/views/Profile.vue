<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import api, { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const acc = () => getAccountId()
const tab = ref('p-farm')
const fsub = ref('list')

/* ---------------- 农场（对齐 legacy renderLandCard：landCanFertilize/Remove 前端判定） ---------------- */
const lands = ref([])
const landTick = ref(0)
let landTimer = null
function landCanFertilize(l) { return Number(l.matureInSec || 0) > 0 && l.status !== 'locked' && l.status !== 'empty' }
function landCanRemove(l) {
  return l.status !== 'locked' && l.status !== 'empty' && Boolean(l.plantName || l.seedImage || Number(l.matureInSec || 0) > 0 || ['dead', 'growing', 'harvestable', 'stealable'].includes(String(l.status || '')))
}
function landCls(l) {
  const c = ['plot']
  if (l.status === 'locked') c.push('locked')
  if (l.status === 'dead') c.push('status-dead')
  if (l.status === 'harvestable') c.push('status-harvestable')
  if (l.status === 'stealable') c.push('status-stealable')
  const lv = Number(l.level) || 0
  if (lv >= 1 && lv <= 5) c.push('lv' + lv)
  if (Number(l.plantSize) > 1) c.push('merged')
  return c.join(' ')
}
const LAND_CLS_MAP = { locked: 'locked', dead: 'status-dead', harvestable: 'status-harvestable', stealable: 'status-stealable' }
function landTags(l) {
  const t = []
  if (l.needWater) t.push('t-water')
  if (l.needWeed) t.push('t-weed')
  if (l.needBug) t.push('t-bug')
  if (l.status === 'harvestable') t.push('t-harvest')
  else if (l.status === 'stealable') t.push('t-steal')
  return t
}
function landTagText(c) { return { 't-water': '水', 't-weed': '草', 't-bug': '虫', 't-harvest': '可收', 't-steal': '可偷' }[c] || '' }
function fmtDur(sec) {
  sec = Math.max(0, Number(sec) || 0)
  const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60
  return (h > 0 ? h + ':' : '') + String(m).padStart(2, '0') + ':' + String(s).padStart(2, '0')
}
function landGrowPct(l) { const m = Number(l.matureInSec || 0), t = Number(l.totalGrowTime || 0); if (t <= 0 || m <= 0) return 0; return Math.min(100, Math.max(0, (m / t) * 100)) }
async function loadLands() { if (!acc()) return; try { const { data } = await api.get('/api/farm/lands'); lands.value = (data?.data?.lands || data?.data || []) } catch (e) {} }
function landCountdown() {
  const now = Date.now()
  lands.value.forEach(l => { if (l.matureAt) l.__left = Math.max(0, Math.ceil((l.matureAt - now) / 1000)) })
  landTick.value++
}
async function farmAction(action) {
  if (!acc()) { app.error('请先选择账号'); return }
  try {
    const { data } = await api.post('/api/farm/action', { action })
    app.success(data?.ok ? ('操作完成：' + action) : ('失败：' + (data?.error || '未知')))
    if (data?.ok) loadLands()
  } catch (e) { app.error('请求失败') }
}
async function removeAll() {
  if (!acc()) { app.error('请先选择账号'); return }
  if (!confirm('确定一键铲除所有作物？')) return
  try {
    const { data } = await api.post('/api/land/remove-all', {})
    app.success(data?.ok ? ('操作完成：' + (data?.message || '铲除')) : ('失败：' + (data?.error || '未知')))
    if (data?.ok) loadLands()
  } catch (e) { app.error('请求失败') }
}
async function landOp(l, op) {
  if (!acc()) { app.error('请先选择账号'); return }
  try {
    const { data } = await api.post(op === 'fertilize' ? '/api/land/fertilize' : '/api/land/remove', { landId: l.id })
    app.success(data?.ok ? (op === 'fertilize' ? '催熟完成' : '已铲除') : ('失败：' + (data?.error || '')))
    if (data?.ok) loadLands()
  } catch (e) { app.error('请求失败') }
}

/* ---------------- 背包（对齐 renderBag） ---------------- */
const bagItems = ref([]); const bagCat = ref('fruit'); const bagSellMode = ref(false); const bagSel = ref(new Set())
function bagCanUse(it) { return Number(it.itemType) === 11 }
function bagCanSell(it) { const t = Number(it.itemType); return t === 17 || t === 6 }
const bagCounts = computed(() => {
  const all = bagItems.value.length
  return { '': all, fruit: bagItems.value.filter(i => i.category === 'fruit').length, seed: bagItems.value.filter(i => i.category === 'seed').length, props: bagItems.value.filter(i => i.category === 'props' || i.category === 'fertilizer').length, other: bagItems.value.filter(i => i.category === 'other').length }
})
const bagShown = computed(() => { const c = bagCat.value; let l = bagItems.value; if (c === 'props') l = l.filter(i => i.category === 'props' || i.category === 'fertilizer'); else if (c) l = l.filter(i => i.category === c); return l })
async function loadBag() { if (!acc()) return; try { const { data } = await api.get('/api/bag/items'); bagItems.value = data?.data || [] } catch (e) {} }
function toggleSellMode() { bagSellMode.value = !bagSellMode.value; bagSel.value = new Set() }
function toggleSel(id) { const s = new Set(bagSel.value); s.has(id) ? s.delete(id) : s.add(id); bagSel.value = s }
async function bagUse(it) {
  if (!confirm('确定使用 ' + it.count + ' 个该道具？')) return
  const { data } = await api.post('/api/bag/use', { itemId: it.id, count: it.count }).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '使用成功' : ('失败：' + (data?.error || '')))
  loadBag()
}
async function bagSellOne(it) {
  if (!acc()) { app.error('请先选择账号'); return }
  if (!confirm('确定出售 ' + it.count + ' 个该物品？')) return
  const { data } = await api.post('/api/bag/sell', { items: [{ id: it.id, count: it.count, uid: it.uid || 0 }] }).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '出售成功' : ('失败：' + (data?.error || '')))
  loadBag()
}
async function bagSellSel() {
  if (!acc()) { app.error('请先选择账号'); return }
  const items = bagItems.value.filter(i => bagSel.value.has(i.id)).map(i => ({ id: i.id, count: i.count, uid: i.uid || 0 }))
  if (!items.length) { app.error('请先勾选物品'); return }
  const { data } = await api.post('/api/bag/sell', { items }).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '出售成功' : ('失败：' + (data?.error || '')))
  toggleSellMode(); loadBag()
}

/* ---------------- 好友（对齐 legacy renderFriendCards） ---------------- */
const friends = ref([]); const friendSearch = ref(''); const friendDogFilter = ref('all')
const isGuard = f => !!(f.hasDog || Number(f.dogId) === 90021)   // legacy 渲染用 hasDog、过滤用 dogId
const shownFriends = computed(() => {
  let l = friends.value
  const q = friendSearch.value.trim()
  if (q) l = l.filter(f => String(f.name || '').includes(q) || String(f.uid ?? f.gid ?? '').includes(q))
  if (friendDogFilter.value === 'guardDog') l = l.filter(f => Number(f.dogId) === 90021)
  else if (friendDogFilter.value === 'noGuardDog') l = l.filter(f => Number(f.dogId) !== 90021)
  return l
})
const friendLandsMap = reactive({})
const openGid = ref('')
async function toggleFriendLands(gid, landsEl) {
  if (!acc()) return
  openGid.value = openGid.value === String(gid) ? '' : String(gid)
  const key = String(gid)
  if (openGid.value === key && friendLandsMap[key] === undefined) {
    friendLandsMap[key] = 'loading'
    try {
      const { data } = await api.get(`/api/friends/lands?gid=${encodeURIComponent(gid)}`)
      friendLandsMap[key] = (data?.ok && Array.isArray(data.data)) ? data.data : []
    } catch (e) { friendLandsMap[key] = [] }
  }
}
const FD_STATUS = { ready: '可收获', growing: '生长中', dry: '干旱', idle: '空地', dead: '枯萎', locked: '未解锁', stealable: '可收获', empty: '空地' }
async function loadFriends() {
  if (!acc()) return
  try { const { data } = await api.get('/api/friends/list'); friends.value = (data?.data?.friends) || [] } catch (e) {}
}
async function friendOp(gid, act) {
  if (!acc()) return
  try {
    let d
    if (act === 'black') d = (await api.post('/api/friend-blacklist/toggle', { gid: String(gid) }).catch(e => e.response)).data
    else if (act === 'del') { d = (await api.post(`/api/friend/${gid}/delete`, {}).catch(e => e.response)).data; loadFriends(); return app.success(d?.ok ? '删除 完成' : ('操作失败：' + (d?.error || '未知'))) }
    else d = (await api.post(`/api/friend/${gid}/op`, { opType: act }).catch(e => e.response)).data
    app.success(d?.ok ? ((act === 'black' ? '已切换黑名单' : act) + ' 完成') : ('操作失败：' + (d?.error || '未知')))
    if (act === 'black') { loadFriends(); loadBlacklist() }
  } catch (e) { app.error('请求失败') }
}
async function fetchDogInfo() {
  if (!acc()) return
  const d = (await api.post('/api/friends/fetch-dog-info', {}).catch(e => e.response)).data
  app.success(d?.ok ? '狗信息已更新' : ('获取失败：' + (d?.error || '未知')))
  loadFriends()
}
/* 加好友（对齐 legacy parseShareLink + /api/friend/apply） */
const addLink = ref(''); const addPreview = ref(''); const addMsg = ref('')
function refreshPreview() {
  const d = parseShare(addLink.value)
  if (d.uid || d.openid || d.shareKey) addPreview.value = `解析：uid=${d.uid || '?'} openid=${d.openid || '?'} share_key=${(d.shareKey || '?').slice(0, 8)}…`
  else addPreview.value = ''
}
function parseShare(raw) {
  const q = (raw || '').indexOf('?') >= 0 ? raw.slice(raw.indexOf('?') + 1) : raw
  const s = new URLSearchParams(q)
  return { uid: s.get('uid'), openid: s.get('openid'), shareKey: s.get('share_key') }
}
async function sendFriendApply() {
  if (!acc()) { app.error('请先选择账号'); return }
  const d = parseShare(addLink.value)
  const uid = (d.uid || '').trim(), openid = (d.openid || '').trim(), share = ((d.shareKey || '').trim()).toLowerCase()
  if (!/^\d+$/.test(uid) || !openid || !/^[0-9a-f]{32}$/.test(share)) { addMsg.value = '分享链接缺少有效 uid / openid / share_key（32位十六进制）'; return }
  addMsg.value = '发送中…'
  try {
    const { data } = await api.post('/api/friend/apply', { gid: Number(uid), openid, shareKey: share })
    addMsg.value = data?.ok ? ('已发送好友申请：uid=' + uid) : ('失败：' + (data?.error || data?.rawError || '未知'))
  } catch (e) { addMsg.value = '请求失败: ' + e.message }
}
/* 黑名单（对齐 legacy：update skipSteal/skipHelp + toggle 移出） */
const blackList = ref([]); const blk = ref(0)
async function loadBlacklist() { if (!acc()) return; try { const { data } = await api.get('/api/friends/blacklist'); blackList.value = data?.data || []; blk.value = blackList.value.length } catch (e) {} }
async function blkToggleSkip(b, which, on) {
  if (!acc()) return
  const payload = { gid: String(b.uid) }; payload[which === 'steal' ? 'skipSteal' : 'skipHelp'] = on
  await api.post('/api/friend-blacklist/update', payload).catch(() => {})
  loadBlacklist()
}
async function rmBlack(gid) {
  if (!acc()) return
  await api.post('/api/friend-blacklist/toggle', { gid: String(gid) }).catch(() => {})
  loadBlacklist(); loadFriends()
}
/* 访客（对齐 legacy：actionType 数字 1/2/3 + 时间格式化） */
const visitors = ref([]); const vFilter = ref('all')
const V_BADGE = { 1: 'v-badge steal', 2: 'v-badge help', 3: 'v-badge bad' }
const V_TEXT = { 1: '偷取', 2: '帮忙', 3: '捣乱' }
const vStats = computed(() => { const v = visitors.value; return { total: v.length, steal: v.filter(x => Number(x.actionType) === 1).length, help: v.filter(x => Number(x.actionType) === 2).length, bad: v.filter(x => Number(x.actionType) === 3).length } })
function fmtInteract(ts) {
  ts = Number(ts) || 0; if (!ts) return '--'
  const date = new Date(ts), now = new Date(), diff = now.getTime() - date.getTime(), minute = 60000, hour = 3600000
  if (diff >= 0 && diff < minute) return '刚刚'
  if (diff >= minute && diff < hour) return Math.floor(diff / minute) + ' 分钟前'
  const p = n => String(n).padStart(2, '0')
  if (now.toDateString() === date.toDateString()) return '今天 ' + p(date.getHours()) + ':' + p(date.getMinutes())
  if (now.getFullYear() === date.getFullYear()) return (date.getMonth() + 1) + '-' + date.getDate() + ' ' + p(date.getHours()) + ':' + p(date.getMinutes())
  return date.getFullYear() + '-' + p(date.getMonth() + 1) + '-' + p(date.getDate()) + ' ' + p(date.getHours()) + ':' + p(date.getMinutes())
}
const shownVisitors = computed(() => {
  const map = { steal: 1, help: 2, bad: 3 }; const tgt = map[vFilter.value] || 0
  let v = visitors.value; if (vFilter.value !== 'all') v = v.filter(r => Number(r.actionType) === tgt)
  return v.slice(0, 50)
})
const vname = v => v.nick || (v.visitorGid ? ('GID:' + v.visitorGid) : (v.name || '访客'))
const vdetail = v => v.actionDetail || v.actionLabel || v.action || ''
const vtime = v => fmtInteract(Number(v.serverTimeMs || v.timeSec || 0))
async function loadVisitors() { if (!acc()) return; try { const { data } = await api.get('/api/friends/visitors'); visitors.value = (Array.isArray(data?.data) ? data.data : []) } catch (e) {} }
/* 批量删除（对齐 legacy：等级阈值 + 保留护主犬 + 勾选 + password） */
const delLevel = ref(30); const delSkipGuard = ref(true); const delPwd = ref(''); const delSel = ref(new Set())
const delSorted = computed(() => friends.value.slice())
function toggleDel(id) { const s = new Set(delSel.value); s.has(id) ? s.delete(id) : s.add(id); delSel.value = s }
async function delBatch() {
  if (!acc()) return
  const lvl = Number(delLevel.value) || 0, skipGuard = delSkipGuard.value, pwd = delPwd.value || ''
  const gids = []
  if (lvl > 0) friends.value.forEach(f => { if (Number(f.level) > lvl) return; if (skipGuard && isGuard(f)) return; if (gids.indexOf(Number(f.uid ?? f.gid)) === -1) gids.push(Number(f.uid ?? f.gid)) })
  delSel.value.forEach(g => { if (g > 0 && gids.indexOf(g) === -1) gids.push(g) })
  if (!gids.length) { app.error('请填写等级阈值或勾选好友'); return }
  if (!confirm('确认批量删除 ' + gids.length + ' 名好友？此操作不可恢复')) return
  const { data } = await api.post('/api/friend/batch-delete', { gids, password: pwd }).catch(e => e.response || { data: {} })
  if (data?.ok) { app.success('成功 ' + (data.successCount || 0) + ' / 失败 ' + (data.failedCount || 0)); loadFriends() }
  else app.error('失败：' + (data?.error || '未知'))
}

/* ---------------- 每日任务（对齐 legacy：/api/task/daily） ---------------- */
const dailyTasks = ref([]); const growthTasks = ref([]); const taskDone = ref('--'); const taskClaim = ref('--')
async function loadTasks() {
  if (!acc()) return
  try {
    const { data } = await api.get('/api/task/daily')
    const d = data || {}
    dailyTasks.value = d.daily || []; growthTasks.value = d.growth || []
    taskDone.value = d.daily_done != null ? d.daily_done : ((d.daily || []).length)
    taskClaim.value = d.daily_claimable != null ? d.daily_claimable : 0
  } catch (e) { taskDone.value = '-'; taskClaim.value = '-' }
}
function canClaim(t) { return t.is_unlocked && !t.is_claimed && t.total > 0 && t.progress >= t.total }
function taskPct(t) { return t.total > 0 ? Math.min(100, Math.round(t.progress / t.total * 100)) : 0 }
async function claimTask(taskId) {
  if (!acc()) return
  const { data } = await api.post('/api/task/claim', { taskId: Number(taskId) }).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '领取成功' : ('失败：' + (data?.error || '未知')))
  loadTasks()
}

/* ---------------- 护主犬（对齐 legacy：d.claimable 顶层） ---------------- */
const dogClaimable = ref('--'); const dogMsg = ref('')
async function loadDog() {
  if (!acc()) return
  try { const { data } = await api.get('/api/dog/gifts'); dogClaimable.value = (data && data.ok !== false && data.claimable != null) ? data.claimable : '--' } catch (e) { dogClaimable.value = '--' }
}
async function claimDog() {
  if (!acc()) return
  dogMsg.value = ''
  const { data } = await api.post('/api/dog/gifts/claim', {}).catch(e => e.response || { data: {} })
  if (data?.ok) { dogMsg.value = '本次领取 ' + data.claimed + ' 个（已入背包，可打开获得金币/道具）'; loadDog() }
  else dogMsg.value = (data?.error) || ''
}

async function onTab(c) {
  tab.value = c
  if (c === 'p-farm') await loadLands()
  else if (c === 'p-bag') await loadBag()
  else if (c === 'p-friends') { await loadFriends(); await loadBlacklist(); await loadVisitors() }
  else if (c === 'p-daily') await loadTasks()
  else if (c === 'p-dog') await loadDog()
}
async function onFsub(s) {
  fsub.value = s
  if (s === 'list') await loadFriends()
  else if (s === 'blacklist') await loadBlacklist()
  else if (s === 'visitors') await loadVisitors()
}

onMounted(() => { if (!acc()) return; loadLands(); landTimer = setInterval(landCountdown, 1000) })
onUnmounted(() => { clearInterval(landTimer) })
</script>

<template>
  <div>
    <div class="seg seg-5">
      <button class="seg-btn" :class="{ active: tab === 'p-farm' }" @click="onTab('p-farm')">🌾 农场</button>
      <button class="seg-btn" :class="{ active: tab === 'p-bag' }" @click="onTab('p-bag')">🎒 背包</button>
      <button class="seg-btn" :class="{ active: tab === 'p-friends' }" @click="onTab('p-friends')">👥 好友</button>
      <button class="seg-btn" :class="{ active: tab === 'p-daily' }" @click="onTab('p-daily')">每日任务</button>
      <button class="seg-btn" :class="{ active: tab === 'p-dog' }" @click="onTab('p-dog')">护主犬</button>
    </div>

    <!-- 农场 -->
    <div v-show="tab === 'p-farm'">
      <div class="farm-actions">
        <button class="fa-btn fa-harvest" @click="farmAction('harvest')">🌾 收获</button>
        <button class="fa-btn fa-work" @click="farmAction('work')">🚿 一键务农</button>
        <button class="fa-btn fa-plant" @click="farmAction('plant')">🌱 种植</button>
        <button class="fa-btn fa-upgrade" @click="farmAction('upgrade')">🏠 升级土地</button>
        <button class="fa-btn fa-full" @click="farmAction('full')">⚡ 一键全收</button>
        <button class="fa-btn fa-clear fa-remove-all" @click="removeAll">🗑️ 一键铲除</button>
      </div>
      <div class="land">
        <div v-for="l in lands" :key="l.id" :class="landCls(l)">
          <span class="lc-id">#{{ l.id }}</span>
          <span v-if="Number(l.plantSize) > 1" class="lc-merged-badge">合种 {{ l.plantSize }}x{{ l.plantSize }}</span>
          <div class="lc-mutants"><img v-for="m in (l.mutantEffects||[]).filter(x=>x&&x.icon)" :key="m.icon" :src="'/game-config/seed_images_named/mutant/' + m.icon + '.png'" :alt="m.name||'变异'" :title="m.name" loading="lazy" style="width:18px;height:18px"></div>
          <div class="lc-img"><img v-if="l.seedImage" :src="l.seedImage" loading="lazy" referrerpolicy="no-referrer" style="width:52px;height:52px;object-fit:contain"><span v-else style="font-size:22px">🌱</span></div>
          <div class="lc-name" :title="l.plantName">{{ l.plantName || '-' }}</div>
          <div class="lc-meta">{{ l.matureInSec > 0 ? ('预计 ' + fmtDur(l.__left ?? l.matureInSec) + ' 后成熟') : (l.phaseName || (l.status === 'locked' ? '未解锁' : '未开垦')) }}</div>
          <div v-if="l.matureInSec>0 && l.totalGrowTime>0" class="lc-bar"><i :style="{ width: landGrowPct(l) + '%' }"></i></div>
          <div class="lc-type">{{ l.landTypeName || '' }}</div>
          <div class="lc-season">季数 {{ (l.totalSeason||0)>0 ? l.currentSeason + '/' + l.totalSeason : '-/-' }}</div>
          <div v-if="landTags(l).length" class="lc-tags"><span v-for="c in landTags(l)" :key="c" :class="c">{{ landTagText(c) }}</span></div>
          <div class="plot-btns">
            <template v-if="landCanFertilize(l) || landCanRemove(l)">
              <button v-if="landCanFertilize(l)" class="p-cuisi" title="催熟" @click="landOp(l,'fertilize')">🌿催熟</button>
              <div v-else></div>
              <button v-if="landCanRemove(l)" class="p-chan" title="铲除作物" @click="landOp(l,'remove')">🗑铲除</button>
            </template>
          </div>
        </div>
        <p v-if="!lands.length" style="text-align:center;color:var(--muted);padding:24px 0">暂无土地数据</p>
      </div>
    </div>

    <!-- 背包 -->
    <div v-show="tab === 'p-bag'">
      <div class="sec-title" style="margin-top:16px"><span>🎒 背包</span><span class="link" @click="toggleSellMode">🗑️ {{ bagSellMode ? '取消' : '批量出售' }}</span></div>
      <div class="seg seg-5 bag-cats" style="margin-bottom:12px">
        <button v-for="(label, k) in { '': '全部', fruit: '果实', seed: '种子', props: '道具', other: '其他' }" :key="k" class="seg-btn" :class="{ active: bagCat === k }" @click="bagCat = k">{{ label }} ({{ bagCounts[k] }})</button>
      </div>
      <div class="bag-grid">
        <div v-for="it in bagShown" :key="it.id" class="bag-item">
          <span class="bi-id">{{ it.id }}</span>
          <div class="bi-icon"><img v-if="it.img" :src="it.img" style="width:34px;height:34px;object-fit:contain;border-radius:8px"><span v-else style="font-size:20px">{{ it.icon || '📦' }}</span></div>
          <div class="bi-name">{{ it.name }}</div>
          <div class="bi-meta">数量 ×{{ it.count }}</div>
          <div v-if="bagCanUse(it) || bagCanSell(it)" class="bi-acts">
            <label v-if="bagSellMode && bagCanSell(it)" class="bi-sel"><input type="checkbox" :checked="bagSel.has(it.id)" @change="toggleSel(it.id)"> 选</label>
            <template v-else>
              <button v-if="bagCanUse(it)" class="bi-use" @click="bagUse(it)">用</button>
              <button v-if="bagCanSell(it)" class="bi-sell" @click="bagSellOne(it)">售</button>
            </template>
          </div>
        </div>
        <p v-if="!bagShown.length" style="text-align:center;color:var(--muted);margin-top:24px">该分类暂无物品</p>
      </div>
      <div v-if="bagSellMode" style="position:fixed;left:0;right:0;bottom:0;padding:12px 16px;background:var(--card);border-top:1px solid var(--border);display:flex;align-items:center;gap:12px;z-index:200">
        <span style="color:var(--muted);font-size:13px">勾选要出售的果实</span>
        <button class="seg-btn" style="margin-left:auto" @click="bagSellSel">确认出售选中 ({{ bagSel.size }})</button>
      </div>
    </div>

    <!-- 好友 -->
    <div v-show="tab === 'p-friends'">
      <div class="seg seg-5" style="margin-bottom:10px">
        <button class="seg-btn" :class="{ active: fsub === 'list' }" @click="onFsub('list')">好友列表</button>
        <button class="seg-btn" :class="{ active: fsub === 'add' }" @click="fsub='add'">加好友</button>
        <button class="seg-btn" :class="{ active: fsub === 'blacklist' }" @click="onFsub('blacklist')">黑名单<span v-if="blk > 0" class="nb">{{ blk }}</span></button>
        <button class="seg-btn" :class="{ active: fsub === 'visitors' }" @click="onFsub('visitors')">访客</button>
        <button class="seg-btn" :class="{ active: fsub === 'del' }" @click="onFsub('del')">删除</button>
      </div>

      <div v-if="fsub === 'list'">
        <input class="field" v-model="friendSearch" placeholder="🔍 搜索好友…" style="margin-top:16px">
        <p style="font-size:11px;color:var(--muted);margin:6px 2px 10px" id="frTotal">共 {{ shownFriends.length }} 名好友</p>
        <div class="friend-filter" style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px">
          <button class="chip" :class="{ on: friendDogFilter === 'all' }" @click="friendDogFilter='all'">全部</button>
          <button class="chip" :class="{ on: friendDogFilter === 'noGuardDog' }" @click="friendDogFilter='noGuardDog'">无护主犬</button>
          <button class="chip" :class="{ on: friendDogFilter === 'guardDog' }" @click="friendDogFilter='guardDog'">有护主犬</button>
          <button class="chip" style="margin-left:auto" @click="fetchDogInfo">🐶 获取狗信息</button>
          <button class="chip" @click="loadFriends">🔄 刷新</button>
        </div>
        <div>
          <div v-for="f in shownFriends" :key="String(f.uid ?? f.gid)" class="friend-card" :data-uid="String(f.uid ?? f.gid)">
            <div class="fc-head" @click="toggleFriendLands(f.uid ?? f.gid)">
              <div class="f-av"><img v-if="/^(https?:)?\/\//i.test(f.avatar || '')" :src="f.avatar" alt=""><span v-else>{{ f.avatar || '👤' }}</span></div>
              <div class="f-info">
                <h4>{{ f.name || '' }} <small class="fc-id">({{ f.uid ?? f.gid }})</small><span v-if="isGuard(f)" class="ripeness fc-dog">护主犬</span></h4>
                <p>Lv.{{ f.level || '-' }}<span v-if="f.coins != null"> · 金币 {{ Number(f.coins || 0).toLocaleString() }}</span><span v-if="f.ripeLands"> · 可收 {{ f.ripeLands }} 块</span></p>
              </div>
              <span class="fc-arrow">{{ openGid === String(f.uid ?? f.gid) ? '▴' : '▾' }}</span>
            </div>
            <p v-if="f.tip" class="fc-tip">{{ f.tip }}</p>
            <div class="fc-actions">
              <button v-if="f.canSteal" class="fa-mini fs1" @click="friendOp(f.uid ?? f.gid, 'steal')">🥷 偷取</button>
              <template v-if="f.canHelp">
                <button class="fa-mini fs2" @click="friendOp(f.uid ?? f.gid, 'water')">💧 浇水</button>
                <button class="fa-mini fs3" @click="friendOp(f.uid ?? f.gid, 'weed')">🌿 除草</button>
                <button class="fa-mini fs4" @click="friendOp(f.uid ?? f.gid, 'bug')">🐛 除虫</button>
              </template>
            </div>
            <div class="fc-more-actions">
              <button class="fa-mini fs5" @click="friendOp(f.uid ?? f.gid, 'bad')">🎭 捣乱</button>
              <button class="fa-mini fs6" @click="friendOp(f.uid ?? f.gid, 'black')">🚫 拉黑</button>
              <button class="fa-mini fs7" @click="friendOp(f.uid ?? f.gid, 'del')">🗑️ 删除</button>
            </div>
            <div v-if="openGid === String(f.uid ?? f.gid)" class="fc-lands">
              <p v-if="friendLandsMap[String(f.uid ?? f.gid)] === 'loading'" class="f-land-empty">地块加载中…</p>
              <p v-else-if="!friendLandsMap[String(f.uid ?? f.gid)] || !friendLandsMap[String(f.uid ?? f.gid)].length" class="f-land-empty">暂无地块</p>
              <div v-else v-for="l in friendLandsMap[String(f.uid ?? f.gid)]" :key="l.id" class="f-land">
                <div class="f-l-icon">{{ l.img ? '' : (l.status === 'locked' ? '🔒' : (l.name ? '🌱' : '⬛')) }}<img v-if="l.img" :src="l.img" alt=""></div>
                <div class="f-l-name">{{ l.name || '空地' }}<em>{{ FD_STATUS[l.status] || l.status || '' }}</em></div>
                <div v-if="l.progress != null && l.status !== 'locked'" class="bar"><i :style="{ width: l.progress + '%' }"></i></div>
              </div>
            </div>
          </div>
          <p v-if="!shownFriends.length" style="text-align:center;color:var(--muted);margin-top:24px">暂无好友</p>
        </div>
      </div>

      <div v-if="fsub === 'add'">
        <p style="font-size:11px;color:var(--muted);margin:8px 2px 10px">粘贴游戏分享链接，自动解析后发送好友申请</p>
        <textarea v-model="addLink" @input="refreshPreview" class="field" rows="2" placeholder="分享链接" style="margin-top:8px;min-height:52px;resize:vertical"></textarea>
        <div style="font-size:11px;color:var(--muted);margin-top:8px">{{ addPreview }}</div>
        <button class="close" style="margin-top:12px" @click="sendFriendApply">发送好友申请</button>
        <p style="font-size:11px;color:var(--muted);text-align:center;margin-top:8px">{{ addMsg }}</p>
      </div>

      <div v-if="fsub === 'blacklist'">
        <p style="font-size:11px;color:var(--muted);margin:8px 2px 10px">以下好友已被加入黑名单</p>
        <div v-for="b in blackList" :key="String(b.uid)" class="friend-card">
          <div class="fc-head">
            <div class="f-av">{{ b.avatar ? '' : '🤖' }}<img v-if="/^(https?:)?\/\//i.test(b.avatar || '')" :src="b.avatar" alt=""></div>
            <div class="f-info"><h4>{{ b.name || '' }} <small class="fc-id">({{ b.uid }})</small></h4><p>{{ b.reason || '无记录' }}<span v-if="b.addedAt"> · {{ b.addedAt }}</span></p></div>
          </div>
          <div class="fc-actions" style="grid-template-columns:repeat(2,1fr)">
            <label class="f-toggle"><input type="checkbox" :checked="!!b.skipSteal" @change="blkToggleSkip(b, 'steal', $event.target.checked)"> 不偷</label>
            <label class="f-toggle"><input type="checkbox" :checked="!!b.skipHelp" @change="blkToggleSkip(b, 'help', $event.target.checked)"> 不帮忙</label>
          </div>
          <div class="fc-more-actions" style="display:grid"><button class="fa-mini fs6" @click="rmBlack(b.uid)">🚫 移出黑名单</button></div>
        </div>
        <p v-if="!blackList.length" style="text-align:center;color:var(--muted);margin-top:24px">黑名单为空</p>
      </div>

      <div v-if="fsub === 'visitors'">
        <div class="visitor-stats" style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin:8px 0">
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block">{{ vStats.total }}</b><span style="font-size:11px;color:var(--muted)">访客总数</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block">{{ vStats.steal }}</b><span style="font-size:11px;color:var(--muted)">偷取</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block">{{ vStats.help }}</b><span style="font-size:11px;color:var(--muted)">帮忙</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block">{{ vStats.bad }}</b><span style="font-size:11px;color:var(--muted)">捣乱</span></div>
        </div>
        <div class="visitor-filters" style="display:flex;gap:6px;flex-wrap:wrap;margin:8px 0">
          <button class="chip" :class="{ on: vFilter === 'all' }" @click="vFilter='all'">全部</button>
          <button class="chip" :class="{ on: vFilter === 'steal' }" @click="vFilter='steal'">偷菜</button>
          <button class="chip" :class="{ on: vFilter === 'help' }" @click="vFilter='help'">帮忙</button>
          <button class="chip" :class="{ on: vFilter === 'bad' }" @click="vFilter='bad'">捣乱</button>
          <button class="chip" style="margin-left:auto" @click="loadVisitors">刷新</button>
        </div>
        <p style="font-size:11px;color:var(--muted);text-align:center;margin:4px 0 8px">仅展示最近 50 条访客记录</p>
        <div v-for="(v, idx) in shownVisitors" :key="v.key || (v.visitorGid + '-' + v.serverTimeMs + '-' + v.actionType + '-' + idx)" class="visitor-card">
          <div class="fc-head">
            <div class="f-av"><img v-if="/^(https?:)?\/\//i.test(v.avatarUrl || v.avatar || '')" :src="v.avatarUrl || v.avatar" alt=""><span v-else>访</span></div>
            <div class="f-info"><h4>{{ vname(v) }} <span :class="V_BADGE[v.actionType] || 'v-badge'">{{ V_TEXT[v.actionType] || '互动' }}</span><span v-if="v.level > 0" class="v-lvl">Lv.{{ v.level }}</span> <small class="fc-id">{{ v.visitorGid ? ('GID ' + v.visitorGid) : '' }}</small></h4><p>{{ vdetail(v) }}</p></div>
          </div>
          <div class="v-time">{{ vtime(v) }}</div>
        </div>
        <p v-if="!shownVisitors.length" style="text-align:center;color:var(--muted);padding:20px 0">暂无访客记录</p>
      </div>

      <div v-if="fsub === 'del'">
        <p style="font-size:11px;color:var(--muted);margin:8px 2px 10px">按等级批量删除，或勾选下方好友（保留护主犬，二级密码）</p>
        <div class="del-form" style="display:flex;flex-direction:column;gap:8px">
          <label style="font-size:12.5px">等级 ≤ <input v-model.number="delLevel" class="field" type="number" placeholder="如30" style="width:90px;display:inline-block"></label>
          <label style="font-size:12.5px"><input type="checkbox" v-model="delSkipGuard" checked> 保留护主犬</label>
          <input v-model="delPwd" class="field" type="password" placeholder="二级密码（可选）">
          <div style="display:flex;gap:8px;align-items:center"><button class="f-batch" @click="delBatch">批量删除</button><span style="font-size:11px;color:var(--muted)">匹配 {{ delSorted.filter(f => Number(f.level) <= (Number(delLevel.value)||0) && (!delSkipGuard.value || Number(f.dogId) !== 90021)).length + delSel.size }} 名</span></div>
        </div>
        <div style="margin-top:14px">
          <div v-for="f in delSorted" :key="String(f.uid ?? f.gid)" class="friend-card">
            <div class="fc-head">
              <div class="f-av">{{ f.avatar || '👤' }}</div>
              <div class="f-info"><h4>{{ f.name || '' }} <small class="fc-id">({{ f.uid ?? f.gid }})</small><span v-if="isGuard(f)" class="ripeness fc-dog">护主犬</span></h4><p>Lv.{{ f.level || '-' }}</p></div>
              <label class="f-toggle"><input type="checkbox" class="del-pick" :checked="delSel.has(Number(f.uid ?? f.gid))" @change="toggleDel(Number(f.uid ?? f.gid))"> 选</label>
            </div>
          </div>
          <p v-if="!delSorted.length" style="text-align:center;color:var(--muted);margin-top:24px">暂无好友</p>
        </div>
      </div>
    </div>

    <!-- 每日任务 -->
    <div v-show="tab === 'p-daily'">
      <div class="sec-title" style="margin-top:16px;margin-bottom:10px"><span>📋 每日任务</span></div>
      <div style="display:flex;gap:8px;margin-bottom:10px">
        <div style="flex:1;border-radius:14px;background:var(--card-strong);padding:12px;text-align:center"><div style="font-size:22px;font-weight:800;color:var(--primary)">{{ taskDone }}</div><div style="font-size:11px;color:var(--muted);margin-top:2px">今日已完成</div></div>
        <div style="flex:1;border-radius:14px;background:var(--card-strong);padding:12px;text-align:center"><div style="font-size:22px;font-weight:800;color:var(--primary)">{{ taskClaim }}</div><div style="font-size:11px;color:var(--muted);margin-top:2px">可领取</div></div>
      </div>
      <div style="border-radius:14px;background:var(--card-strong);padding:12px;margin-bottom:10px">
        <div class="f-label" style="margin:2px 0 8px">每日任务</div>
        <div v-for="t in dailyTasks" :key="t.id">
          <div style="padding:9px 0;border-bottom:1px solid var(--line,rgba(128,128,128,.15))">
            <div style="display:flex;align-items:center;gap:8px">
              <div style="flex:1;font-size:12.5px">{{ t.desc || ('任务#' + t.id) }}<div style="font-size:10.5px;color:var(--muted);margin-top:2px">{{ t.progress }}/{{ t.total }}{{ t.is_claimed ? ' · 已领取' : '' }}</div></div>
              <button v-if="canClaim(t)" class="chip" @click="claimTask(t.id)">领取</button>
              <span v-else-if="t.is_claimed" style="font-size:12px;color:var(--primary)">✓ 已领</span>
            </div>
            <div style="height:4px;border-radius:2px;background:var(--line,rgba(128,128,128,.2));margin-top:5px;overflow:hidden"><div :style="{ height: '100%', width: taskPct(t) + '%', background: 'var(--primary)', borderRadius: '2px' }"></div></div>
          </div>
        </div>
        <p v-if="!dailyTasks.length" style="color:var(--muted);padding:6px 0">暂无任务</p>
      </div>
      <div style="border-radius:14px;background:var(--card-strong);padding:12px">
        <div class="f-label" style="margin:2px 0 8px">成长任务</div>
        <div v-for="t in growthTasks" :key="t.id">
          <div style="padding:9px 0;border-bottom:1px solid var(--line,rgba(128,128,128,.15))">
            <div style="display:flex;align-items:center;gap:8px">
              <div style="flex:1;font-size:12.5px">{{ t.desc || ('任务#' + t.id) }}<div style="font-size:10.5px;color:var(--muted);margin-top:2px">{{ t.progress }}/{{ t.total }}{{ t.is_claimed ? ' · 已领取' : '' }}</div></div>
              <button v-if="canClaim(t)" class="chip" @click="claimTask(t.id)">领取</button>
              <span v-else-if="t.is_claimed" style="font-size:12px;color:var(--primary)">✓ 已领</span>
            </div>
            <div style="height:4px;border-radius:2px;background:var(--line,rgba(128,128,128,.2));margin-top:5px;overflow:hidden"><div :style="{ height: '100%', width: taskPct(t) + '%', background: 'var(--primary)', borderRadius: '2px' }"></div></div>
          </div>
        </div>
        <p v-if="!growthTasks.length" style="color:var(--muted);padding:6px 0">暂无任务</p>
      </div>
    </div>

    <!-- 护主犬 -->
    <div v-show="tab === 'p-dog'">
      <div class="sec-title" style="margin-top:16px;margin-bottom:10px"><span>🐕 护主犬奖励</span></div>
      <div style="border-radius:16px;background:var(--card-strong);padding:16px">
        <div style="display:flex;align-items:baseline;gap:6px;margin-top:12px">
          <span style="font-size:34px;font-weight:800;color:var(--primary)">{{ dogClaimable }}</span>
          <span style="font-size:12px;color:var(--muted)">个可领取</span>
        </div>
        <p style="font-size:11.5px;color:var(--muted);margin-top:8px;line-height:1.7">帮忙好友有机会获得「同气连枝礼包」，点击领取即收入背包，可开出金币/道具。</p>
        <button class="f-batch" style="margin-top:14px;width:100%" @click="claimDog">🎁 领取同气礼包</button>
        <p style="font-size:11px;color:var(--muted);margin-top:8px;min-height:16px">{{ dogMsg }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
.visitor-card { display: flex; align-items: center; gap: 8px; padding: 8px 4px; border-bottom: 1px solid var(--border); font-size: 12.5px; justify-content: space-between; }
.v-time { flex: none; font-size: 11px; color: var(--muted); }
.f-l-name em { display: flex; align-items: center; font-style: normal; color: var(--primary); font-size: 11px; margin-left: 6px; gap: 8px; }
.fc-actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
.fa-mini { border: 1px solid var(--border); background: var(--card-strong); border-radius: 7px; font-size: 11px; color: var(--foreground); cursor: pointer; padding: 4px 8px; }
.fa-mini:active { transform: scale(.96); }
.fc-more-actions { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 8px; }
</style>
