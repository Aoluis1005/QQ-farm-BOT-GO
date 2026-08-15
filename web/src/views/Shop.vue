<script setup>
import { ref, reactive, onMounted } from 'vue'
import api from '@/api'
import { getAccountId } from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const tab = ref('s-seed')
const acc = () => getAccountId()

const seed = reactive({ list: [], qty: {} })
const pet = reactive({ list: [] })
const decor = reactive({ list: [] })
const mall = reactive({ list: [], qty: {} })
const mystery = ref(null)
const mysteryAb = ref(false)
const mysteryCur = ref([])
const prof = ref({})

const CUR = { 1001: '金币', 1002: '点券', 1005: '金豆豆' }
const isFert = it => it.goodsId === 1002 || it.goodsId === 1003

function imgOf(it) {
  if (it.images && it.images.length) return it.images[0]
  if (it.image || it.itemImage) return it.image || it.itemImage
  return ''
}
function fmtNum(n) { return n === undefined || n === null ? 0 : Number(String(n).replace(/,/g, '')) }

async function loadSeed() {
  if (!acc()) { seed.list = []; return }
  try { const { data } = await api.get('/api/shop/seed'); seed.list = data?.data || [] } catch (e) { seed.list = [] }
  seed.list.forEach(i => { if (seed.qty[i.id] === undefined) seed.qty[i.id] = 1 })
}
async function loadPet() { if (!acc()) { pet.list = []; return } try { const { data } = await api.get('/api/shop/pet'); pet.list = data?.data || [] } catch (e) { pet.list = [] } }
async function loadDecor() { if (!acc()) { decor.list = []; return } try { const { data } = await api.get('/api/shop/decoration'); decor.list = data?.data || [] } catch (e) { decor.list = [] } }
async function loadMall() {
  if (!acc()) { mall.list = []; return }
  try { const { data } = await api.get('/api/shop/mall'); mall.list = data?.data || [] } catch (e) { mall.list = [] }
  mall.list.forEach(i => { if (mall.qty[i.id] === undefined) mall.qty[i.id] = 1 })
}
async function loadMystery() {
  mystery.value = null
  if (!acc()) return
  try {
    const { data } = await api.get('/api/settings')
    const d = data?.data || {}
    mysteryAb.value = !!(d.automation && d.automation.mystery_auto_buy)
    mysteryCur.value = (d.mysteryAutoBuyCurrencies || []).map(String)
  } catch (e) {}
  try {
    const { data } = await api.get('/api/shop/mystery')
    if (data?.ok) mystery.value = data.data || {}
  } catch (e) {}
}
async function shopProfile() { if (!acc()) return; try { const { data } = await api.get('/api/home/profile'); prof.value = data?.data || {} } catch (e) {} }

async function onTab(t) {
  tab.value = t
  if (t === 's-seed') await loadSeed()
  else if (t === 's-pet') await loadPet()
  else if (t === 's-decor') await loadDecor()
  else if (t === 's-prop') await loadMall()
  else if (t === 's-mystery') await loadMystery()
  await shopProfile()
}

