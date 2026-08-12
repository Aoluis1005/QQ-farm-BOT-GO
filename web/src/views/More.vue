<script setup>
import { ref } from 'vue'
import Settings from '@/views/Settings.vue'
import Illustrated from '@/views/Illustrated.vue'
import Backend from '@/views/Backend.vue'

// 严格对齐原 HTML page-more 的 seg-5 五个分段
const segs = [
  { key: 'm-auto', label: '自动控制' },
  { key: 'm-strategy', label: '种植策略' },
  { key: 'm-default', label: '默认方案' },
  { key: 'm-codex', label: '图鉴' },
  { key: 'm-analysis', label: '分析' },
]
const active = ref('m-auto')
</script>

<template>
  <div class="more">
    <h3 class="pg-title">更多</h3>
    <div class="seg seg-5">
      <button
        v-for="s in segs"
        :key="s.key"
        class="seg-btn"
        :class="{ active: active === s.key }"
        @click="active = s.key"
      >{{ s.label }}</button>
    </div>

    <div class="seg-pane" :class="{ active: active === 'm-auto' }"><Settings /></div>
    <div class="seg-pane" :class="{ active: active === 'm-strategy' }">
      <div class="placeholder glass">种植策略（待接入）</div>
    </div>
    <div class="seg-pane" :class="{ active: active === 'm-default' }">
      <div class="placeholder glass">默认方案（待接入）</div>
    </div>
    <div class="seg-pane" :class="{ active: active === 'm-codex' }"><Illustrated /></div>
    <div class="seg-pane" :class="{ active: active === 'm-analysis' }">
      <div class="placeholder glass">分析（待接入）</div>
    </div>
  </div>
</template>

<style scoped>
.more { display: grid; gap: 14px; }
.pg-title { font-size: 20px; font-weight: 700; margin: 2px 2px 0; }
.placeholder { padding: 40px 16px; text-align: center; color: var(--muted); border-radius: var(--radius-lg); }
</style>
