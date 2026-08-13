<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'
import { useAccountStore } from '@/stores/account'

const app = useAppStore()
const account = useAccountStore()
const accounts = ref([])
const activeId = ref('')
const code = ref('')
const name = ref('')
const platform = ref('qq')
const adding = ref(false)

// 扫码登录状态
const qrDialog = ref(false)
const qrImg = ref('')
const qrStatus = ref('')
const qrPolling = ref(false)
let qrTimer = null

async function load() {
  try {
    const [{ data: list }, { data: act }] = await Promise.all([
      api.get('/api/accounts'),
      api.get('/api/accounts/active'),
    ])
    accounts.value = list.data || []
    activeId.value = act.data?.accountId || ''
  } catch (e) {
    app.error('加载账号列表失败：' + (e.response?.data?.error || e.message))
  }
}

async function switchTo(id) {
  try {
    await api.post('/api/accounts/active', { id })
    account.switchAccount(id)
    activeId.value = id
    app.success('已切换到 ' + (accounts.value.find((a) => a.id === id)?.name || id))
  } catch (e) {
    app.error('切换失败：' + (e.response?.data?.error || e.message))
  }
}

async function addAccount() {
  if (!code.value.trim()) {
    app.warn('请输入账号 code')
    return
  }
  adding.value = true
  try {
    const { data } = await api.post('/api/accounts', {
      name: name.value.trim() || '新账号',
      code: code.value.trim(),
      platform: platform.value,
    })
    app.success('账号已添加')
    code.value = ''
    name.value = ''
    await load()
    if (data.activeAccountId) {
      account.switchAccount(data.activeAccountId)
      activeId.value = data.activeAccountId
    }
  } catch (e) {
    app.error('添加失败：' + (e.response?.data?.error || e.message))
  } finally {
    adding.value = false
  }
}

async function del(id) {
  try {
    await api.delete('/api/accounts/' + encodeURIComponent(id))
    app.success('已删除账号')
    await load()
  } catch (e) {
    app.error('删除失败：' + (e.response?.data?.error || e.message))
  }
}

const editId = ref('')
const editName = ref('')
const editCode = ref('')
const editPlatform = ref('qq')
async function openEdit(a) {
  editId.value = a.id
  editName.value = a.name || a.remark || ''
  editCode.value = a.code || ''
  editPlatform.value = a.platform || 'qq'
}
async function saveEdit() {
  try {
    const { data } = await api.put('/api/accounts/' + encodeURIComponent(editId.value), {
      name: editName.value, code: editCode.value, platform: editPlatform.value,
    })
    app.success(data.message || '已更新')
    await load()
  } catch (e) { app.error('更新失败：' + (e.response?.data?.error || e.message)) }
}

function logout() {
  account.logout()
  location.reload()
}

