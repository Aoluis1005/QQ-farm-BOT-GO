<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const subTabs = [
  { key: 'list', label: '📋 活动列表' },
  { key: 'group', label: '📦 活动组' },
  { key: 'season', label: '🍂 四季' },
  { key: 'solar', label: '☀️ 观星' },
  { key: 'guanxing', label: '🔭 观星详情' },
  { key: 'qingmei', label: '🍏 青梅' },
  { key: 'shop', label: '🏪 活动商店' },
]
const active = ref('list')
const items = ref([])
const busy = ref(false)

const subUrls = {
  list: '/api/activity/list',
  group: '/api/activity/group',
  season: '/api/activity/season',
  solar: '/api/activity/solar',
  guanxing: '/api/activity/guanxing',
  qingmei: '/api/activity/qingmei',
  qingmeiClaim: '/api/activity/qingmei/claim',
  shop: '/api/activity/shop',
}

function claimUrl() {
  return active.value === 'qingmei' ? subUrls.qingmeiClaim : subUrls[active.value]
}

async function load() {
  busy.value = true
  try {
    const url = subUrls[active.value]
    if (active.value === 'group') {
      const { data } = await api.get(url)
      const raw = data.data?.groups || data.data?.list || data.data || []
      items.value = Array.isArray(raw) ? raw : []
    } else {
      const { data } = await api.get(url)
      const raw = data.data?.list || data.data?.items || data.data || []
      items.value = Array.isArray(raw) ? raw : []
    }
    busy.value = false
  } catch (e) {
    app.error('加载失败：' + (e.response?.data?.error || e.message))
    items.value = []
    busy.value = false
  }
}

// 领取奖励
async function claimReward(item, endpoint) {
  busy.value = true
  try {
    // qingmei 领取为裸 POST 无 body
    const isQingmei = endpoint.includes('qingmei/claim')
    const body = isQingmei ? {} : { groupId: item.groupId || item.id, activityId: item.activityId || item.id }
    const { data } = await api.post(endpoint, body)
    app.success(data.message || '领取成功')
    await load()
  } catch (e) {
    app.error('领取失败：' + (e.response?.data?.error || e.message))
    busy.value = false
  }
}

// 活动商店兑换（参数在 URL query: ?id=&slotId=&count=）
async function shopExchange(item) {
  busy.value = true
  try {
    const { data } = await api.post('/api/activity/shop/exchange', null, {
      params: { id: item.id, slotId: item.slotId || item.slot || 0, count: 1 },
    })
    app.success(data.message || '兑换成功')
    await load()
  } catch (e) {
    app.error('兑换失败：' + (e.response?.data?.error || e.message))
    busy.value = false
  }
}

// 青梅酿酒（裸 POST 无 body）
async function qingmeiMakeWine() {
  busy.value = true
  try {
    const { data } = await api.post('/api/activity/qingmei/wine', {})
    app.success(data.message || '酿酒已提交')
    await load()
  } catch (e) {
    app.error('酿酒失败：' + (e.response?.data?.error || e.message))
    busy.value = false
  }
}

onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button v-for="t in subTabs" :key="t.key"
        class="tab" :class="{ active: active === t.key }"
        @click="active = t.key; load()">
        {{ t.label }}
      </button>
    </div>

    <!-- 活动列表 -->
    <div v-if="active === 'list'">
      <div class="act-list">
        <div v-for="(a, i) in items" :key="i" class="act-item glass">
          <div class="act-name">{{ a.name || a.title || ('活动 ' + i) }}</div>
          <div class="act-desc">{{ a.desc || a.description || '' }}</div>
          <div class="act-time" v-if="a.startTime || a.endTime">
            {{ a.startTime ? new Date(a.startTime * 1000).toLocaleDateString() : '' }} ~ {{ a.endTime ? new Date(a.endTime * 1000).toLocaleDateString() : '' }}
          </div>
          <button class="btn primary sm" v-if="a.canClaim" @click="claimReward(a, '/api/activity/list')">🎁 领取奖励</button>
        </div>
        <div v-if="!items.length" class="empty-tip">暂无进行中的活动</div>
      </div>
    </div>

    <!-- 活动组 -->
    <div v-if="active === 'group'">
      <div class="act-grid">
        <div v-for="(g, i) in items" :key="i" class="act-card glass">
          <div class="act-name">{{ g.groupName || g.name || ('活动组 ' + i) }}</div>
          <div class="act-desc" v-if="g.subCount">含 {{ g.subCount }} 个子活动</div>
          <button class="btn primary sm" v-if="g.canClaimAll" @click="claimReward(g, '/api/activity/group')">🎁 一键领取</button>
        </div>
        <div v-if="!items.length" class="empty-tip">暂无活动组</div>
      </div>
    </div>

    <!-- 四季 / 观星 / 观星详情 / 青梅 / 活动商店 -->
    <div v-else>
      <div class="act-grid">
        <div v-for="(a, i) in items" :key="i" class="act-item glass">
          <div class="act-name">{{ a.name || a.title || ('条目 ' + i) }}</div>
          <div class="act-desc" v-if="a.desc || a.description">{{ a.desc || a.description }}</div>
          <div class="act-price" v-if="a.price || a.cost">💰 {{ a.price || a.cost }}</div>
          <div class="act-actions" v-if="active === 'shop' && a.canBuy">
            <button class="btn primary sm" @click="shopExchange(a)">兑换</button>
          </div>
          <button class="btn primary sm" v-if="a.canClaim" @click="claimReward(a, claimUrl())">🎁 领取奖励</button>
          <button class="btn accent sm" v-if="active === 'qingmei' && a.canMakeWine" @click="qingmeiMakeWine()">🍷 酿酒</button>
        </div>
        <div v-if="!items.length" class="empty-tip">暂无数据</div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border-radius: var(--radius-lg); }
.tabs { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; }
.tab { padding: 8px 14px; border-radius: 999px; border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); cursor: pointer; font-size: 13px; }
.tab.active { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
.act-list { display: flex; flex-direction: column; gap: 10px; }
.act-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; }
.act-item, .act-card { padding: 14px; border-radius: var(--radius-md); border: 1px solid var(--border); }
.act-name { font-weight: 700; margin-bottom: 4px; font-size: 14px; }
.act-desc { font-size: 13px; color: var(--muted); margin-bottom: 6px; }
.act-time { font-size: 11px; color: var(--muted); margin-bottom: 8px; }
.act-price { font-size: 14px; color: var(--accent); margin-bottom: 8px; }
.act-actions { display: flex; gap: 6px; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 24px 0; text-align: center; grid-column: 1 / -1; }
</style>