/* 普通商品购买：POST /api/shop/buy {goodsId,num,price} */
async function buy(kind, gid, price, q, name) {
  if (!acc()) { app.error('请先选择账号'); return }
  try {
    const body = kind === 'mall' ? { goodsId: Number(gid), count: Number(q) } : { goodsId: Number(gid), num: Number(q) || 1, price: Number(price) || 0 }
    const url = kind === 'mall' ? '/api/shop/mall/buy' : '/api/shop/buy'
    const { data } = await api.post(url, body)
    if (data?.ok) {
      app.success((name || '商品') + ' ×' + q + (kind === 'mall' && Number(price) === 0 ? ' 领取成功' : ' 购买成功'))
      onTab(tab.value)
    } else app.error(data?.error || '操作失败')
  } catch (e) { app.error('操作失败') }
}
async function mysteryBuy(npcId) {
  const { data } = await api.post('/api/shop/mystery/buy', { npcId: Number(npcId) }).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '购买成功' : (data?.error || '操作失败'))
  loadMystery()
}
async function mysteryAbandon() {
  if (!confirm('确定请离神秘商人？')) return
  const { data } = await api.post('/api/shop/mystery/abandon', {}).catch(e => e.response || { data: {} })
  app.success(data?.ok ? '已请离神秘商人' : (data?.error || '操作失败'))
  loadMystery()
}
async function toggleMysteryAb() {
  mysteryAb.value = !mysteryAb.value
  try {
    const { data } = await api.get('/api/settings')
    const aut = (data?.data && data.data.automation) || {}
    aut.mystery_auto_buy = mysteryAb.value
    const r = await api.post('/api/automation', aut)
    app.success(r?.data?.ok ? ('神秘商人自动购买已' + (mysteryAb.value ? '开启' : '关闭')) : (r?.data?.error || '保存失败'))
  } catch (e) { mysteryAb.value = !mysteryAb.value; app.error('保存失败') }
}
async function toggleMysteryCur(c) {
  const has = mysteryCur.value.includes(String(c))
  mysteryCur.value = has ? mysteryCur.value.filter(x => x !== String(c)) : [...mysteryCur.value, String(c)]
  try {
    const { data } = await api.post('/api/settings/save', { mysteryAutoBuyCurrencies: mysteryCur.value.map(Number) })
    if (!data?.ok) app.error(data?.error || '保存失败')
  } catch (e) { app.error('保存失败') }
}

function fmtTime(unix) {
  if (!unix) return ''
  const dt = new Date(unix * 1000)
  return ('0' + (dt.getMonth() + 1)).slice(-2) + '/' + ('0' + dt.getDate()).slice(-2) + ' ' + ('0' + dt.getHours()).slice(-2) + ':' + ('0' + dt.getMinutes()).slice(-2)
}

onMounted(() => { loadSeed(); shopProfile() })
</script>

