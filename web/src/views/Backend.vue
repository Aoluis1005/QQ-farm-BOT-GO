<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'

const app = useAppStore()
const tabs = [
  { key: 'status', label: '📊 状态' },
  { key: 'accounts', label: '👤 账号管理' },
  { key: 'logs', label: '📜 日志' },
  { key: 'config', label: '⚙️ 系统配置' },
]
const active = ref('status')

// 状态
const status = ref(null)
async function loadStatus() {
  try {
    const { data } = await api.get('/api/admin/status')
    status.value = data.data || data
  } catch (e) { app.error('加载状态失败：' + (e.response?.data?.error || e.message)) }
}

// 系统配置
const systemConfig = ref(null)
async function loadSystemConfig() {
  try {
    const { data } = await api.get('/api/admin/system-config')
    systemConfig.value = data.data || data
  } catch { /* silent */ }
}
async function saveSystemConfig() {
  try {
    const { data } = await api.post('/api/admin/system-config', { clientVersion: systemConfig.value?.clientVersion || '' })
    app.success(data.message || '保存成功')
  } catch (e) { app.error('保存失败：' + (e.response?.data?.error || e.message)) }
}
async function clearLogs() {
  try {
    await api.delete('/api/logs')
    app.success('日志已清空')
    await loadAdminLogs()
  } catch (e) { app.error('清空失败：' + (e.response?.data?.error || e.message)) }
}

// 账号管理（复用 accounts API）
const accounts = ref([])
async function loadAccounts() {
  try {
    const { data } = await api.get('/api/accounts')
    accounts.value = data.data?.list || data.data || []
  } catch { /* silent */ }
}

// 日志
const adminLogs = ref([])
async function loadAdminLogs() {
  try {
    const { data } = await api.get('/api/logs')
    adminLogs.value = data.data || []
  } catch { /* silent */ }
}

// 修改密码
const pwdDialog = ref(false)
const newPwd = ref('')
const oldPwd = ref('')
const setupPwd = ref('')
const setupDialog = ref(false)
async function doSetup() {
  try {
    await api.post('/api/admin/setup', { password: setupPwd.value })
    app.success('初始化成功')
    setupPwd.value = ''
    setupDialog.value = false
    await loadStatus()
  } catch (e) { app.error('初始化失败：' + (e.response?.data?.error || e.message)) }
}
async function changePwd() {
  try {
    await api.post('/api/admin/change-password', { oldPassword: oldPwd.value, newPassword: newPwd.value })
    app.success('密码已更改')
    newPwd.value = ''
    oldPwd.value = ''
    pwdDialog.value = false
  } catch (e) { app.error('修改失败：' + (e.response?.data?.error || e.message)) }
}

onMounted(() => { loadStatus(); loadAccounts(); loadAdminLogs(); loadSystemConfig() })
</script>

