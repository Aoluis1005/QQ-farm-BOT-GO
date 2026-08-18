<script setup>
import { ref, computed, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const tabs = [
  { key: 'list', label: '👥 好友列表' },
  { key: 'visitors', label: '👀 访客' },
  { key: 'blacklist', label: '🚫 黑名单' },
  { key: 'dog', label: '🐕 护主犬' },
  { key: 'delete', label: '🗑 删除好友' },
]
const active = ref('list')

// 好友列表
const friends = ref([])
const friendLoading = ref(false)
async function loadFriends(forceSync = false) {
  friendLoading.value = true
  try {
    // 默认走展示缓存（秒回）；仅传 forceSync=true 时强制绕过缓存拉最新（刷新按钮/删除后）
    const { data } = await api.get('/api/friends/list', forceSync ? { params: { forceSync: true } } : {})
    friends.value = data.data?.friends || data.data || []
  } catch (e) {
    app.error('加载好友失败：' + (e.response?.data?.error || e.message))
    friends.value = []
  } finally { friendLoading.value = false }
}

function isGuardDog(f) { return Number(f.dogId) === 90021 }
function guardDogCount() { return friends.value.filter(isGuardDog).length }

// 好友操作
async function friendOp(f, opType) {
  try {
    const { data } = await api.post(`/api/friend/${f.uid}/op`, { opType })
    app.success(data.message || '操作成功')
    await loadFriends()
  } catch (e) {
    app.error('操作失败：' + (e.response?.data?.error || e.message))
  }
}

// 访客
const visitors = ref([])
const visitorFilter = ref('all')
const visitorTotal = ref(0)
const visitorStats = ref({ steal: 0, help: 0, bad: 0 })
async function loadVisitors() {
  try {
    const { data } = await api.get('/api/friends/visitors')
    const list = data.data || []
    visitors.value = list
    visitorTotal.value = list.length
    visitorStats.value = {
      steal: list.filter(r => Number(r.actionType) === 1).length,
      help: list.filter(r => Number(r.actionType) === 2).length,
      bad: list.filter(r => Number(r.actionType) === 3).length,
    }
  } catch (e) {
    app.error('加载访客失败：' + (e.response?.data?.error || e.message))
  }
}
const filteredVisitors = computed(() => {
  if (visitorFilter.value === 'all') return visitors.value.slice(0, 50)
  const map = { steal: 1, help: 2, bad: 3 }
  return visitors.value.filter(r => Number(r.actionType) === map[visitorFilter.value]).slice(0, 50)
})

function interactBadgeText(type) {
  return { 1: '偷菜', 2: '帮忙', 3: '捣乱' }[type] || '交互'
}
function formatTime(sec) {
  if (!sec) return '-'
  const d = new Date(Number(sec) * 1000)
  return d.toLocaleString()
}

// 黑名单
const blacklist = ref([])
async function loadBlacklist() {
  try {
    const { data } = await api.get('/api/friends/blacklist')
    blacklist.value = data.data || []
  } catch (e) {
    app.error('加载黑名单失败：' + (e.response?.data?.error || e.message))
  }
}
async function toggleBlacklist(gid) {
  try {
    await api.post('/api/friend-blacklist/toggle', { gid: String(gid) })
    await loadBlacklist()
  } catch (e) {
    app.error('操作失败：' + (e.response?.data?.error || e.message))
  }
}
async function updateBlacklist(gid, field, val) {
  try {
    const body = { gid: String(gid) }
    body[field] = val
    await api.post('/api/friend-blacklist/update', body)
    await loadBlacklist()
  } catch (e) {
    app.error('更新失败：' + (e.response?.data?.error || e.message))
  }
}

// 护主犬
const dogInfo = ref([])
async function loadDogInfo() {
  try {
    const { data } = await api.get('/api/friends/fetch-dog-info')
    dogInfo.value = data.data || []
  } catch (e) {
    app.error('加载护主犬信息失败：' + (e.response?.data?.error || e.message))
  }
}

// 删除好友
const delSelections = ref({})
const delPassword = ref('')
const delPwdPrompt = ref(false)
async function batchDelete() {
  if (!delPassword.value.trim()) {
    delPwdPrompt.value = true
    return
  }
  const gids = Object.entries(delSelections.value).filter(([, v]) => v).map(([k]) => k)
  if (!gids.length) { app.error('请先选择好友'); return }
  try {
    const { data } = await api.post('/api/friend/batch-delete', { gids: gids.map(Number), password: delPassword.value })
    app.success(data.message || '删除成功')
    delSelections.value = {}
    delPassword.value = ''
    delPwdPrompt.value = false
    await loadFriends(true) // 删好友后强制刷新，避免旧缓存仍显示被删好友
  } catch (e) {
    app.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}

// 添加好友
const applyOpen = ref(false)
const applyGid = ref('')
const applyCode = ref('')
async function applyFriend() {
  try {
    const body = { gid: Number(applyGid.value) }
    if (applyGid.value && applyCode.value) body.shareKey = applyCode.value
    const { data } = await api.post('/api/friend/apply', body)
    app.success(data.message || '申请已发送')
    applyGid.value = ''
    applyCode.value = ''
    applyOpen.value = false
  } catch (e) {
    app.error('申请失败：' + (e.response?.data?.error || e.message))
  }
}
// 自动同意好友申请
const autoAcceptOn = ref(false)
async function autoAcceptFriend() {
  try {
    const { data } = await api.post('/api/friend/apply', { autoAccept: true })
    app.success(data.message || '已检查好友申请')
  } catch (e) {
    app.error('检查申请失败：' + (e.response?.data?.error || e.message))
  }
}
// 手动添加"已知好友 GID"（对齐 Node knownFriendGids：好友列表拉不到时填 GID 让 bot 抓取/巡查）
const knownOpen = ref(false)
const knownGidInput = ref('')
const knownGids = ref([])
async function loadKnownGids() {
  try {
    const { data } = await api.get('/api/friend-known-gids')
    knownGids.value = Array.isArray(data?.data) ? data.data : (Array.isArray(data) ? data : [])
  } catch (e) { /* 忽略 */ }
}
async function addKnownGids() {
  const gids = String(knownGidInput.value || '').split(/[,，\s]+/).map(x => Number(x.trim())).filter(x => x > 0)
  if (!gids.length) { app.error('请输入有效的 GID（多个用逗号分隔）'); return }
  try {
    const { data } = await api.post('/api/friend-known-gids/batch-add', { gids })
    app.success(data.message || `已添加 ${gids.length} 个已知好友 GID`)
    knownGidInput.value = ''
    await loadKnownGids()
    loadFriends(true)
  } catch (e) {
    app.error('添加失败：' + (e.response?.data?.error || e.message))
  }
}
async function removeKnownGid(gid) {
  try {
    await api.post('/api/friend-known-gids/remove', { gid: String(gid) })
    await loadKnownGids()
    loadFriends(true)
  } catch (e) { /* 忽略 */ }
}

// 好友农场详情
const friendLandDialog = ref(false)
const friendLandData = ref(null)
const friendLandGid = ref(0)
async function openFriendLand(f) {
  friendLandGid.value = f.uid
  friendLandDialog.value = true
  try {
    const { data } = await api.get(`/api/friends/lands`, { params: { gid: f.uid } })
    friendLandData.value = data.data || data.data?.list || data.data?.lands || []
  } catch (e) {
    app.error('加载好友农场失败：' + (e.response?.data?.error || e.message))
    friendLandDialog.value = false
  }
}

onMounted(() => loadFriends())
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button v-for="t in tabs" :key="t.key"
        class="tab" :class="{ active: active === t.key }"
        @click="active = t.key; if (active==='visitors') loadVisitors(); else if (active==='blacklist') loadBlacklist(); else if (active==='dog') loadDogInfo(); else if (active==='list') loadFriends()">
        {{ t.label }}
      </button>
    </div>

    <!-- 好友列表 -->
    <div v-if="active === 'list'">
      <div class="sec-head">
        <h2>好友列表 <small>({{ friends.length }}人 · 护主犬 {{ guardDogCount() }}只)</small></h2>
        <div class="sec-actions">
          <button class="btn ghost sm" @click="applyOpen = !applyOpen">➕ 添加好友</button>
          <button class="btn ghost sm" @click="knownOpen = !knownOpen; if (knownOpen) loadKnownGids()">📇 抓取GID</button>
          <button class="btn ghost sm" @click="autoAcceptFriend">自动同意申请</button>
          <button class="btn ghost sm" @click="loadFriends(true)">刷新</button>
        </div>
      </div>
      <div class="apply-form" v-if="applyOpen">
        <input v-model="applyGid" placeholder="好友 UID" class="input" />
        <input v-model="applyCode" placeholder="验证码（可选）" class="input" />
        <button class="btn primary sm" @click="applyFriend">申请添加</button>
      </div>
      <div class="apply-form" v-if="knownOpen">
        <input v-model="knownGidInput" placeholder="好友 GID（多个用逗号分隔）" class="input" style="flex:1" />
        <button class="btn primary sm" @click="addKnownGids">添加抓取</button>
        <span v-if="knownGids.length" class="known-gids">
          <small>已添加：</small>
          <span v-for="g in knownGids" :key="g" class="known-gid-chip">
            {{ g }} <a class="known-gid-del" @click="removeKnownGid(g)">✕</a>
          </span>
        </span>
      </div>
      <div v-if="friendLoading" class="empty-tip">加载中...</div>
      <div v-else-if="!friends.length" class="empty-tip">暂无好友</div>
      <div v-else class="friend-list">
        <div v-for="f in friends" :key="f.uid" class="friend-card">
          <div class="fc-av">{{ f.avatar || '👤' }}</div>
          <div class="fc-info">
            <div class="fc-name" style="cursor:pointer" @click="openFriendLand(f)" title="查看农场">
              {{ f.name || '未命名' }}
              <small class="fc-id">({{ f.uid }})</small>
              <span v-if="isGuardDog(f)" class="guard-badge">🐕 护主犬</span>
            </div>
            <div class="fc-level">Lv.{{ f.level || '-' }}</div>
          </div>
          <div class="fc-ops">
            <button class="btn ghost xs" @click="friendOp(f, 'steal')">🕵️ 偷菜</button>
            <button class="btn ghost xs" @click="friendOp(f, 'help')">🤝 帮忙</button>
            <button class="btn ghost xs" @click="friendOp(f, 'bad')">💣 捣乱</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 访客 -->
    <div v-if="active === 'visitors'">
      <div class="sec-head">
        <h2>访客记录 <small>(共 {{ visitorTotal }} 条)</small></h2>
        <div class="filter-bar">
          <button v-for="k in ['all','steal','help','bad']" :key="k"
            class="tab" :class="{ active: visitorFilter === k }" @click="visitorFilter = k">
            {{ {all:'全部',steal:'偷菜('+visitorStats.steal+')',help:'帮忙('+visitorStats.help+')',bad:'捣乱('+visitorStats.bad+')'}[k] }}
          </button>
        </div>
      </div>
      <div v-if="!filteredVisitors.length" class="empty-tip">暂无访客记录</div>
      <div v-else class="visitor-list">
        <div v-for="(v, i) in filteredVisitors" :key="i" class="visitor-card">
          <div class="fc-av">{{ v.avatarUrl ? '🖼️' : '访' }}</div>
          <div class="fc-info">
            <div class="fc-name">
              {{ v.nick || ('GID:' + v.visitorGid) }}
              <span class="badge-sm">{{ interactBadgeText(v.actionType) }}</span>
              <span v-if="v.level"> Lv.{{ v.level }}</span>
            </div>
            <div class="fc-desc">{{ v.actionDetail || v.actionLabel || '' }}</div>
          </div>
          <div class="v-time">{{ formatTime(v.serverTimeMs || v.timeSec) }}</div>
        </div>
      </div>
    </div>

    <!-- 黑名单 -->
    <div v-if="active === 'blacklist'">
      <div class="sec-head">
        <h2>黑名单 <small>({{ blacklist.length }} 人)</small></h2>
 shedding        <button class="btn ghost sm" @click="loadBlacklist">刷新</button>
      </div>
      <div v-if="!blacklist.length" class="empty-tip">黑名单为空</div>
      <div v-else class="friend-list">
        <div v-for="b in blacklist" :key="b.uid" class="friend-card">
          <div class="fc-av">{{ b.avatar || '👤' }}</div>
          <div class="fc-info">
            <div class="fc-name">{{ b.name || '' }} <small class="fc-id">({{ b.uid }})</small></div>
            <div class="fc-desc">{{ b.reason || '无记录' }} · {{ b.addedAt || '' }}</div>
          </div>
          <div class="fc-ops">
            <label class="f-toggle"><input type="checkbox" :checked="!b.skipSteal"
              @change="updateBlacklist(b.uid, 'skipSteal', !$event.target.checked)" /> 不偷</label>
            <label class="f-toggle"><input type="checkbox" :checked="!b.skipHelp"
              @change="updateBlacklist(b.uid, 'skipHelp', !$event.target.checked)" /> 不帮</label>
            <button class="btn ghost xs" @click="toggleBlacklist(b.uid)">🚫 移出</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 护主犬 -->
    <div v-if="active === 'dog'">
      <div class="sec-head">
        <h2>护主犬信息</h2>
        <button class="btn ghost sm" @click="loadDogInfo">刷新</button>
      </div>
      <div v-if="!dogInfo.length" class="empty-tip">暂无护主犬信息</div>
      <div v-else class="dog-list">
        <div v-for="(d, i) in dogInfo" :key="i" class="friend-card">
          <div class="fc-info">
            <div class="fc-name">GID: {{ d.gid || d.uid || '-' }}</div>
            <div class="fc-desc">狗名: {{ d.dogName || '-' }} · 等级: {{ d.dogLevel || '-' }}</div>
          </div>
        </div>
      </div>
    </div>

    <!-- 删除好友 -->
    <div v-if="active === 'delete'">
      <div class="sec-head">
        <h2>批量删除好友</h2>
        <button class="btn danger sm" @click="batchDelete" :disabled="!Object.values(delSelections).filter(Boolean).length">
          删除选中 ({{ Object.values(delSelections).filter(Boolean).length }})
        </button>
      </div>
      <div v-if="delPwdPrompt" class="delpwd">
        <input v-model="delPassword" type="password" placeholder="输入管理员密码" class="input sm" />
        <button class="btn primary sm" @click="batchDelete">确认删除</button>
      </div>
      <div v-if="!friends.length" class="empty-tip">暂无好友</div>
      <div v-else class="friend-list">
        <div v-for="f in friends" :key="f.uid" class="friend-card">
          <div class="fc-av">{{ f.avatar || '👤' }}</div>
          <div class="fc-info">
            <div class="fc-name">{{ f.name || '未命名' }} <small class="fc-id">({{ f.uid }})</small></div>
          </div>
          <label class="f-toggle">
            <input type="checkbox" v-model="delSelections[f.uid]" /> 选择删除
          </label>
        </div>
      </div>
    </div>
    <!-- 好友农场详情弹窗 -->
    <div class="pwd-dialog glass" v-if="friendLandDialog" style="max-width:600px">
      <div class="sec-head">
        <h2>好友农场 GID:{{ friendLandGid }}</h2>
        <button class="btn ghost sm" @click="friendLandDialog = false">关闭</button>
      </div>
      <div v-if="!friendLandData || !friendLandData.length" class="empty-tip">暂无土地数据</div>
      <div v-else class="land-grid" style="grid-template-columns: repeat(auto-fill, minmax(120px, 1fr))">
        <div v-for="ld in friendLandData" :key="ld.id" class="land-item glass" style="padding:10px;border-radius:var(--radius-md);border:1px solid var(--border)">
          <div style="display:flex;justify-content:space-between;font-size:12px">
            <span>{{ ld.landTypeName || '地块' }}</span>
            <span>Lv.{{ ld.level }}</span>
          </div>
          <div style="font-size:12px;color:var(--muted)" v-if="ld.plantName">{{ ld.plantName }} · {{ ld.phaseName }}</div>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border-radius: var(--radius-lg); }
.tabs { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; }
.tab { padding: 8px 14px; border-radius: 999px; border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); cursor: pointer; font-size: 13px; }
.tab.active { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
.sec-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; flex-wrap: wrap; gap: 8px; }
.sec-head h2 { margin: 0; font-size: 16px; }
.sec-actions { display: flex; gap: 6px; flex-wrap: wrap; }
.apply-form { display: flex; gap: 8px; margin-bottom: 12px; align-items: center; }
.apply-form .input { flex: 1; min-width: 100px; padding: 6px 10px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--foreground); }
.friend-list { display: flex; flex-direction: column; gap: 8px; }
.friend-card { display: flex; align-items: center; gap: 12px; padding: 12px; border-radius: var(--radius-md); background: var(--card-strong); border: 1px solid var(--border); flex-wrap: wrap; }
.fc-av { width: 36px; height: 36px; border-radius: 50%; background: var(--primary-soft); display: grid; place-items: center; font-size: 18px; flex: none; }
.fc-info { flex: 1; min-width: 0; }
.fc-name { font-weight: 600; font-size: 14px; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.fc-id { color: var(--muted); font-size: 12px; }
.fc-level, .fc-desc { font-size: ordon12px; color: var(--muted); }
.fc-ops { display: flex; gap: 4px; flex-wrap: wrap; }
.guard-badge { padding: 2px 8px; border-radius: 999px; background: color-mix(in oklch, var(--good) 25%, transparent); color: var(--good); font-size: 11px; }
.badge-sm { padding: 1px 6px; border-radius: 999px; background: var(--primary-soft); color: var(--primary); font-size: 11px; }
.f-toggle { display: flex; align-items: center; gap: 4px; font-size: 12px; cursor: pointer; }
.visitor-card { display: flex; align-items: center; gap: 12px; padding: 10px 14px; border-radius: var(--radius-md); background: var(--card-strong); border: 1px solid var(--border); }
.v-time { font-size: 11px; color: var(--muted); flex: none; }
.visitor-list, .dog-list { display: flex; flex-direction: column; gap: 6px; }
.filter-bar { display: frex; gap: 4px; flex-wrap: wrap; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 24px 0; text-align: center; }
</style>