// ---------- 扫码登录 ----------
function _sleep(ms) { return new Promise((r) => setTimeout(r, ms)) }
function stopQrPoll() {
  qrPolling.value = false
  if (qrTimer) { clearTimeout(qrTimer); qrTimer = null }
}
function renderQrImg(src) {
  if (!src) return
  qrImg.value = /^data:/i.test(src) ? src : (/^https?:/i.test(src) ? src : 'data:image/png;base64,' + src)
}
async function pollQrOnce(sessionId) {
  try {
    const { data } = await api.post('/api/yyb/qr/poll', { sessionId })
    if (!(data.ok && data.data)) return { terminal: false, ready: false }
    const st = data.data.status
    if (st === 'authorized' || st === 'confirmed') return { terminal: false, ready: true }
    if (st === 'pending') { qrStatus.value = '等待手机扫码…'; return { terminal: false, ready: false } }
    if (st === 'scanned') { qrStatus.value = '已扫描，请在手机上确认…'; return { terminal: false, ready: false } }
    if (st === 'cancelled') return { terminal: true, msg: '已取消扫码' }
    if (st === 'expired') return { terminal: true, msg: '二维码已失效' }
    return { terminal: false, ready: false }
  } catch { return { terminal: false, ready: false } }
}
async function startQrLogin() {
  stopQrPoll()
  qrDialog.value = true
  qrImg.value = ''
  qrStatus.value = '正在获取二维码…'
  try {
    // 1. 拉二维码
    const { data } = await api.post('/api/yyb/qr/create', {})
    if (!(data.ok && data.data)) { qrStatus.value = data.error || '获取二维码失败'; return }
    const d = data.data
    const sessionId = d.session_id
    if (!sessionId) { qrStatus.value = '后端未返回 session_id'; return }
    if (d.image_base64) renderQrImg(d.image_base64)
    else if (d.image_url) renderQrImg(d.image_url)
    else qrStatus.value = '二维码已生成，请刷新页面'
    qrStatus.value = '请使用手机 QQ / 应用宝扫描'

    // 2. 轮询（上限 3 分钟，间隔 2.5s，顺序避免并发）
    qrPolling.value = true
    const deadline = Date.now() + 180000
    let ready = false, terminalMsg = null
    while (qrPolling.value && Date.now() < deadline) {
      const pr = await pollQrOnce(sessionId)
      if (pr.terminal) { terminalMsg = pr.msg; break }
      if (pr.ready) { ready = true; break }
      await _sleep(2500)
    }
    if (!qrPolling.value) return // 用户关闭
    if (terminalMsg) { qrStatus.value = terminalMsg; return }
    if (!ready) { qrStatus.value = '登录超时，请重新获取二维码'; return }

    // 3. 确认 → openid
    qrStatus.value = '手机已确认，正在登录…'
    const cfRes = await api.post('/api/yyb/qr/confirm', { sessionId })
    const cfin = cfRes.data
    if (!(cfin.ok && cfin.data)) { qrStatus.value = cfin.error || '登录确认未完成，请重试'; return }
    const cfa = cfin.data
    const openid = cfa && (cfa.openid || cfa.ref)
    if (!openid) { qrStatus.value = '未获取到 openid'; return }

    // 4. openid 换 code
    const gcRes = await api.post('/api/yyb/getcode', { openid })
    const gc = gcRes.data
    if (!(gc.ok && gc.data && gc.data.code)) { qrStatus.value = gc.error || '获取 code 失败，请重试'; return }
    const codeVal = gc.data.code

    // 5. 添加账号
    const platformV = (cfa && cfa.platform) || 'wx'
    const addName = (cfa && (cfa.nickname || cfa.alias || cfa.name)) || '新账号'
    const addRes = await api.post('/api/accounts', { name: addName, code: codeVal, platform: platformV, openId: openid })
    const add = addRes.data
    if (add.ok) {
      stopQrPoll()
      qrDialog.value = false
      app.success('扫码登录成功')
      await load()
      if (add.activeAccountId) {
        account.switchAccount(add.activeAccountId)
        activeId.value = add.activeAccountId
      }
    } else {
      qrStatus.value = add.error || '添加账号失败'
      code.value = codeVal // 失败时保留 code 供手动添加
    }
  } catch (e) {
    qrStatus.value = e.message || '扫码登录失败'
  }
}

onMounted(load)
</script>

<template>
  <div class="dash">
    <h3 class="pg-title">账号</h3>

    <div class="sec-title"><span>已登录账号</span></div>
    <div class="acc-list">
      <div v-for="a in accounts" :key="a.id" class="acc-item glass">
        <div class="acc-meta">
          <div class="acc-name">
            {{ a.remark || a.name || a.id }}
            <span v-if="a.id === activeId" class="tag-on">当前</span>
            <span class="tag-st" :class="a.status === 'online' ? 'on' : 'off'">{{ a.status === 'online' ? '在线' : '离线' }}</span>
          </div>
          <div class="acc-sub">ID: {{ a.id }} · {{ a.platform || 'qq' }}</div>
        </div>
        <div class="acc-ops">
          <button class="btn sm" :disabled="a.id === activeId" @click="switchTo(a.id)">切换</button>
          <button class="btn sm ghost" @click="openEdit(a)">编辑</button>
          <button class="btn sm ghost" @click="del(a.id)">删除</button>
        </div>
      </div>
      <div v-if="!accounts.length" class="empty-tip">暂无账号</div>
    </div>

    <div class="edit-box glass" v-if="editId">
      <h4>编辑账号 {{ editId }}</h4>
      <input class="field" v-model="editName" placeholder="名称" />
      <input class="field" v-model="editCode" placeholder="code" />
      <select class="field" v-model="editPlatform">
        <option value="qq">QQ</option>
        <option value="wx">微信/应用宝</option>
      </select>
      <div class="row">
        <button class="btn primary" @click="saveEdit">保存</button>
        <button class="btn ghost" @click="editId = ''">取消</button>
      </div>
    </div>

    <div class="sec-title"><span>添加账号</span></div>
    <div class="add-box glass">
      <button class="btn primary" style="width:100%" @click="startQrLogin">📱 扫码登录</button>
      <div class="divider"><span>或手动输入 code</span></div>
      <input class="field" v-model="name" placeholder="备注名（可选）" />
      <input class="field" v-model="code" placeholder="账号 code（必填）" />
      <div class="row">
        <select class="field" v-model="platform">
          <option value="qq">QQ</option>
          <option value="wx">微信/应用宝</option>
        </select>
        <button class="btn primary" :disabled="adding" @click="addAccount">添加</button>
      </div>
    </div>

    <button class="logout" @click="logout">退出登录</button>

    <!-- 扫码登录弹窗 -->
    <div v-if="qrDialog" class="qr-mask" @click.self="stopQrPoll(); qrDialog = false">
      <div class="qr-sheet glass">
        <div class="qr-head">
          <h3>扫码登录</h3>
          <button class="qr-close" @click="stopQrPoll(); qrDialog = false">✕</button>
        </div>
        <p class="qr-status">{{ qrStatus }}</p>
        <div class="qr-box">
          <img v-if="qrImg" :src="qrImg" alt="二维码" />
          <div v-else class="qr-loading">{{ qrStatus === '正在获取二维码…' ? '加载中…' : '暂无二维码' }}</div>
        </div>
        <button v-if="qrStatus.includes('失效') || qrStatus.includes('取消') || qrStatus.includes('超时') || qrStatus.includes('失败')"
          class="btn primary sm" style="width:100%;margin-top:12px" @click="startQrLogin">重新获取</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash { display: grid; gap: 14px; }
