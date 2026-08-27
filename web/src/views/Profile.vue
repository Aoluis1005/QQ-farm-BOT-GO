<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import api, { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const acc = () => getAccountId()
const tab = ref('p-farm')
const fsub = ref('list')

/* ---------------- 农场 ---------------- */
const lands = ref([])
const landTick = ref(0)
let landTimer = null
function landCanFertilize(l) { return Number(l.matureInSec || 0) > 0 && l.status !== 'locked' && l.status !== 'empty' }
// 大数友好缩写：≥1亿→X.XX亿，≥1万→X.X万，否则千分位
function fmtBig(n) {
  if (n === undefined || n === null || n === '') return 0
  const v = Number(String(n).replace(/,/g, ''))
  if (isNaN(v)) return 0
  if (v >= 1e8) return (v / 1e8).toFixed(2).replace(/\.0+$/, '') + '亿'
  if (v >= 1e4) return (v / 1e4).toFixed(1).replace(/\.0$/, '') + '万'
  return v.toLocaleString()
}
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

/* ---------------- 背包 ---------------- */
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

/* ---------------- 好友 ---------------- */
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
// 手动添加"已知好友 GID"
const knownGids = ref([]); const knownOpen = ref(false); const knownGidInput = ref('')
async function loadKnownGids() {
  try {
    const d = await api.get('/api/friend-known-gids')
    knownGids.value = Array.isArray(d?.data?.data) ? d.data.data : (Array.isArray(d?.data) ? d.data : [])
  } catch (e) {}
}
async function addKnownGids() {
  const gids = String(knownGidInput.value || '').split(/[,，\s]+/).map(x => Number(x.trim())).filter(x => x > 0)
  if (!gids.length) { app.error('请输入有效的 GID（多个用逗号分隔）'); return }
  try {
    const d = await api.post('/api/friend-known-gids/batch-add', { gids })
    app.success(d?.data?.message || `已添加 ${gids.length} 个已知好友 GID`)
    knownGidInput.value = ''
    await loadKnownGids(); loadFriends()
  } catch (e) { app.error('添加失败：' + (e.response?.data?.error || e.message)) }
}
async function removeKnownGid(gid) {
  try {
    await api.post('/api/friend-known-gids/remove', { gid: String(gid) })
    await loadKnownGids(); loadFriends()
  } catch (e) {}
}
/* 加好友：批量解析 + 串行队列 */
const addText = ref('')
const addItems = ref([])   // { uid, openid, shareKey, status, sel, err }
const addMsg = ref('')
let applyTimer = null

function parseShare(raw) {
  const q = (raw || '').indexOf('?') >= 0 ? raw.slice(raw.indexOf('?') + 1) : raw
  const s = new URLSearchParams(q)
  let uid = s.get('uid'), openid = s.get('openid'), key = s.get('share_key') || s.get('sharekey')
  if (!uid && !openid && !key) {
    const parts = (raw || '').trim().split(/[\s,，]+/).filter(Boolean)
    if (parts.length >= 3) { [uid, openid, key] = parts }
  }
  return { uid: (uid || '').trim(), openid: (openid || '').trim(), shareKey: (key || '').trim().toLowerCase() }
}
function validItem(d) {
  return /^\d+$/.test(d.uid) && d.openid && /^[0-9a-f]{32}$/.test(d.shareKey)
}
function addParsed(text) {
  let n = 0
  const have = new Set(addItems.value.map(i => i.uid))
  ;(text || '').split(/\r?\n/).forEach(line => {
    const t = line.trim(); if (!t) return
    const d = parseShare(t)
    if (validItem(d) && !have.has(d.uid)) {
      have.add(d.uid)
      addItems.value.push({ uid: d.uid, openid: d.openid, shareKey: d.shareKey, status: 'pending', sel: false })
      n++
    }
  })
  return n
}
function onAddInput(e) {
  const added = addParsed(e.target.value)
  if (added) { e.target.value = ''; addMsg.value = '自动解析 +' + added + ' 条'; startApplyPoll() }
}
function onAddFile(e) {
  const f = e.target.files[0]; if (!f) return
  const r = new FileReader()
  r.onload = ev => { const n = addParsed(ev.target.result); addMsg.value = n ? ('已解析 ' + n + ' 条') : '未识别到有效数据'; if (n) startApplyPoll() }
  r.readAsText(f); e.target.value = ''
}
/* 工具栏：全选 / 删除 / 发送 / 取消发送 */
const selCount = computed(() => addItems.value.filter(i => i.sel).length)
const allSel = computed(() => addItems.value.length > 0 && addItems.value.every(i => i.sel))
function toggleAll() { const on = !allSel.value; addItems.value.forEach(i => i.sel = on) }
function delSel() { const b = addItems.value.length; addItems.value = addItems.value.filter(i => !i.sel); addMsg.value = '已删除 ' + (b - addItems.value.length) + ' 条' }
function delOne(uid) { addItems.value = addItems.value.filter(i => i.uid !== uid) }
async function sendSel() {
  if (!acc()) { app.error('请先选择账号'); return }
  const pick = addItems.value.filter(i => i.sel && i.status !== 'sent' && i.status !== 'cancelled')
  if (!pick.length) { addMsg.value = '请勾选待发送的数据'; return }
  try {
    await api.post('/api/friend/apply/batch', { items: pick.map(i => ({ gid: i.uid, openid: i.openid, shareKey: i.shareKey })) })
    pick.forEach(i => { if (i.status !== 'sent' && i.status !== 'cancelled') i.status = 'sending' })
    addMsg.value = '已提交 ' + pick.length + ' 条，后台串行发送中…'
    startApplyPoll()
  } catch (e) { addMsg.value = '提交失败: ' + e.message }
}
async function cancelSel() {
  const pick = addItems.value.filter(i => i.sel && i.status !== 'sent' && i.status !== 'cancelled')
  if (!pick.length) { addMsg.value = '没有可取消的发送'; return }
  try {
    await api.post('/api/friend/apply/cancel', { gids: pick.map(i => i.uid) })
    pick.forEach(i => { i.status = 'cancelled' })
    addMsg.value = '已取消 ' + pick.length + ' 条，发送已停止'
    startApplyPoll()
  } catch (e) { addMsg.value = '取消失败: ' + e.message }
}
/* 状态轮询：合并后端队列状态到卡片 */
function startApplyPoll() {
  if (applyTimer) return
  applyTimer = setInterval(pollApply, 1500)
  pollApply()
}
function stopApplyPoll() { if (applyTimer) { clearInterval(applyTimer); applyTimer = null } }
async function pollApply() {
  if (!acc() || !addItems.value.length) return
  try {
    const { data } = await api.get('/api/friend/apply/status')
    const m = {}; (data?.items || []).forEach(it => m[String(it.gid)] = it)
    let active = false
    addItems.value.forEach(i => {
      const st = m[String(i.uid)]
      if (st) { i.status = st.status; if (st.error) i.err = st.error }
      if (i.status === 'pending' || i.status === 'sending') active = true
    })
    if (!active) stopApplyPoll()
  } catch (e) {}
}
/* 展示辅助 */
function maskKey(k) { return !k ? '' : (k.length > 12 ? k.slice(0, 8) + '…' + k.slice(-4) : k) }
function badgeText(s) { return { pending: '待发送', sending: '发送中', sent: '已发送', failed: '失败', cancelled: '已取消' }[s] || s }
function badgeCls(s) { return { pending: 'b-pending', sending: 'b-sending', sent: 'b-sent', failed: 'b-failed', cancelled: 'b-cancelled' }[s] || 'b-pending' }
/* 黑名单 */
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
/* 访客 */
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
/* 批量删除（登录密码验证，入口在好友列表内联面板） */
const delOpen = ref(false); const delLevel = ref(30); const delSkipGuard = ref(true); const delPwd = ref('')
const delMatchCount = computed(() => friends.value.filter(f => Number(f.level) <= (Number(delLevel.value) || 0) && (!delSkipGuard.value || Number(f.dogId) !== 90021)).length)
async function delBatch() {
  if (!acc()) return
  if (!delPwd.value) { app.error('请输入登录密码'); return }
  const lvl = Number(delLevel.value) || 0, skipGuard = delSkipGuard.value, pwd = delPwd.value
  const gids = []
  if (lvl > 0) friends.value.forEach(f => { if (Number(f.level) > lvl) return; if (skipGuard && isGuard(f)) return; if (gids.indexOf(Number(f.uid ?? f.gid)) === -1) gids.push(Number(f.uid ?? f.gid)) })
  if (!gids.length) { app.error('请填写等级阈值'); return }
  if (!confirm('确认批量删除 ' + gids.length + ' 名好友？此操作不可恢复')) return
  const { data } = await api.post('/api/friend/batch-delete', { gids, password: pwd }).catch(e => e.response || { data: {} })
  if (data?.ok) { app.success('成功 ' + (data.successCount || 0) + ' / 失败 ' + (data.failedCount || 0)); loadFriends(); delOpen.value = false }
  else if (data?.error === '登录密码错误') app.error('登录密码错误')
  else app.error('失败：' + (data?.error || '未知'))
}

/* ---------------- 每日任务 ---------------- */
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

/* ---------------- 每日礼包 ---------------- */
const giftState = ref({}); const giftBusy = ref(false); const giftMsg = ref('')
const giftItems = [
  { key: 'email',     icon: '📬', label: '邮箱奖励',    desc: '邮件中的奖励附件' },
  { key: 'share',     icon: '🔗', label: '每日分享',    desc: '分享礼包' },
  { key: 'monthcard', icon: '🗓️', label: '月卡礼包',    desc: '月卡每日领取' },
  { key: 'mall',      icon: '🎁', label: '商城免费礼',  desc: '每日免费商品' },
  { key: 'vip',       icon: '👑', label: 'QQ会员礼包', desc: '会员每日档位' },
]
async function loadGifts() {
  if (!acc()) return
  try { const { data } = await api.get('/api/task/daily-gifts'); giftState.value = (data && data.state) || {} } catch (e) { /* 静默 */ }
}
async function claimGifts() {
  if (!acc()) return
  if (giftBusy.value) return
  giftBusy.value = true; giftMsg.value = '领取中…'
  try {
    const { data } = await api.post('/api/task/daily-gifts', { type: 'all' }).catch(e => e.response || { data: {} })
    const r = (data && data.result) || {}
    const parts = []
    if (r.email > 0) parts.push('邮件 ' + r.email + ' 封')
    if (r.share > 0) parts.push('分享 x' + r.share)
    if (r.monthcard > 0) parts.push('月卡 x' + r.monthcard)
    if (r.mall > 0) parts.push('免费礼 x' + r.mall)
    if (r.vip > 0) parts.push('会员 x' + r.vip)
    giftMsg.value = parts.length ? '已领取：' + parts.join('、') : '今日无可领取项'
    await loadGifts()
  } catch (e) { giftMsg.value = '领取失败' }
  giftBusy.value = false
}

/* 好友检测（疑似外挂，基于访客行为统计；后端 /api/friends/bot-scan） */
const botScan = ref([]); const botLoading = ref(false); const botHint = ref('')
const botExpanded = ref({}); const botDelRange = ref('high'); const excludeGuardDog = ref(true); const botDelPwd = ref('')
const botStats = computed(() => ({ high: botScan.value.filter(b => b.risk === 'high').length, mid: botScan.value.filter(b => b.risk === 'medium').length, low: botScan.value.filter(b => b.risk === 'low').length }))
function botTargets() {
  return botScan.value.filter(b => {
    if (botDelRange.value === 'high' ? b.risk !== 'high' : !['high', 'medium'].includes(b.risk)) return false
    if (excludeGuardDog.value && b.isGuardDog) return false
    return true
  })
}
const botDelCount = computed(() => botTargets().length)
async function loadBotScan() {
  if (!acc()) return
  botLoading.value = true
  try {
    const { data } = await api.get('/api/friends/bot-scan')
    botScan.value = Array.isArray(data?.data) ? data.data : []
    botHint.value = data?.errorHint || ''
  } catch (e) { botScan.value = []; botHint.value = '加载失败：' + (e?.response?.data?.error || e?.message || e) }
  finally { botLoading.value = false }
}
function botRisk(r, s) { return { high: ['🔴 高嫌疑 ' + s, '#e5484d'], medium: ['🟠 中嫌疑 ' + s, '#f59e0b'], low: ['🟡 低嫌疑 ' + s, '#f0c000'], clean: ['🟢 正常 ' + s, '#30a46c'] }[r] || ['⚪ ' + s, '#9ca3af'] }
function valColor(v) { return v >= 45 ? '#16a34a' : v >= 25 ? '#f59e0b' : '#9ca3af' }
// 好友检测筛选/排序（6 维度：全部/帮价值/偷价值/嫌疑分/低帮/清洗）
const botSort = ref('all')
const botSorts = [['all', '全部'], ['helpValue', '帮价值高'], ['stealValue', '偷价值高'], ['risk', '🔴 嫌疑高'], ['lowHelp', '低帮价值'], ['junk', '建议清洗']]
const filteredBotScan = computed(() => {
  const list = [...botScan.value]
  switch (botSort.value) {
    case 'helpValue': list.sort((a, b) => (b.value || 0) - (a.value || 0)); break
    case 'stealValue': list.sort((a, b) => (b.stealValue || 0) - (a.stealValue || 0)); break
    case 'risk': list.sort((a, b) => (b.score || 0) - (a.score || 0)); break
    case 'lowHelp': return list.filter(b => b.valueLevel === 'low')
    case 'junk': return list.filter(b => b.valueLevel === 'junk' || (b.value || 0) < 25)
  }
  return list
})
function toggleBotDetail(gid) { botExpanded.value[gid] = !botExpanded.value[gid] }
async function botDeleteBatch() {
  if (!acc()) return
  if (!botDelPwd.value) { app.error('请输入登录密码'); return }
  const gids = botTargets().map(b => Number(b.gid))
  if (!gids.length) { app.error('没有可删除的嫌疑好友'); return }
  if (!confirm('确认删除 ' + gids.length + ' 名疑似外挂好友？此操作不可恢复')) return
  const { data } = await api.post('/api/friend/batch-delete', { gids, password: botDelPwd.value }).catch(e => e.response || { data: {} })
  if (data?.ok) { app.success('成功 ' + (data.successCount || 0) + ' / 失败 ' + (data.failedCount || 0)); loadBotScan(); loadFriends() }
  else if (data?.error === '登录密码错误') app.error('登录密码错误')
  else app.error('失败：' + (data?.error || '未知'))
}

/* ---------------- 护主犬 ---------------- */
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
  else if (c === 'p-daily') { await loadTasks(); await loadGifts() }
  else if (c === 'p-dog') await loadDog()
}
async function onFsub(s) {
  fsub.value = s
  stopApplyPoll()
  if (s === 'list') await loadFriends()
  else if (s === 'blacklist') await loadBlacklist()
  else if (s === 'visitors') await loadVisitors()
  else if (s === 'bot') await loadBotScan()
  else if (s === 'add') startApplyPoll()
}

