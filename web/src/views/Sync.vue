<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import api from '@/api'
import { useRouter } from 'vue-router'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const router = useRouter()
const tab = ref('assets')
const status = ref(null)
const syncing = ref(false)
const busy = ref(false)
const cfgSyncing = ref(false)
const cfgBusy = ref(false)
let timer = null
let cfgTimer = null

const assets = computed(() => status.value?.assets || {})
const last = computed(() => assets.value.last || null)
const progress = computed(() => assets.value.progress || { done: 0, total: 0, pct: 0 })

const cfg = computed(() => status.value?.config || {})
const cfgLast = computed(() => cfg.value.last || null)

const newItems = computed(() => {
  if (!last.value?.detail) return []
  return last.value.detail.filter((d) => d.status === 'added' || d.status === 'updated')
})
const failures = computed(() => {
  if (!last.value?.detail) return []
  return last.value.detail.filter((d) => d.status === 'failed').slice(0, 6)
})
const cfgFiles = computed(() => cfgLast.value?.files || [])

async function loadStatus() {
  try {
    const { data } = await api.get('/api/sync/status')
    status.value = data
    syncing.value = !!data.assets?.syncing
    cfgSyncing.value = !!data.config?.running
  } catch (e) {
    /* 静默 */
  }
}

function fmtTime(t) {
  if (!t) return '从未同步'
  const d = new Date(t)
  if (isNaN(d.getTime())) return t
  const p = (n) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function check() {
  if (busy.value || syncing.value) return
  busy.value = true
  try {
    const { data } = await api.post('/api/sync/assets/check')
    if (data?.ok) {
      app.success('开始检查素材更新')
      syncing.value = true
      poll()
    } else app.error(data?.error || '启动失败')
  } catch (e) {
    app.error(e.response?.data?.error || '网络错误')
  } finally {
    busy.value = false
  }
}

async function checkConfig() {
  if (cfgBusy.value || cfgSyncing.value) return
  cfgBusy.value = true
  try {
    const { data } = await api.post('/api/sync/config/check')
    if (data?.ok) {
      app.success('开始检查配置更新')
      cfgSyncing.value = true
      pollConfig()
    } else app.error(data?.error || '启动失败')
  } catch (e) {
    app.error(e.response?.data?.error || '网络错误')
  } finally {
    cfgBusy.value = false
  }
}

function poll() {
  clearInterval(timer)
  timer = setInterval(async () => {
    await loadStatus()
    if (!syncing.value) {
      clearInterval(timer)
      if (last.value) {
        const r = last.value
        if (r.failed > 0) {
          app.error(`素材同步完成:新增 ${r.added} · 更新 ${r.updated} · 失败 ${r.failed}`)
        } else {
          app.success(`素材同步完成:新增 ${r.added} · 更新 ${r.updated} · 跳过 ${r.skipped}`)
        }
      }
    }
  }, 2000)
}

function pollConfig() {
  clearInterval(cfgTimer)
  cfgTimer = setInterval(async () => {
    await loadStatus()
    if (!cfgSyncing.value) {
      clearInterval(cfgTimer)
      const r = cfgLast.value
      if (r) {
        if (r.failed > 0) {
          app.error(`配置同步完成:新增 ${r.added} · 更新 ${r.updated} · 失败 ${r.failed}`)
        } else {
          app.success(`配置同步完成:新增 ${r.added} · 更新 ${r.updated} · 保留 ${r.kept}`)
        }
      }
    }
  }, 2000)
}

function tabStyle(name) {
  const active = tab.value === name
  return {
    padding: '9px 2px 11px',
    fontSize: '14px',
    cursor: 'pointer',
    fontWeight: active ? 700 : 400,
    color: active ? 'var(--primary)' : 'var(--muted)',
    borderBottom: active ? '2px solid var(--primary)' : '2px solid transparent',
  }
}

onMounted(() => {
  loadStatus()
  if (assets.value?.syncing) poll()
  if (cfg.value?.running) pollConfig()
})
onUnmounted(() => {
  clearInterval(timer)
  clearInterval(cfgTimer)
})
</script>

<template>
  <div>
    <div class="subbar">
      <button class="icon-btn" @click="router.push('/more')">‹</button>
      <h3>同步管理</h3>
      <span v-if="syncing" style="margin-left:8px;font-size:11.5px;color:var(--primary)">同步中 {{ progress.done }}/{{ progress.total }}</span>
    </div>

    <div style="display:flex;gap:24px;padding:0 16px;border-bottom:0.5px solid var(--border);">
      <div :style="tabStyle('assets')" @click="tab = 'assets'">素材</div>
      <div :style="tabStyle('config')" @click="tab = 'config'">配置</div>
    </div>

    <!-- 素材 Tab -->
    <div v-if="tab === 'assets'" style="padding:12px;">
      <div class="sec-title" style="margin:2px 0 10px"><span>素材增量同步</span></div>

      <div class="card" style="display:flex;align-items:center;gap:12px;padding:14px;border-radius:var(--radius-md);">
        <div style="width:44px;height:44px;border-radius:12px;background:var(--primary-soft);display:flex;align-items:center;justify-content:center;font-size:18px;color:var(--primary);flex:none;">🖼️</div>
        <div style="flex:1;min-width:0;">
          <div style="font-size:12px;color:var(--muted)">素材包版本 · plant</div>
          <div style="font-size:16px;font-weight:700;font-family:var(--font-mono);">{{ (last && last.bundles && last.bundles.plant) || '--' }}</div>
        </div>
        <div style="text-align:right;font-size:11.5px;color:var(--muted)">
          上次同步<br>
          <span style="font-size:13px;color:var(--foreground);font-weight:500;">{{ fmtTime(assets.lastSync || (last && last.finishedAt)) }}</span>
        </div>
      </div>

      <div style="display:flex;gap:10px;margin-top:12px;">
        <button :disabled="busy || syncing" style="flex:1.6;padding:13px;border:none;border-radius:var(--radius-md);background:var(--primary);color:var(--on-primary);font-size:14px;font-weight:700;cursor:pointer;" @click="check">
          {{ syncing ? '同步中…' : '检查更新' }}
        </button>
        <button :disabled="syncing" style="flex:1;padding:13px;border:0.5px solid var(--border);border-radius:var(--radius-md);background:var(--card);color:var(--primary);font-size:14px;font-weight:500;cursor:pointer;" @click="loadStatus">刷新状态</button>
      </div>

      <div v-if="syncing" class="card" style="margin-top:12px;padding:12px 14px;border-radius:var(--radius-md);">
        <div style="display:flex;justify-content:space-between;font-size:12px;color:var(--muted);margin-bottom:7px;">
          <span>{{ assets.current || '正在解析资源…' }}</span>
          <span style="font-family:var(--font-mono);color:var(--primary);">{{ progress.pct }}%</span>
        </div>
        <div style="height:6px;border-radius:999px;background:var(--primary-soft);overflow:hidden;">
          <div :style="{ width: progress.pct + '%', height: '100%', background: 'var(--primary)', borderRadius: '999px', transition: 'width .4s' }"></div>
        </div>
      </div>

      <template v-if="last && !syncing">
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-top:12px;">
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--good);">{{ last.added }}</div>
            <div style="font-size:11.5px;color:var(--muted)">新增</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--warn);">{{ last.updated }}</div>
            <div style="font-size:11.5px;color:var(--muted)">更新</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;">{{ last.skipped }}</div>
            <div style="font-size:11.5px;color:var(--muted)">跳过</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--danger);">{{ last.failed }}</div>
            <div style="font-size:11.5px;color:var(--muted)">失败</div>
          </div>
        </div>

        <div v-if="newItems.length" class="sec-title" style="margin:14px 0 8px"><span>本次新增素材</span></div>
        <div v-if="newItems.length" style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;">
          <div v-for="it in newItems.slice(0, 12)" :key="it.file" class="card" style="padding:6px;border-radius:var(--radius-sm);text-align:center;">
            <img :src="'/game-config/seed_images_named/' + it.file" style="width:100%;height:44px;object-fit:contain;border-radius:6px;background:var(--bg-lo);">
            <div style="font-size:10.5px;color:var(--muted);margin-top:4px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">{{ it.name }}</div>
          </div>
        </div>

        <div v-if="failures.length" class="card" style="margin-top:14px;padding:10px 12px;border-radius:var(--radius-md);">
          <div style="font-size:12px;color:var(--danger);font-weight:500;margin-bottom:6px;">失败 {{ failures.length }} 项(前 {{ failures.length }} 条)</div>
          <div v-for="(f, i) in failures" :key="i" style="font-size:11.5px;color:var(--muted);line-height:1.7;">
            <span style="color:var(--foreground);">{{ f.itemId }} {{ f.name }}</span> · {{ f.reason }}
          </div>
        </div>
      </template>

      <p v-else-if="!last && !syncing" style="margin-top:14px;text-align:center;font-size:12px;color:var(--muted);">尚未同步过素材,点击「检查更新」获取最新素材</p>
    </div>

    <!-- 配置 Tab -->
    <div v-if="tab === 'config'" style="padding:12px;">
      <div class="sec-title" style="margin:2px 0 10px"><span>配置同步</span></div>

      <div class="card" style="display:flex;align-items:center;gap:12px;padding:14px;border-radius:var(--radius-md);">
        <div style="width:44px;height:44px;border-radius:12px;background:var(--primary-soft);display:flex;align-items:center;justify-content:center;font-size:18px;color:var(--primary);flex:none;">📋</div>
        <div style="flex:1;min-width:0;">
          <div style="font-size:12px;color:var(--muted)">配置来源 · 官方 CDN(mainscene)</div>
          <div style="font-size:16px;font-weight:700;font-family:var(--font-mono);">{{ cfgLast?.source === 'cdnsync' ? '已连接' : '--' }}</div>
        </div>
        <div style="text-align:right;font-size:11.5px;color:var(--muted)">
          上次同步<br>
          <span style="font-size:13px;color:var(--foreground);font-weight:500;">{{ fmtTime(cfgLast?.syncedAt || cfg.lastAt) }}</span>
        </div>
      </div>

      <div style="display:flex;gap:10px;margin-top:12px;">
        <button :disabled="cfgBusy || cfgSyncing" style="flex:1.6;padding:13px;border:none;border-radius:var(--radius-md);background:var(--primary);color:var(--on-primary);font-size:14px;font-weight:700;cursor:pointer;" @click="checkConfig">
          {{ cfgSyncing ? '同步中…' : '检查更新' }}
        </button>
        <button :disabled="cfgSyncing" style="flex:1;padding:13px;border:0.5px solid var(--border);border-radius:var(--radius-md);background:var(--card);color:var(--primary);font-size:14px;font-weight:500;cursor:pointer;" @click="loadStatus">刷新状态</button>
      </div>

      <div v-if="cfgSyncing" class="card" style="margin-top:12px;padding:12px 14px;border-radius:var(--radius-md);">
        <div style="display:flex;justify-content:space-between;font-size:12px;color:var(--muted);">
          <span>{{ cfg.current || '正在拉取配置…' }}</span>
          <span style="color:var(--primary);">后台同步中</span>
        </div>
      </div>

      <template v-if="cfgLast && !cfgSyncing">
        <div style="display:grid;grid-template-columns:repeat(4,1fr);gap:8px;margin-top:12px;">
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--good);">{{ cfgLast.added }}</div>
            <div style="font-size:11.5px;color:var(--muted)">新增 id</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--warn);">{{ cfgLast.updated }}</div>
            <div style="font-size:11.5px;color:var(--muted)">更新</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;">{{ cfgLast.kept }}</div>
            <div style="font-size:11.5px;color:var(--muted)">保留本地</div>
          </div>
          <div class="card" style="padding:10px 0;text-align:center;border-radius:var(--radius-md);">
            <div style="font-size:18px;font-weight:700;color:var(--danger);">{{ cfgLast.failed }}</div>
            <div style="font-size:11.5px;color:var(--muted)">失败</div>
          </div>
        </div>

        <div v-if="cfgLast.newIds && cfgLast.newIds.length" class="card" style="margin-top:12px;padding:10px 12px;border-radius:var(--radius-md);">
          <div style="font-size:12px;color:var(--good);font-weight:500;margin-bottom:6px;">新增物品 id({{ cfgLast.newIds.length }})</div>
          <div style="display:flex;flex-wrap:wrap;gap:6px;">
            <span v-for="id in cfgLast.newIds" :key="id" style="font-family:var(--font-mono);font-size:11.5px;background:var(--bg-lo);border-radius:6px;padding:3px 8px;">{{ id }}</span>
          </div>
        </div>

        <div class="card" style="margin-top:12px;padding:10px 12px;border-radius:var(--radius-md);">
          <div style="font-size:12px;color:var(--muted);font-weight:500;margin-bottom:6px;">配置表明细</div>
          <div v-for="f in cfgFiles" :key="f.file" style="display:flex;align-items:center;gap:8px;font-size:11.5px;line-height:2;border-bottom:0.5px solid var(--border);padding:4px 0;">
            <span :style="{ width: '8px', height: '8px', borderRadius: '50%', background: f.status === 'updated' ? 'var(--warn)' : f.status === 'failed' ? 'var(--danger)' : 'var(--muted-2)', flex: 'none' }"></span>
            <span style="font-family:var(--font-mono);color:var(--foreground);">{{ f.file }}</span>
            <span v-if="f.status === 'updated'" style="color:var(--warn);">已更新</span>
            <span v-else-if="f.status === 'skipped'" style="color:var(--muted);">无变化</span>
            <span v-else-if="f.status === 'failed'" style="color:var(--danger);">失败</span>
            <span style="margin-left:auto;color:var(--muted);font-size:11px;">{{ f.reason || (f.status === 'skipped' ? 'hash 一致' : '') }}</span>
          </div>
        </div>
      </template>

      <p v-else-if="!cfgLast && !cfgSyncing" style="margin-top:14px;text-align:center;font-size:12px;color:var(--muted);">尚未同步过配置,点击「检查更新」从官方 CDN 拉取 ItemInfo / Plant / MutantEffect</p>
    </div>
  </div>
</template>
