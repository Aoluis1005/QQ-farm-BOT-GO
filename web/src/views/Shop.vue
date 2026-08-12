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

async function load() {
  const t = tabs.find((x) => x.key === active.value)
  try {
    const { data } = await api.get(t.url)
    items.value = data.data?.list || data.data?.items || data.data || []
  } catch (e) {
    app.error('加载商店失败：' + (e.response?.data?.error || e.message))
    items.value = []
  }
}

async function buy(it) {
  busy.value = true
  try {
    let url = '/api/shop/buy'
    if (active.value === 'mall') url = '/api/shop/mall/buy'
    if (active.value === 'mystery') url = '/api/shop/mystery/buy'
    const { data } = await api.post(url, { itemId: it.id ?? it.itemId, count: 1 })
    app.success(data.message || '购买成功')
    await load()
  } catch (e) {
    app.error('购买失败：' + (e.response?.data?.error || e.message))
  } finally {
    busy.value = false
  }
}

watch(active, load)
onMounted(load)
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button
        v-for="t in tabs"
        :key="t.key"
        class="tab"
        :class="{ active: active === t.key }"
        @click="active = t.key"
      >
        {{ t.label }}
      </button>
    </div>
    <div class="shop-grid">
      <div v-for="(it, i) in items" :key="i" class="shop-item">
        <div class="si-name">{{ it.name || it.itemName || ('商品' + i) }}</div>
        <div class="si-price">💰 {{ it.price ?? it.cost ?? '-' }}</div>
        <button class="btn primary sm" :disabled="busy" @click="buy(it)">购买</button>
      </div>
      <div v-if="!items.length" class="empty-tip">该类目暂无商品</div>
    </div>
  </section>
</template>

<style scoped>
.panel {
  padding: 18px;
  border-radius: var(--radius-lg);
}
.tabs {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 14px;
}
.tab {
  padding: 8px 14px;
  border-radius: 999px;
  border: 1px solid var(--border);
  background: var(--card-strong);
  color: var(--foreground);
  cursor: pointer;
  font-size: 13px;
}
.tab.active {
  background: var(--primary);
  color: var(--on-primary);
  border-color: var(--primary);
}
.shop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 12px;
}
.shop-item {
  padding: 14px;
  border-radius: var(--radius-md);
  background: var(--card-strong);
  border: 1px solid var(--border);
  text-align: center;
}
.si-name {
  font-size: 14px;
  font-weight: 600;
  margin-bottom: 6px;
}
.si-price {
  font-size: 13px;
  color: var(--accent);
  margin-bottom: 10px;
}
.empty-tip {
  color: var(--muted);
  font-size: 13px;
  grid-column: 1 / -1;
}
</style>