// 切号事件：按当前 tab 用新账号重拉数据（热切换）
const onSwitched = () => { if (!acc()) return; onTab(tab.value) }
onMounted(() => { if (!acc()) return; loadLands(); landTimer = setInterval(landCountdown, 1000); window.addEventListener('account-switched', onSwitched) })
onUnmounted(() => { clearInterval(landTimer); window.removeEventListener('account-switched', onSwitched); stopApplyPoll() })
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
          <span v-if="Number(l.purpleCrystalResonanceExpBonus) > 0" class="lc-mutants" style="display:inline-flex;align-items:center;gap:3px;font-size:10px;color:#c39bf3;margin-left:4px" title="紫金土地+变异 经验加成"><img :src="'/game-config/seed_images_named/mutant/crystal.png'" style="width:16px;height:16px;object-fit:contain" alt="紫晶共鸣">紫晶共鸣 +{{ Number(l.purpleCrystalResonanceExpBonus) / 100 }}%</span>
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
      <div class="seg seg-6" style="margin-bottom:10px">
        <button class="seg-btn" :class="{ active: fsub === 'list' }" @click="onFsub('list')">👥 好友列表</button>
        <button class="seg-btn" :class="{ active: fsub === 'add' }" @click="fsub='add'">➕ 加好友</button>
        <button class="seg-btn" :class="{ active: fsub === 'blacklist' }" @click="onFsub('blacklist')">🚫 黑名单<span v-if="blk > 0" class="nb">{{ blk }}</span></button>
        <button class="seg-btn" :class="{ active: fsub === 'visitors' }" @click="onFsub('visitors')">👁 访客</button>
        <button class="seg-btn" :class="{ active: fsub === 'bot' }" @click="onFsub('bot')">🔍 好友检测</button>
      </div>

      <div v-if="fsub === 'list'">
        <input class="field" v-model="friendSearch" placeholder="🔍 搜索好友…" style="margin-top:16px">
        <p style="font-size:11px;color:var(--muted);margin:6px 2px 10px" id="frTotal">共 {{ shownFriends.length }} 名好友</p>
        <div class="friend-filter" style="display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px">
          <button class="chip" :class="{ on: friendDogFilter === 'all' }" @click="friendDogFilter='all'">全部</button>
          <button class="chip" :class="{ on: friendDogFilter === 'noGuardDog' }" @click="friendDogFilter='noGuardDog'">无护主犬</button>
          <button class="chip" :class="{ on: friendDogFilter === 'guardDog' }" @click="friendDogFilter='guardDog'">有护主犬</button>
          <button class="chip" style="border-color:var(--danger,#e54);color:var(--danger,#e54)" @click="delOpen = !delOpen">🗑 批量删除</button>
          <button class="chip" style="margin-left:auto" @click="knownOpen = !knownOpen; if (knownOpen) loadKnownGids()">📇 抓取GID</button>
          <button class="chip" @click="fetchDogInfo">🐶 获取狗信息</button>
          <button class="chip" @click="loadFriends">🔄 刷新</button>
        </div>
        <div v-if="knownOpen" style="margin-bottom:10px;display:flex;gap:6px;flex-wrap:wrap;align-items:center">
          <input class="field" v-model="knownGidInput" placeholder="好友 GID（多个用逗号分隔）" style="flex:1;min-width:200px">
          <button class="chip" style="border-color:var(--primary);color:var(--primary)" @click="addKnownGids">添加抓取</button>
          <span v-if="knownGids.length" style="font-size:12px;color:var(--muted)">已添加：
            <span v-for="g in knownGids" :key="g" style="display:inline-block;background:var(--bg);border:1px solid var(--border);border-radius:10px;padding:1px 8px;margin:2px">
              {{ g }} <a @click="removeKnownGid(g)" style="color:var(--danger,#e54);cursor:pointer">✕</a>
            </span>
          </span>
        </div>
        <div v-if="delOpen" class="del-panel" style="border:1px solid var(--border);border-radius:12px;padding:12px;margin-bottom:10px;background:var(--card)">
          <div style="font-size:12.5px;font-weight:700;margin-bottom:8px">批量删除（需登录密码）</div>
          <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center">
            <label style="font-size:12.5px">等级 ≤ <input v-model.number="delLevel" class="field" type="number" placeholder="如30" style="width:90px;display:inline-block"></label>
            <label style="font-size:12.5px"><input type="checkbox" v-model="delSkipGuard" checked> 保留护主犬</label>
          </div>
          <div style="display:flex;gap:8px;align-items:center;margin-top:8px">
            <input v-model="delPwd" class="field" type="password" placeholder="登录密码（必填）" style="flex:1;min-width:140px">
            <button class="f-batch" @click="delBatch">删除 {{ delMatchCount }} 名</button>
          </div>
          <p style="font-size:11px;color:var(--muted);margin-top:6px">匹配 {{ delMatchCount }} 名（等级≤阈值且非护主犬）</p>
        </div>
        <div>
          <div v-for="f in shownFriends" :key="String(f.uid ?? f.gid)" class="friend-card" :data-uid="String(f.uid ?? f.gid)">
            <div class="fc-head" @click="toggleFriendLands(f.uid ?? f.gid)">
              <div class="f-av"><img v-if="/^(https?:)?\/\//i.test(f.avatar || '')" :src="f.avatar" alt=""><span v-else>{{ f.avatar || '👤' }}</span></div>
              <div class="f-info">
                <h4>{{ f.name || '' }} <small class="fc-id">({{ f.uid ?? f.gid }})</small><span v-if="isGuard(f)" class="ripeness fc-dog">护主犬</span></h4>
                <p>Lv.{{ f.level || '-' }}<span v-if="f.coins != null"> · 金币 {{ fmtBig(f.coins) }}</span><span v-if="f.ripeLands"> · 可收 {{ f.ripeLands }} 块</span></p>
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
        <p class="add-hint">粘贴分享链接（每行一条，支持批量）或导入文件，自动解析 UID + OPENID + SHAREKEY，勾选后发送好友申请。</p>
        <div class="add-input">
          <textarea v-model="addText" @input="onAddInput" class="field" rows="3" placeholder="在此粘贴分享链接，每行一条，自动解析…"></textarea>
          <div class="add-input-row">
            <label class="chip add-file">📁 导入文件<input type="file" id="addFile" accept=".txt,.csv,.json,text/*" hidden @change="onAddFile"></label>
            <span class="add-phint" v-if="addItems.length">已解析 {{ addItems.length }} 条</span>
          </div>
        </div>

        <div class="add-tool">
          <button class="chip" :class="{ on: allSel }" @click="toggleAll">☑ 全选</button>
          <button class="chip add-del" @click="delSel">🗑 删除</button>
          <button class="chip add-cancel" @click="cancelSel">✕ 取消发送</button>
          <button class="f-batch" @click="sendSel">📤 发送</button>
        </div>

        <div class="add-head" v-if="addItems.length">
          <span>解析结果</span>
          <span class="add-cnt">共 {{ addItems.length }} 条 · 已选 {{ selCount }}</span>
        </div>

        <div v-for="it in addItems" :key="it.uid" class="add-card" :class="{ sent: it.status === 'sent', failed: it.status === 'failed', cancelled: it.status === 'cancelled' }">
          <input type="checkbox" class="ck" v-model="it.sel">
          <div class="ac-body">
            <div class="ac-uid">UID {{ it.uid }}</div>
            <div class="ac-kv">OPENID {{ maskKey(it.openid) }}</div>
            <div class="ac-kv">SHAREKEY {{ maskKey(it.shareKey) }}</div>
          </div>
          <span class="badge" :class="badgeCls(it.status)">{{ badgeText(it.status) }}</span>
          <button class="del-x" @click="delOne(it.uid)">✕</button>
        </div>
        <p v-if="!addItems.length" class="add-empty">暂无解析数据，粘贴链接或导入文件试试</p>
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

      <div v-if="fsub === 'bot'">
        <div class="visitor-stats" style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin:8px 0">
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block;color:var(--danger)">{{ botStats.high }}</b><span style="font-size:11px;color:var(--muted)">高嫌疑</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block;color:var(--warn)">{{ botStats.mid }}</b><span style="font-size:11px;color:var(--muted)">中嫌疑</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block;color:var(--warn)">{{ botStats.low }}</b><span style="font-size:11px;color:var(--muted)">低嫌疑</span></div>
          <div class="vstat" style="background:var(--card);border-radius:12px;padding:10px;text-align:center"><b style="font-size:18px;display:block">{{ botScan.length }}</b><span style="font-size:11px;color:var(--muted)">已检测</span></div>
        </div>
        <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin:8px 0">
          <select v-model="botDelRange" class="field" style="width:auto;flex:none;padding:6px 8px">
            <option value="high">仅高嫌疑</option>
            <option value="midHigh">中+高嫌疑</option>
          </select>
          <label style="font-size:12.5px;display:flex;align-items:center;gap:4px"><input type="checkbox" v-model="excludeGuardDog" checked> 🐕 排除护主犬</label>
          <input v-model="botDelPwd" class="field" type="password" placeholder="登录密码（必填）" style="width:130px;padding:6px 8px">
          <button class="f-batch" @click="botDeleteBatch">🗑 一键删除 ({{ botDelCount }})</button>
          <button class="chip" style="margin-left:auto" @click="loadBotScan">{{ botLoading ? '扫描中…' : '🔄 重扫' }}</button>
        </div>
        <div style="display:flex;gap:4px;flex-wrap:wrap;margin:0 0 8px">
          <button v-for="k in botSorts" :key="k[0]" class="chip" :class="{ on: botSort === k[0] }" @click="botSort = k[0]">{{ k[1] }}</button>
        </div>
        <p v-if="botHint" style="font-size:12px;color:var(--muted);text-align:center;margin:6px 0">{{ botHint }}</p>
        <p v-else-if="!botLoading && !botScan.length" style="text-align:center;color:var(--muted);padding:20px 0">暂无检测结果</p>
        <div v-for="b in filteredBotScan" :key="String(b.gid)" class="bot-card">
          <div class="bc-row1">
            <div class="bc-av">
              <img v-if="/^(https?:)?\/\//i.test(b.avatar || '')" :src="b.avatar" alt="" @error="$event.target.classList.add('hide')" />
              <span class="bc-fall">👤</span>
            </div>
            <div class="bc-name">{{ b.nick || ('GID:' + b.gid) }}</div>
            <span v-if="b.isGuardDog" class="bc-guard">🐕护主犬</span>
            <span class="bc-vbadge" style="background:#16a34a">帮 {{ b.value }}</span>
            <span class="bc-vbadge" :style="{ background: b.stealValue > 0 ? '#059669' : '#9ca3af' }">偷 {{ b.stealValue }}</span>
          </div>
          <div class="bc-gid">GID {{ b.gid }}</div>
          <div class="bc-row2">
            <span class="bc-risk" :style="{ background: botRisk(b.risk, b.score)[1] }">{{ botRisk(b.risk, b.score)[0] }}</span>
            <span v-for="r in (b.reasons || [])" :key="r" class="bc-tag">{{ r }}</span>
          </div>
          <div v-if="botExpanded[b.gid]" class="bc-expand">
            样本 {{ b.recordCount }} 条 · 窗口 {{ b.sampleWindow }}h<br>
            间隔均值 {{ b.signals.intervalMeanSec }}s ± {{ b.signals.intervalStdMs }}ms · 活跃 {{ b.signals.activeHours }}h · 凌晨 {{ b.signals.nightRatio }}%<br>
            偷{{ b.signals.steal }} / 帮{{ b.signals.help }} / 捣{{ b.signals.bad }} · 日均 {{ b.signals.avgPerDay }} 次<br>
            帮价值 {{ b.value }}/100 = 护主犬{{ b.valueDetail.guardDog ? '+38' : '+0' }} + 帮{{ b.valueDetail.help }}×3(封顶32) + 活跃{{ b.valueDetail.activeHours }}×2(封顶20) − 偷{{ b.valueDetail.steal }}×2 − 捣{{ b.valueDetail.bad }}×5 − 风险{{ b.valueDetail.riskScore }}×0.5<br>
            偷价值 {{ b.stealValue }}/100 = 我偷TA{{ b.valueDetail.stealTo }}×4(封顶40) + 实时可偷数×10（偷菜为单向行为，不受嫌疑分影响）
          </div>
          <div class="bc-ops">
            <button class="fa-mini" @click="friendOp(b.gid, 'black')">🚫 拉黑</button>
            <button class="fa-mini" style="color:var(--danger)" @click="friendOp(b.gid, 'del')">🗑️ 删除</button>
            <button class="fa-mini" style="margin-left:auto" @click="toggleBotDetail(b.gid)">{{ botExpanded[b.gid] ? '收起 ▴' : '展开明细 ▾' }}</button>
          </div>
        </div>
        <p style="font-size:11px;color:var(--muted);text-align:center;margin-top:10px">* 基于访客行为统计，仅供参考，可能误报；删除不可恢复</p>
      </div>

      <!-- 批量删除入口已并入好友列表（delOpen 面板） -->
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
      <div style="border-radius:14px;background:var(--card-strong);padding:12px;margin-top:10px">
        <div style="display:flex;align-items:center;gap:8px;margin:2px 0 8px">
          <span class="f-label" style="margin:0">🎁 每日礼包</span>
          <span style="flex:1"></span>
          <button class="chip" :disabled="giftBusy" @click="claimGifts">{{ giftBusy ? '领取中…' : '一键领取' }}</button>
        </div>
        <div style="display:grid;grid-template-columns:1fr 1fr;gap:8px">
          <div v-for="g in giftItems" :key="g.key" style="border-radius:12px;background:var(--bg,rgba(128,128,128,.07));padding:10px">
            <div style="display:flex;align-items:center;gap:6px;font-size:12.5px">
              <span>{{ g.icon }}</span><span style="font-weight:700">{{ g.label }}</span>
              <span style="flex:1"></span>
              <span v-if="giftState[g.key]" style="font-size:10.5px;color:var(--primary)">✓ 已处理</span>
              <span v-else style="font-size:10.5px;color:var(--muted)">待领取</span>
            </div>
            <div style="font-size:10.5px;color:var(--muted);margin-top:3px">{{ g.desc }}</div>
          </div>
        </div>
        <p v-if="giftMsg" style="font-size:11px;color:var(--primary);margin-top:8px;min-height:14px">{{ giftMsg }}</p>
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
.seg-6 .seg-btn { font-size: 11px; padding: 8px 2px; white-space: nowrap; }
/* 好友检测卡片（紧凑排版） */
.bot-card { background: var(--card-strong); border: 1px solid var(--border); border-radius: var(--radius-md); padding: 10px 12px; margin-bottom: 8px; }
.bc-row1 { display: flex; align-items: center; gap: 10px; cursor: pointer; }
.bc-av { width: 40px; height: 40px; border-radius: 50%; overflow: hidden; position: relative; background: var(--primary-soft); display: grid; place-items: center; flex: none; }
.bc-av img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; }
.bc-av img.hide { display: none; }
.bc-fall { font-size: 20px; color: var(--muted); }
.bc-name { font-size: 14px; font-weight: 700; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.bc-risk { font-size: 10px; padding: 2px 6px; border-radius: 6px; color: #fff; font-weight: 600; flex: none; }
.bc-guard { font-size: 10px; color: #b45309; background: #fef3c7; border-radius: 6px; padding: 2px 5px; flex: none; white-space: nowrap; }
.bc-val { font-size: 10px; padding: 2px 6px; border-radius: 6px; font-weight: 600; flex: none; white-space: nowrap; border: 1px solid currentColor; }
.bc-vbadge { font-size: 11px; padding: 3px 7px; border-radius: 6px; color: #fff; font-weight: 700; flex: none; white-space: nowrap; }
.bc-gid { font-size: 10px; color: var(--muted); margin-top: 4px; }
.bc-tags { display: flex; gap: 4px; flex-wrap: wrap; margin: 8px 0 6px; }
.bc-tag { font-size: 10px; padding: 2px 7px; border-radius: 999px; background: color-mix(in oklch, var(--danger) 8%, transparent); color: var(--danger); border: 1px solid color-mix(in oklch, var(--danger) 25%, transparent); }
.bc-row2 { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-top: 6px; }
.bc-track { flex: 1; height: 5px; border-radius: 999px; background: var(--border); overflow: hidden; }
.bc-fill { height: 100%; border-radius: 999px; }
.bc-score { font-size: 11px; font-weight: 600; flex: none; }
.bc-expand { font-size: 11px; color: var(--muted); line-height: 1.8; padding: 8px 0 0; border-top: 1px dashed var(--border); margin-top: 8px; }
.bc-ops { display: flex; gap: 6px; margin-top: 8px; }
.bc-ops .fa-mini { padding: 4px 10px; }
/* ── 加好友：批量解析 + 串行队列 ── */
.add-hint { font-size: 11.5px; color: var(--muted); margin: 2px 4px 10px; line-height: 1.5; }
.add-input { background: var(--card-strong); border: 1px solid var(--border); border-radius: var(--radius-md); padding: 12px; box-shadow: var(--shadow-sm); }
.add-input textarea { min-height: 64px; resize: vertical; }
.add-input-row { display: flex; align-items: center; gap: 10px; margin-top: 10px; }
.add-file { display: inline-flex; align-items: center; gap: 6px; color: var(--primary); border-color: var(--primary); background: var(--primary-soft); }
.add-phint { margin-left: auto; font-size: 11px; color: var(--good); }
.add-tool { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 14px 0 6px; }
.add-tool .f-batch { margin-left: auto; }
.add-del { color: var(--danger); border-color: color-mix(in oklch, var(--danger) 35%, transparent); background: color-mix(in oklch, var(--danger) 8%, transparent); }
.add-cancel { color: var(--warn); border-color: color-mix(in oklch, var(--warn) 35%, transparent); background: color-mix(in oklch, var(--warn) 10%, transparent); }
.add-head { display: flex; align-items: center; margin: 10px 4px 8px; font-size: 11.5px; color: var(--muted); }
.add-cnt { margin-left: auto; }
.add-card { display: flex; align-items: flex-start; gap: 10px; background: var(--card-strong); border: 1px solid var(--border); border-radius: 14px; padding: 11px 12px; margin-bottom: 9px; box-shadow: var(--shadow-sm); position: relative; }
.add-card.sent { border-color: color-mix(in oklch, var(--good) 50%, transparent); }
.add-card.failed { border-color: color-mix(in oklch, var(--danger) 50%, transparent); }
.add-card.cancelled { opacity: .6; border-color: color-mix(in oklch, var(--muted) 45%, transparent); }
.b-cancelled { color: var(--muted); background: color-mix(in oklch, var(--muted) 16%, transparent); border: 1px solid color-mix(in oklch, var(--muted) 35%, transparent); }
.ck { appearance: none; width: 18px; height: 18px; border: 2px solid var(--border); border-radius: 6px; margin-top: 2px; cursor: pointer; flex: none; position: relative; background: oklch(1 0 0 / .5); }
.ck:checked { background: var(--primary); border-color: var(--primary); }
.ck:checked::after { content: "✓"; color: #fff; font-size: 12px; position: absolute; inset: 0; display: flex; align-items: center; justify-content: center; }
.ac-body { flex: 1; min-width: 0; }
.ac-uid { font-size: 13.5px; font-weight: 700; }
.ac-kv { font-size: 11px; color: var(--muted); margin-top: 3px; font-family: ui-monospace, "SF Mono", Menlo, monospace; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.badge { font-size: 10px; font-weight: 700; padding: 2px 8px; border-radius: 20px; flex: none; margin-top: 2px; }
.b-pending { color: var(--muted); background: color-mix(in oklch, var(--muted) 14%, transparent); }
.b-sending { color: var(--primary); background: var(--primary-soft); }
.b-sent { color: var(--good); background: color-mix(in oklch, var(--good) 14%, transparent); }
.b-failed { color: var(--danger); background: color-mix(in oklch, var(--danger) 12%, transparent); }
.del-x { position: absolute; top: 8px; right: 8px; width: 22px; height: 22px; border-radius: 50%; border: 1px solid var(--border); background: var(--card); color: var(--danger); font-size: 13px; cursor: pointer; display: flex; align-items: center; justify-content: center; }
.add-empty { text-align: center; color: var(--muted); font-size: 12px; padding: 22px 0; }
</style>