<template>
  <section class="glass panel">
    <div class="tabs">
      <button v-for="t in tabs" :key="t.key"
        class="tab" :class="{ active: active === t.key }"
        @click="active = t.key">{{ t.label }}</button>
      <button class="btn ghost sm" @click="pwdDialog = true">🔑 改密码</button>
      <button class="btn ghost sm" @click="setupDialog = true">⚙️ 初始化</button>
    </div>

    <!-- 修 改密码弹窗 -->
    <div class="pwd-dialog glass" v-if="pwdDialog">
      <h3>修改管理员密码</h3>
      <input v-model="oldPwd" type="password" placeholder="旧密码" class="input" />
      <input v-model="newPwd" type="password" placeholder="新密码" class="input" />
      <div style="display:flex;gap:8px;margin-top:8px">
        <button class="btn primary sm" @click="changePwd">确认</button>
        <button class="btn ghost sm" @click="pwdDialog = false">取消</button>
      </div>
    </div>

    <!-- 初始化弹窗 -->
    <div class="pwd-dialog glass" v-if="setupDialog">
      <h3>管理员初始化</h3>
      <input v-model="setupPwd" type="password" placeholder="设置管理员密码" class="input" />
      <div style="display:flex;gap:8px;margin-top:8px">
        <button class="btn primary sm" @click="doSetup">确认初始化</button>
        <button class="btn ghost sm" @click="setupDialog = false">取消</button>
      </div>
    </div>

    <!-- 状态 -->
    <div v-if="active === 'status'" class="status-panel">
      <div v-if="!status" class="empty-tip">加载中...</div>
      <div v-else class="glass" style="padding:16px">
        <div v-for="(v, k) in status" :key="k" class="rc-item">
          <strong>{{ k }}</strong>: {{ typeof v === 'object' ? JSON.stringify(v) : v }}
        </div>
      </div>
    </div>

    <!-- 账号管理 -->
    <div v-if="active === 'accounts'">
      <div class="list" v-if="accounts.length">
        <div v-for="(a, i) in accounts" :key="i" class="list-item">
          <span>{{ a.name || ('账号 ' + i) }}</span>
          <small>{{ a.uid }}</small>
          <span class="acc-status">{{ a.connected ? '已连接' : '离线' }}</span>
        </div>
      </div>
      <div v-else class="empty-tip">暂无账号</div>
    </div>

    <!-- 日志 -->
    <div v-if="active === 'logs'">
      <button class="btn danger sm" style="margin-bottom:12px" @click="clearLogs">🗑 清空日志</button>
      <div class="log-list" v-if="adminLogs.length">
        <div v-for="(g, i) in adminLogs" :key="i" class="log-item">
          <span class="log-tag">{{ g.tag }}</span>
          <span class="log-msg">{{ g.msg }}</span>
          <span class="log-time">{{ (g.time || '').split(' ')[1] }}</span>
        </div>
      </div>
      <div v-else class="empty-tip">暂无日志</div>
    </div>

    <!-- 系统配置 -->
    <div v-if="active === 'config'" class="config-panel">
      <div v-if="!systemConfig" class="empty-tip">加载中...</div>
      <div v-else class="glass" style="padding:16px">
        <div v-for="(v, k) in systemConfig" :key="k" class="config-item">
          <label>{{ k }}</label>
          <input v-if="typeof v === 'boolean'" type="checkbox" v-model="systemConfig[k]" />
          <input v-else :value="systemConfig[k]" @input="systemConfig[k] = $event.target.value" class="input sm" />
        </div>
        <button class="btn primary sm" style="margin-top:12px" @click="saveSystemConfig">保存配置</button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.panel { padding: 18px; border-radius: var(--radius-lg); }
.tabs { display: flex; gap: 8px; flex-wrap: wrap; margin-bottom: 14px; align-items: center; }
.tab { padding: 86px 14px; border-radius: 999px; border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); cursor: pointer; font-size: 13px; }
.tab.active { background: var(--primary); color: var(--on-primary); border-color: var(--primary); }
.pwd-dialog { padding: 18px; border-radius: var(--radius-lg); max-width: 400px; margin-bottom: 14px; }
.pwd-dialog h3 { margin-top: 0; font-size: 16px; }
.input { width: 100%; padding: 86px 12px; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--foreground); margin-top: 8px; }
.input.sm { width: 150px; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 24px 0; text-align: center; }
.rc-item { padding: 4px 0; font-size: 13px; }
.list { display: flex; flex-direction: column; gap: 6px; }
.list-item { display: flex; gap: 10px; padding: 8px 0; border-bottom68 1px solid var(--border); font-size: 13px; align-items: center; }
.acc-status { font-size: 11px; padding: 2px 8px; border-radius: 999px; }
.log-list { display: flex; flex-direction: column; gap: 6px; max-height: 400px; overflow: auto; }
.log-item { display: flex; gap: 8px; font-size: 13px; }
.log-tag { flex: none; padding: 1px 8px; border-radius: 999px; background: var(--primary-soft); color: var(--primary); font-size: 11px; }
.log-time { flex: none; color: var(--muted); font-size: 11px; }
.config-item { display: flex; gap: 10px; align-items: center; padding: 6px 0; font-size: 13px; border-bottom: 1px solid var(--border); }
</style>