<template>
  <div>
    <div class="seg seg-5">
      <button class="seg-btn" :class="{ active: tab === 's-seed' }" @click="onTab('s-seed')">🌱 种子</button>
      <button class="seg-btn" :class="{ active: tab === 's-pet' }" @click="onTab('s-pet')">🐶 宠物</button>
      <button class="seg-btn" :class="{ active: tab === 's-decor' }" @click="onTab('s-decor')">🎨 装扮</button>
      <button class="seg-btn" :class="{ active: tab === 's-prop' }" @click="onTab('s-prop')">🎒 道具</button>
      <button class="seg-btn" :class="{ active: tab === 's-mystery' }" @click="onTab('s-mystery')">🧙 神秘商人</button>
    </div>

    <!-- 商城资产条 -->
    <div v-show="acc()" style="display:flex;gap:14px;flex-wrap:wrap;padding:10px 14px;margin:10px 0;border-radius:14px;background:var(--card-strong);font-size:12.5px">
      <span>Lv.<b style="color:var(--primary)">{{ fmtNum(prof.level) }}</b></span>
      <span>金币 <b style="color:var(--primary)">{{ fmtNum(prof.gold) }}</b></span>
      <span>点券 <b style="color:var(--primary)">{{ fmtNum(prof.coupons) }}</b></span>
      <span>金豆豆 <b style="color:var(--primary)">{{ fmtNum(prof.goldenBeans) }}</b></span>
    </div>

    <!-- 种子 -->
    <div v-show="tab === 's-seed'">
      <div class="act-grid">
        <div v-for="it in seed.list" :key="it.id" class="act-item">
          <div class="lc-img"><img v-if="imgOf(it)" :src="imgOf(it)" style="width:44px;height:44px;object-fit:contain" loading="lazy"><span v-else style="font-size:22px">🌱</span></div>
          <div class="nm">{{ it.name }}</div>
          <div class="ct">💰 {{ it.price }}</div>
          <div class="act-limit">Lv.{{ fmtNum(it.requiredLevel) }} · 每季×{{ fmtNum(it.itemCount) }} · {{ fmtNum(it.expPerSeason) }}经验<span v-if="it.limitCount>0"> · 限{{ fmtNum(it.limitCount) }}</span></div>
          <template v-if="!it.isSoldOut && it.unlocked && it.canBuy">
            <div class="act-qty">
              <button class="act-btn act-sm" @click="seed.qty[it.id] = Math.max(1, (seed.qty[it.id]||1) - 1)">−</button>
              <input class="act-num" :value="seed.qty[it.id]||1" min="1" max="99" inputmode="numeric" @input="seed.qty[it.id] = Math.max(1, Math.min(99, Number($event.target.value)||1))">
              <button class="act-btn act-sm" @click="seed.qty[it.id] = Math.min(99, (seed.qty[it.id]||1) + 1)">＋</button>
            </div>
            <button class="act-btn act-sm" @click="buy('seed', it.id, it.price, seed.qty[it.id]||1, it.name)">💎 购买</button>
          </template>
          <button v-else class="act-btn act-sm disabled" disabled>{{ it.isSoldOut ? '已售罄' : (it.unlocked ? '需Lv.' + fmtNum(it.requiredLevel) : '未解锁') }}</button>
        </div>
        <p v-if="!seed.list.length" style="grid-column:1/-1;text-align:center;color:var(--muted);padding:24px 0">{{ acc() ? '暂无可售种子' : '请先选择账号' }}</p>
      </div>
    </div>

    <!-- 宠物 -->
    <div v-show="tab === 's-pet'">
      <div class="act-grid">
        <div v-for="it in pet.list" :key="it.id" class="act-item">
          <div class="lc-img"><img v-if="imgOf(it)" :src="imgOf(it)" style="width:44px;height:44px;object-fit:contain" loading="lazy"><span v-else style="font-size:22px">🐶</span></div>
          <div class="nm">{{ it.name }}</div>
          <div class="ct">{{ it.isGoldenBean ? '🫘 金豆豆' : '💰 金币' }} {{ it.price }}</div>
          <div class="act-limit">Lv.{{ fmtNum(it.requiredLevel) }}<span v-if="it.desc"> · {{ it.desc }}</span></div>
          <button v-if="!it.isSoldOut && it.unlocked && it.canBuy" class="act-btn act-sm" @click="buy('pet', it.id, it.price, 1, it.name)">💎 购买</button>
          <button v-else class="act-btn act-sm disabled" disabled>{{ it.isSoldOut ? '已售罄' : (it.unlocked ? (it.isGoldenBean ? '金豆豆不足' : '金币不足') : '未解锁') }}</button>
        </div>
        <p v-if="!pet.list.length" style="grid-column:1/-1;text-align:center;color:var(--muted);padding:24px 0">{{ acc() ? '暂无可售宠物' : '请先选择账号' }}</p>
      </div>
    </div>

    <!-- 装扮 -->
    <div v-show="tab === 's-decor'">
      <div class="act-grid">
        <div v-for="it in decor.list" :key="it.id" class="act-item">
          <div class="lc-img"><img v-if="imgOf(it)" :src="imgOf(it)" style="width:44px;height:44px;object-fit:contain" loading="lazy"><span v-else style="font-size:22px">🎨</span></div>
          <div class="nm">{{ it.name }}</div>
          <div class="ct">🫘 金豆豆 {{ it.price }}</div>
          <div class="act-limit">{{ it.effectDesc || it.desc || '装扮商品' }}</div>
          <button v-if="it.canBuy" class="act-btn act-sm" @click="buy('decor', it.id, it.price, 1, it.name)">💎 购买</button>
          <button v-else class="act-btn act-sm disabled" disabled>金豆豆不足</button>
        </div>
        <p v-if="!decor.list.length" style="grid-column:1/-1;text-align:center;color:var(--muted);padding:24px 0">{{ acc() ? '暂无装扮商品' : '请先选择账号' }}</p>
      </div>
    </div>

    <!-- 道具 -->
    <div v-show="tab === 's-prop'">
      <div class="act-grid">
        <div v-for="it in mall.list" :key="it.id" class="act-item">
          <div class="lc-img"><img v-if="imgOf(it)" :src="imgOf(it)" style="width:44px;height:44px;object-fit:contain" loading="lazy"><span v-else style="font-size:22px">🎁</span></div>
          <div class="nm">{{ it.name }}</div>
          <div class="ct">{{ it.isFree ? '免费' : '🎫 ' + it.price + ' 点券' }}</div>
          <div class="act-limit"><span v-if="it.discount">折扣 {{ it.discount }}</span><span v-if="it.limitCount>0"> · 限{{ fmtNum(it.limitCount) }}</span></div>
          <template v-if="!it.isSoldOut && it.canBuy">
            <template v-if="isFert(it)">
              <div class="act-qty">
                <button class="act-btn act-sm" @click="mall.qty[it.id] = Math.max(1, (mall.qty[it.id]||1) - 1)">−</button>
                <input class="act-num" :value="mall.qty[it.id]||1" min="1" max="99" inputmode="numeric" @input="mall.qty[it.id] = Math.max(1, Math.min(99, Number($event.target.value)||1))">
                <button class="act-btn act-sm" @click="mall.qty[it.id] = Math.min(99, (mall.qty[it.id]||1) + 1)">＋</button>
              </div>
            </template>
            <button class="act-btn act-sm" @click="buy('mall', it.id, it.price, isFert(it) ? (mall.qty[it.id]||1) : 1, it.name)">{{ it.isFree ? '立即领取' : '🎫 兑换' }}</button>
          </template>
          <button v-else class="act-btn act-sm disabled" disabled>{{ it.isSoldOut ? '已售罄' : '点券不足' }}</button>
        </div>
        <p v-if="!mall.list.length" style="grid-column:1/-1;text-align:center;color:var(--muted);padding:24px 0">{{ acc() ? '暂无商城商品' : '请先选择账号' }}</p>
      </div>
    </div>

    <!-- 神秘商人 -->
    <div v-show="tab === 's-mystery'">
      <div class="act-card" style="max-width:340px"><div class="auto-item"><span>🛒 神秘商人自动购买</span><div class="switch" :class="{ on: mysteryAb }" @click="toggleMysteryAb"></div></div><div class="act-hint">开启后每 60 分钟自动购买当前在约商品（仅用所选货币结算）</div></div>
      <div class="act-card" style="max-width:340px"><div class="f-label">💱 神秘商人购买货币<small>自动购买仅用所选货币结算</small></div><div class="chips">
        <button v-for="(label, c) in CUR" :key="c" class="chip" :class="{ on: mysteryCur.includes(c) }" @click="toggleMysteryCur(c)">{{ label }}</button>
      </div></div>
      <template v-if="mystery">
        <p v-if="!mystery.active" class="shop-empty">🧙 神秘商人暂未出现，请稍后刷新看看。</p>
        <div v-else class="act-card" style="max-width:340px">
          <div class="act-card-hd"><h4>🧙 神秘商人</h4><span class="act-badge">限时{{ mystery.discount > 0 ? ' · ' + (mystery.discount / 10) + ' 折' : '' }}</span></div>
          <div class="act-item" style="margin:0">
            <div class="lc-img"><img v-if="mystery.itemImage" :src="mystery.itemImage" style="width:44px;height:44px;object-fit:contain" loading="lazy"><span v-else style="font-size:22px">🎁</span></div>
            <div class="nm">{{ mystery.itemName }}<span v-if="mystery.itemCount>1"> ×{{ mystery.itemCount }}</span></div>
            <div class="ct">{{ mystery.currencyName }} <span v-if="mystery.originalPrice>mystery.price" style="text-decoration:line-through;opacity:.6">{{ mystery.originalPrice }}</span> {{ fmtNum(mystery.price) }}</div>
            <div class="act-limit" v-if="mystery.endTime">⏰ {{ fmtTime(mystery.endTime) }} 离开</div>
            <div style="display:flex;gap:8px;margin-top:8px">
              <button class="act-btn act-sm" @click="mysteryBuy(mystery.npcId)">💎 购买</button>
              <button class="act-btn act-sm" @click="mysteryAbandon">请离</button>
            </div>
          </div>
        </div>
      </template>
      <p v-else style="text-align:center;color:var(--muted);padding:20px 0">{{ acc() ? '加载中…' : '请先选择账号' }}</p>
    </div>
  </div>
</template>
