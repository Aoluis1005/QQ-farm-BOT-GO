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

const curPanel = computed(() => panels.value[panelIdx.value] || null)

function n(v) { return v == null ? 0 : (Number(v) || 0) }

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

onMounted(loadActivity)
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
          <div class="act-stats"><span>💰 {{ shopState.cur }}余额 <b>{{ shopState.bal }}</b></span></div>
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

    <div v-else-if="curPanel" class="act-empty">该活动暂无可展示的面板</div>
  </div>
</template>
