<script setup>
import { ref, watch, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const tabs = [
  { key: 'seed', label: '🌱 种子', url: '/api/shop/seed' },
  { key: 'pet', label: '🐾 宠物', url: '/api/shop/pet' },
  { key: 'decoration', label: '🎀 装扮', url: '/api/shop/decoration' },
  { key: 'mall', label: '🛍️ 商城', url: '/api/shop/mall' },
  { key: 'mystery', label: '🔮 神秘', url: '/api/shop/mystery' },
]
const active = ref('seed')
const items = ref([])
const busy = ref(false)
const buyQty = ref({})

// 获取购买数量
function getQty(item) {
  return buyQty.value[item.id] ?? 1
}
function setQty(item, val) {
  const v = Math.max(1, Math.min(99, parseInt(val) || 1))
  buyQty.value = { ...buyQty.value, [item.id]: v }
}

async function load() {
  const t = tabs.find((x) => x.key === active.value)
  if (!t) return
  try {
    const { data } = await api.get(t.url)
    const raw = data.data?.list || data.data?.items || data.data || []
    items.value = Array.isArray(raw) ? raw : []
  } catch (e) {
    app.error('加载商店失败：' + (e.response?.data?.error || e.message))
    items.value = []
  }
}

async function buy(it) {
  busy.value = true
  try {
    let url = '/api/shop/buy'
    let body
    const q = getQty(it)
    const id = it.id ?? it.goodsId
    if (active.value === 'mall') {
      url = '/api/shop/mall/buy'
      body = { goodsId: id, count: q }
    } else if (active.value === 'mystery') {
      url = '/api/shop/mystery/buy'
      body = { npcId: id }
    } else {
      // 种子/宠物/装扮: {goodsId, num, price}
      body = { goodsId: id, num: q, price: it.price || 0 }
    }
    const { data } = await api.post(url, body)
    app.success(data.message || '购买成功')
    await load()
  } catch (e) {
    app.error('购买失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

async function abandonMystery() {
  busy.value = true
  try {
    const { data } = await api.post('/api/shop/mystery/abandon', {})
    app.success(data.message || '已放弃')
    await load()
  } catch (e) {
    app.error('放弃失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

function priceText(it) {
  if (it.isGoldenBean) return '🫘 ' + it.price
  return '💰 ' + (it.price ?? it.cost ?? '-')
}

function canBuy(it) {
  if (it.isSoldOut) return false
  if (it.unlocked === false) return false
  if (it.canBuy === false) return false
  return true
}

function buyLabel(it) {
  if (it.isSoldOut) return '已售罄'
  if (it.unlocked === false) return '未解锁'
  if (!it.canBuy) return it.requiredLevel ? '需 Lv.' + it.requiredLevel : '无法购买'
  if (it.isGoldenBean) return '🫘 购买'
  return '💎 购买'
}

watch(active, load)
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button v-for="t in tabs" :key="t.key"
        class="tab" :class="{ active: active === t.key }"
        @click="active = t.key">{{ t.label }}</button>
    </div>

    <!-- 商城特殊布局：左侧分类列表 -->
    <template v-if="active === 'shopMall'">
      <div class="mall-grid">
        <div v-for="(it, i) in items" :key="i" class="shop-item">
          <div class="si-name">{{ it.name || it.itemName || ('商品' + i) }}</div>
          <div class="si-price">{{ priceText(it) }}</div>
          <div class="si-limit" v-if="it.requiredLevel">Lv.{{ it.requiredLevel }}</div>
          <button class="btn primary sm" :disabled="busy || !canBuy(it)" @click="buy(null, it)">
            {{ buyLabel(it) }}
          </button>
        </div>
        <div v-if="!items.length" class="empty-tip">该类目暂无商品</div>
      </div>
    </template>

    <!-- 种子/宠物/装扮：支持购买数量 -->
    <template v-else-if="active === 'seed' || active === 'pet' || active === 'decoration'">
      <div class="shop-grid">
        <div v-for="(it, i) in items" :key="i" class="shop-item">
          <div class="si-name">{{ it.name || it.itemName || ('商品' + i) }}</div>
          <div class="si-price">{{ priceText(it) }}</div>
          <div class="si-limit" v-if="it.requiredLevel">
            Lv.{{ it.requiredLevel }} ·
            <template v-if="it.itemCount">每季×{{ it.itemCount }} · </template>
            {{ it.expPerSeason }}经验
            <span v-if="it.limitCount"> · 限{{ it.limitCount }}</span>
          </div>
          <div class="si-desc" v-if="it.desc || it.effectDesc">{{ it.desc || it.effectDesc }}</div>
          <div class="si-actions" v-if="canBuy(it) && active !== 'decoration'">
            <input type="number" class="qty-input" :value="getQty(it)"
              min="1" max="99" @input="setQty(it, $event.target.value)" />
          </div>
          <button class="btn primary sm" :disabled="busy || !canBuy(it)" @click="buy(null, it)">
            {{ buyLabel(it) }}
          </button>
        </div>
        <div v-if="!items.length" class="empty-tip">该类目暂无商品</div>
      </div>
    </template>

    <!-- 神秘商店：支持放弃 -->
    <template v-else-if="active === 'mystery'">
      <div class="shop-grid">
        <div v-for="(it, i) in items" :key="i" class="shop-item mystery-item">
          <div class="si-name">{{ it.name || it.itemName || ('神秘商品' + i) }}</div>
          <div class="si-price">{{ priceText(it) }}</div>
          <div class="si-limit" v-if="it.requiredLevel">Lv.{{ it.requiredLevel }}</div>
          <div class="si-actions">
            <button class="btn primary sm" :disabled="busy || !canBuy(it)" @click="buy(null, it)">
              {{ buyLabel(it) }}
            </button>
            <button class="btn ghost sm" :disabled="busy" @click="abandonMystery()">放弃</button>
          </div>
        </div>
        <div v-if="!items.length" class="empty-tip">暂无神秘商品</div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border-radius: var(--radius-lg); }
.tabs { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; }
.tab { padding: 8px 14px; border-radius: 999px; border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); cursor: pointer; font-size: 13px; }
.tab.active { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
.shop-grid, .mall-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 12px; }
.shop-item { padding: 14px; border-radius: var(--radius-md); background: var(--card-strong); border: 1px solid var(--border); text-align: center; }
.si-name { font-size: 14px; font-weight: 600; margin-bottom: 6px; }
.si-price { font-size: 13px; color: var(--accent); margin-bottom: 6px; }
.si-limit { font-size: 11px; color: var(--muted); margin-bottom: 8px; }
.si-desc { font-size: boog: 11px; color: var(--muted); margin-bottom: 8px; }
.si-actions { display: flex; gap: 6px; justify-content: center; align-items: center; margin-bottom: 8px; }
.qty-input { width: 50px; padding: 4px; text-align: center; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--foreground); }
.empty-tip { color: var(--muted); font-size: 13px; grid-column: 1 / -1; }
.mystery-item .si-actions { flex-direction: row; }
</style>