.pg-title { font-size: 20px; font-weight: 700; margin: 2px 2px 0; }
.sec-title { display: flex; justify-content: space-between; align-items: center; font-size: 14px; color: var(--muted); font-weight: 600; margin-top: 4px; }
.acc-list { display: grid; gap: 10px; }
.acc-item { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 14px; border-radius: var(--radius-md); }
.acc-name { font-size: 15px; font-weight: 600; display: flex; align-items: center; gap: 8px; }
.acc-sub { color: var(--muted); font-size: 12px; margin-top: 2px; }
.acc-ops { display: flex; gap: 8px; flex: none; }
.tag-on { font-size: 11px; padding: 1px 8px; border-radius: 999px; background: var(--primary-soft); color: var(--primary); }
.tag-st { font-size: 11px; padding: 1px 8px; border-radius: 999px; }
.tag-st.on { background: color-mix(in oklch, var(--good) 25%, transparent); color: var(--good); }
.tag-st.off { background: color-mix(in oklch, var(--muted) 25%, transparent); color: var(--muted); }
.add-box { padding: 14px; border-radius: var(--radius-md); display: grid; gap: 10px; }
.edit-box { padding: 14px; border-radius: var(--radius-md); display: grid; gap: 10px; }
.edit-box h4 { margin: 0; font-size: 15px; }
.field { padding: 10px 12px; border-radius: var(--radius-md); border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); font-size: 14px; font-family: inherit; }
.row { display: flex; gap: 10px; }
.row .field { flex: 1; }
.hint { font-size: 12px; color: var(--muted); }
.divider { display: flex; align-items: center; gap: 10px; color: var(--muted); font-size: 12px; }
.divider::before, .divider::after { content: ''; flex: 1; height: 1px; background: var(--border); }
.logout { margin-top: 4px; padding: 12px; border-radius: var(--radius-md); border: 1px solid var(--border); background: transparent; color: var(--danger); font-size: 14px; cursor: pointer; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 8px 0; }
.qr-mask { position: fixed; inset: 0; background: rgba(0,0,0,0.5); display: grid; place-items: center; z-index: 100; backdrop-filter: blur(2px); }
.qr-sheet { width: 320px; max-width: 90vw; padding: 20px; border-radius: var(--radius-lg); text-align: center; }
.qr-head { display: flex; justify-content: space-between; align-items: center; }
.qr-head h3 { margin: 0; font-size: 16px; }
.qr-close { background: none; border: none; font-size: 18px; color: var(--muted); cursor: pointer; }
.qr-status { font-size: 13px; color: var(--muted); margin: 10px 0; }
.qr-box { width: 240px; height: 240px; margin: 0 auto; border-radius: 16px; overflow: hidden; background: var(--card-strong); display: grid; place-items: center; border: 1px solid var(--border); }
.qr-box img { width: 100%; height: 100%; object-fit: contain; }
.qr-loading { color: var(--muted); font-size: 13px; }
</style>
