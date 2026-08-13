<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const tabs = [
  { key: 'bag', label: '🎒 背包' },
  { key: 'lands', label: '🌱 土地' },
  { key: 'history', label: '📜 收益记录' },
]
const active = ref('bag')

// 背包
const bag = ref({ items: [], seeds: [] })
async function loadBag() {
  try {
    const [itemsRes, seedsRes] = await Promise.all([
      api.get('/api/bag/items'),
      api.get('/api/bag/seeds'),
    ])
    bag.value = {
      items: itemsRes.data.data?.items || itemsRes.data.data || [],
      seeds: seedsRes.data.data?.list || seedsRes.data.data || [],
    }
  } catch (e) {
    app.error('加载背包失败：' + (e.response?.data?.error || e.message))
  }
}
async function useBagItem(item, count) {
  try {
    const { data } = await api.post('/api/bag/use', { itemId: item.id, count })
    app.success(data.message || '使用成功')
    await loadBag()
  } catch (e) {
    app.error('使用失败：' + (e.response?.data?.error || e.message))
  }
}
async function sellBagItem(item, countSQL) {
  try {
    const { data } = await api.post('/api/bag/sell', { items: [{ id: item.id, count: count || 1, uid: item.uuid || '' }] })
    app.success(data.message || '出售成功')
    await loadBag()
  } catch (e) {
    app.error('出售失败：' + (e.response?.data?.error || e.message))
  }
}

// 土地
const lands = ref([])
async function loadLands() {
  try {
    const { data } = await api.get('/api/farm/lands')
    lands.value = data.data || []
  } catch (e) {
    app.error('加载土地失败：' + (e.response?.data?.error || e.message))
  }
}
async function landAction(landId, action) {
  try {
    const url = action === 'fertilize' ? '/api/land/fertilize' : '/api/land/remove'
    const { data } = await api.post(url, { landId })
    app.success(data.message || '操作成功')
    await loadLands()
  } catch (e) {
    app.error('操作失败：' + (e.response?.data?.error || e.message))
  }
}
async function removeAllLands() {
  try {
    const { data } = await api.post('/api/land/remove-all')
    app.success(data.message || '全部铲除成功')
    await loadLands()
  } catch (e) {
    app.error('铲除失败：' + (e.response?.data?.error || e.message))
  }
}
async function farmAction(action) {
  try {
    const { data } = await api.post('/api/farm/action', { action })
    app.success(data.message || '操作成功')
    await loadLands()
  } catch (e) {
    app.error('操作失败：' + (e.response?.data?.error || e.message))
  }
}

onMounted(() => { loadBag(); loadLands() })
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button v-for="t in tabs" :key="t.key"
        class="tab" :class="{ active: active === t.key }"
        @click="active = t.key">{{ t.label }}</button>
    </div>

    <!-- 背包 -->
    <div v-if="active === 'bag'">
      <div class="sec-head"><h2>物品</h2></div>
      <div class="bag-grid">
        <div v-for="(it, i) in bag.items" :key="i" class="bag-item glass">
          <div class="bag-name">{{ it.name || ('物品 ' + i) }}</div>
          <div class="bag-count">×{{ it.count || 0 }}</div>
          <div class="bag-actions">
            <button class="btn ghost sm" @click="useBagItem(it, 1)" v-if="hasBagCanUse(it)">
              使用</button>
            <button class="btn ghost sm" @click="sellBagItem(it, 1)" v-if="it.canSell">出售</button>
          </div>
        </div>
      </div>
      <div v-if="!bag.items.length" class="empty-tip">暂无物品</div>
    </div>

    <!-- 土地 -->
    <div v-if="active === 'lands'">
      <div class="sec-head">
        <h2>农场土地</h2>
        <div style="display:flex;gap:6px">
          <button class="btn ghost sm" @click="farmAction('harvest')">🧺 一键收菜</button>
          <button class="btn ghost sm" @click="farmAction('plant')">🌱 一键种植</button>
          <button class="btn ghost sm" @click="removeAllLands" v-if="lands.length">🗑 全部铲除</button>
          <button class="btn ghost sm" @click="loadLands">刷新</button>
        </div>
      </div>
      <div class="land-grid">
        <div v-for="ld in lands" :key="ld.id" class="land item glass">
          <div class="land-top">
            <span>{{ ld.landTypeName || '地块' }} Lv.{{ ld.level }}</span>
            <span class="st-{{ ld.status }}">{{ ld.status || 'empty' }}</span>
          </div>
          <div class="land-plant" v-if="ld.plantName">{{ ld.plantName }} · {{ ld.phaseName }}</div>
          <div class="land-progress" v-if="ld.growProgress !== undefined">
            <div class="bar"><div class="fill" :style="{ width: ld.growProgress + '%' }"></div></div>
          </div>
          <div class="land-actions">
            <button class="btn ghost xs" @click="landAction(ld.id, 'fertilize')" v-if="landCanFertilize(ld)">🧪 催熟</button>
            <button class="btn ghost xs" @click="landAction(ld.id, 'remove')" v-show="landCanRemove(ld)">铲除</button>
          </div>
        </div>
        <div v-if="!lands.length" class="empty-tip">暂无土地数据</div>
      </div>
    </div>

    <!-- 收益记录 -->
    <div v-if="active === 'history'">
      <div v-if="true" class="placeholder glass" style="padding:40px;text-align:center">
        收益记录（接入中：/api/logs + /api/home/income/history）
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border-radius: var(--radius-lg); }
.tabs { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; }
.tab { padding: 8px 14px; border-radius: 999px; border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); cursor: pointer; font-size: 13px; }
.tab.active { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
.sec-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.sec-head h2 { font-size: 16px; margin: 0; }
.bag-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); gap: 10px; }
.bag-item { padding: 12px; border-radius: var(--radius-md); border: 1p solid var(--border); text-align: center; }
.bag-name { font-size: 14px; margin-bottom: 4px; }
.bag-count { font-size: 18px; font-weight: 700; margin-bottom: 8px; color: var(--accent); }
.bag-actions { display: flex; gap: 4px; justify-content: center; }
.land-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 12px; }
.land-item { padding: 12px; border-radius: var(--radius-md); border: 1px solid var(--border); }
.land-top { display: flex; justify-content: space-between; font-size: 13px; }
.land-plant { font-size: 12px; color: var(--muted); margin: 4px 0; }
.land-progress { margin: 4px 0; }
.land-progress .bar { height: 6px; border-radius: 999px; background: var(--card-strong); overflow: hidden; }
.land-progress .fill { height: 100%; background: var(--gradient, var(--primary)); }
.land-actions { display: flex; gap: 4px; margin-top: 6px; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 24px 0; text-align: center; grid-column: 1 / -1; }
.placeholder { border-radius: var(--radius-lg); }
</style>
