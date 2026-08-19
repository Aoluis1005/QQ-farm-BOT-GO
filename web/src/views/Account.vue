<script setup>
import { ref, reactive, onMounted, onUnmounted } from 'vue'
import api, { setAccountId } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAccountStore } from '@/stores/account'
import { useRouter } from 'vue-router'

const app = useAppStore()
const account = useAccountStore()
const router = useRouter()

const accounts = ref([])
const activeId = ref('')          // 当前 active 账号（来自 GET /api/accounts/active）
const sheet = ref('')             // '' | manage | add | qr
const editing = ref(null)

/* ---------- 加载账号列表 + 当前 active ---------- */
async function loadAccounts() {
  try {
    const { data } = await api.get('/api/accounts')
    accounts.value = (data && data.data) || []
  } catch (e) { accounts.value = [] }
  try {
    const { data } = await api.get('/api/accounts/active')
    const cur = data && data.data && data.data.accountId
    activeId.value = cur ? String(cur) : (accounts.value[0] && String(accounts.value[0].id)) || ''
    if (activeId.value) setAccountId(activeId.value)
  } catch (e) {
    activeId.value = (accounts.value[0] && String(accounts.value[0].id)) || ''
  }
}

/* ---------- 切换当前账号（对齐 legacy switchTo：POST active + 整页刷新） ---------- */
async function switchAcc(id) {
  if (String(id) === String(activeId.value)) return
  try {
    const { data } = await api.post('/api/accounts/active', { id })
    if (data?.ok) {
      setAccountId(String(id))
      location.reload()
    } else app.error(data?.error || '切换失败')
  } catch (e) { app.error(e.response?.data?.error || '切换请求失败') }
}

/* ---------- 手动添加 code ---------- */
const addCode = ref(''); const addName = ref(''); const addPlatform = ref('qq'); const addBusy = ref(false)
const ADD_CHS = [{ v: 'qq', l: 'QQ' }, { v: 'wx', l: '微信' }] // 对齐 legacy：仅 QQ / 微信
async function addByCode() {
  if (!addCode.value.trim()) { app.error('请输入 code'); return }
  addBusy.value = true
  try {
    const { data } = await api.post('/api/accounts', { name: addName.value || '新账号', code: addCode.value.trim(), platform: addPlatform.value })
    if (data?.ok) { app.success(`添加成功（${addPlatform.value}）`); sheet.value = ''; addCode.value = ''; addName.value = ''; location.reload() }
    else app.error('添加失败: ' + (data?.error || '未知'))
  } catch (e) { app.error('请求失败: ' + (e.response?.data?.error || e.message)) } finally { addBusy.value = false }
}

/* ---------- 扫码登录 YYB（对齐 legacy 精确协议） ---------- */
const qrUrl = ref(''); const qrMsg = ref(''); const qrBusy = ref(false)
let qrTimer = null
function stopQr() { if (qrTimer) { clearInterval(qrTimer); qrTimer = null } }
function _qrStatus(text) { qrMsg.value = text }
function _renderQrImg(src) {
  if (!src) return ''
  const s = /^data:/i.test(src) ? src : (/^https?:/i.test(src) ? src : 'data:image/png;base64,' + src)
  return s
}
async function _pollQrOnce(sessionId) {
  try {
    const { data } = await api.post('/api/yyb/qr/poll', { sessionId })
    if (!(data?.ok && data.data)) return { terminal: false, ready: false }
    const st = data.data.status
    if (st === 'authorized' || st === 'confirmed') return { terminal: false, ready: true }
    if (st === 'pending') { _qrStatus('等待手机扫码…'); return { terminal: false, ready: false } }
    if (st === 'scanned') { _qrStatus('已扫描，请在手机上确认…'); return { terminal: false, ready: false } }
    if (st === 'cancelled') return { terminal: true, msg: '已取消扫码' }
    if (st === 'expired') return { terminal: true, msg: '二维码已失效' }
    return { terminal: false, ready: false }
  } catch (e) { return { terminal: false, ready: false } }
}
async function startQrLogin() {
  stopQr(); qrUrl.value = ''; qrMsg.value = '正在获取二维码…'; qrBusy.value = true
  let sessionId = null
  try {
    // 1. 拉二维码
    const { data } = await api.post('/api/yyb/qr/create', {})
    if (!(data?.ok && data?.data)) { _qrStatus('获取二维码失败'); qrBusy.value = false; return }
    const d = data.data
    sessionId = d.session_id
    if (!sessionId) { _qrStatus('后端未返回 session_id'); qrBusy.value = false; return }
    qrUrl.value = _renderQrImg(d.image_base64 || d.image_url || '')
    _qrStatus('请使用手机 QQ / 应用宝扫描')

    // 2. 轮询（顺序请求，上限 3 分钟）
    const deadline = Date.now() + 180000
    let ready = false, terminalMsg = null
    while (Date.now() < deadline) {
      const pr = await _pollQrOnce(sessionId)
      if (pr.terminal) { terminalMsg = pr.msg; break }
      if (pr.ready) { ready = true; break }
      await new Promise(r => setTimeout(r, 2500))
    }
    if (terminalMsg) { _qrStatus(terminalMsg); qrBusy.value = false; return }
    if (!ready) { _qrStatus('登录超时，请重新获取二维码'); qrBusy.value = false; return }

    // 3. confirm → openid
    _qrStatus('手机已确认，正在登录…')
    const cf = (await api.post('/api/yyb/qr/confirm', { sessionId })).data
    const cfa = cf && cf.data
    const openid = cfa && (cfa.openid || cfa.ref)
    if (!openid) { _qrStatus('未获取到 openid'); qrBusy.value = false; return }

    // 4. getcode
    const gc = (await api.post('/api/yyb/getcode', { openid })).data
    const code = gc && gc.data && gc.data.code
    if (!code) { _qrStatus('获取 code 失败'); qrBusy.value = false; return }

    // 5. 添加账号
    const platform = (cfa && cfa.platform) || 'wx'
    const name = (cfa && (cfa.nickname || cfa.alias || cfa.name)) || '新账号'
    const add = (await api.post('/api/accounts', { name, code, platform, openId: openid })).data
    qrBusy.value = false
    if (add.ok) { stopQr(); sheet.value = ''; app.success('扫码登录成功'); location.reload() }
    else { _qrStatus('添加账号失败: ' + (add.error || '未知')) }
  } catch (e) { qrBusy.value = false; _qrStatus('扫码登录失败'); console.error(e) }
}

/* ---------- 掉线自动重连（扫码弹窗内 rc-panel，对齐 legacy） ---------- */
const rcfg = reactive({ enabled: true, delay: 3, max: 3 })
const rcState = ref('连接：-')
function _fmtRc(st) {
  const s = (st && st.state) || '-'
  let t = '连接：' + s
  if (st && st.stopped) t += '（已停止）'
  if (st && st.attempts) t += ' · 已重连' + st.attempts + '次'
  return t
}
async function loadRc() {
  try {
    const { data } = await api.get('/api/reconnect/config')
    const d = data && data.data
    if (!d) return
    rcfg.enabled = !!d.enabled
    if (d.reconnectDelayMin !== undefined) rcfg.delay = d.reconnectDelayMin
    if (d.reconnectMaxAttempts !== undefined) rcfg.max = d.reconnectMaxAttempts
    if (d.state) rcState.value = _fmtRc(d.state)
  } catch (e) {}
}
async function saveRc() {
  try {
    const { data } = await api.post('/api/reconnect/config', { enabled: rcfg.enabled, reconnectDelayMin: Number(rcfg.delay) || 3, reconnectMaxAttempts: Number(rcfg.max) || 3 })
    if (data?.ok) { app.success('设置已保存'); await loadRc() }
    else app.error('保存失败：' + (data?.error || '未知'))
  } catch (e) { app.error('保存失败') }
}
const rcRetryBusy = ref(false)
async function retryRc() {
  rcRetryBusy.value = true
  try {
    const { data } = await api.post('/api/reconnect/retry', {})
    if (data?.ok) { app.success('已触发重连，将在后台执行'); setTimeout(loadRc, 2000) }
    else app.error('触发失败' + (data?.error ? '：' + data.error : ''))
  } catch (e) { app.error('触发失败') } finally { rcRetryBusy.value = false }
}

/* ---------- 编辑 / 删除（对齐 legacy：PUT/DELETE /api/accounts/{id}） ---------- */
async function saveEdit() {
  if (!editing.value) return
  try {
    const { data } = await api.put(`/api/accounts/${encodeURIComponent(editing.value.id)}`, { name: editing.value.name || '', code: editing.value.code || '', platform: '' })
    if (data?.ok) { app.success(data.relinked ? '已保存并刷新登录凭证' : '已保存'); sheet.value = ''; editing.value = null; await loadAccounts() }
    else app.error('保存失败: ' + (data?.error || '?'))
  } catch (e) { app.error(e.response?.data?.error || '保存失败') }
}
async function delAcc(id) {
  if (!confirm('确定删除该账号？此操作不可恢复，删除后需重新添加 code 才能登录。')) return
  try {
    const { data } = await api.delete(`/api/accounts/${encodeURIComponent(id)}`)
    if (data?.ok) { app.success('已删除'); await loadAccounts() }
    else app.error('删除失败: ' + (data?.error || '?'))
  } catch (e) { app.error(e.response?.data?.error || '删除失败') }
}

/* ---------- 退出登录 ---------- */
function logout() {
  // 统一清 admin_token + 内存登录态（account.logout 内部 setToken('') 会 removeItem，并对齐 legacy）
  account.logout()
  router.push('/login')
}

/* ---------- 第三方应用宝登录（与内置 YYB 互不冲突） ---------- */
const t3rdApiBase = ref(''); const t3rdApiToken = ref(''); const t3rdOpenid = ref(''); const t3rdName = ref('')
const t3rdAuto = ref(true); const t3rdDelay = ref(5); const t3rdMax = ref(3)
const t3rdBusy = ref(false); const t3rdErr = ref('')
async function addByThirdpartyYyb() {
  t3rdErr.value = ''
  if (!t3rdApiBase.value.trim() || !t3rdOpenid.value.trim() || !t3rdApiToken.value.trim()) {
    t3rdErr.value = '请填写接口地址、APITOKEN、OPENID'
    return
  }
  t3rdBusy.value = true
  try {
    // 1. 向第三方接口换登录 code
    const tc = (await api.post('/api/yyb/thirdparty-code', {
      apiBase: t3rdApiBase.value.trim(),
      apiToken: t3rdApiToken.value.trim(),
      openid: t3rdOpenid.value.trim(),
    })).data
    if (!(tc?.ok && tc?.data?.code)) {
      t3rdErr.value = tc?.error || '获取登录 code 失败'
      t3rdBusy.value = false
      return
    }
    const code = tc.data.code
    const openid = (tc.data.openid || t3rdOpenid.value).trim()
    const name = t3rdName.value.trim() || `第三方应用宝${openid.slice(-4)}`

    // 2. 添加账号（thirdparty 字段一并持久化，重连由 refreshCodeFromYyb 第三方分支负责）
    const add = (await api.post('/api/accounts', {
      name, code, platform: 'wx', openId: openid,
      thirdparty: {
        apiBase: t3rdApiBase.value.trim(),
        apiToken: t3rdApiToken.value.trim(),
        openid,
        autoReconnect: t3rdAuto.value === true,
        reconnectDelayMin: Math.max(1, Number(t3rdDelay.value) || 5),
        reconnectMaxAttempts: Math.max(1, Number(t3rdMax.value) || 3),
      },
    })).data
    if (!add?.ok) {
      t3rdErr.value = '添加账号失败: ' + (add?.error || '未知')
      t3rdBusy.value = false
      return
    }
    const newId = add.activeAccountId || (Array.isArray(add.data) && add.data.length && add.data[add.data.length - 1].id)
    // 3. 把第三方专属重连配置同步到账号级 AutoReconnect（即使勾选不勾都写，留档可追踪）
    if (newId) {
      try {
        await api.post(`/api/reconnect/config?accountId=${encodeURIComponent(newId)}`, {
          enabled: t3rdAuto.value === true,
          reconnectDelayMin: Math.max(1, Number(t3rdDelay.value) || 5),
          reconnectMaxAttempts: Math.max(1, Number(t3rdMax.value) || 3),
        })
      } catch (_) { /* rc 配置失败不影响账号添加 */ }
    }
    app.success('第三方应用宝登录成功')
    sheet.value = ''
    t3rdApiBase.value = ''; t3rdApiToken.value = ''; t3rdOpenid.value = ''; t3rdName.value = ''
    location.reload()
  } catch (e) {
    t3rdErr.value = e.response?.data?.error || e.message || '第三方登录失败'
  } finally { t3rdBusy.value = false }
}

onMounted(() => loadAccounts())
onUnmounted(() => stopQr())
</script>

<template>
  <div>
    <h3 style="font-size:20px;font-weight:700;margin:2px 2px 0">账号</h3>

    <div class="sec-title"><span>已登录账号</span><span class="link" @click="sheet='manage'">管理</span></div>
    <div class="acc-list">
      <button v-for="a in accounts" :key="a.id" class="acc-row" :class="{ active: String(a.id) === String(activeId) }" @click="switchAcc(a.id)">
        <div class="a-av">🐰</div>
        <div class="a-info"><b>{{ a.name || '未命名' }}</b><span>{{ a.platform || 'qq' }} · {{ ({ online: '在线', offline: '离线' })[a.status] || a.status || '离线' }}</span></div>
        <span class="check">{{ String(a.id) === String(activeId) ? '✓' : '' }}</span>
      </button>
      <p v-if="!accounts.length" style="font-size:12px;color:var(--muted);text-align:center;padding:16px 0">暂无账号</p>
    </div>

    <div class="sec-title"><span>添加账号</span></div>
    <div class="menu">
      <button class="sub-item" @click="sheet='add'"><span class="mi">🔑</span>手动添加 code<span class="arr">›</span></button>
      <button class="sub-item" @click="sheet='qr'; loadRc(); startQrLogin()"><span class="mi">📱</span>扫码登录<span class="arr">›</span></button>
      <button class="sub-item" @click="sheet='thirdparty'; t3rdErr=''"><span class="mi">🔗</span>第三方登录<span class="arr">›</span></button>
    </div>

    <button class="logout" @click="logout">退出登录</button>
  </div>

  <!-- 账号管理 sheet -->
  <div v-if="sheet==='manage'" class="sheet-mask show" @click="sheet=''"></div>
  <div v-if="sheet==='manage'" class="sheet show">
    <div class="handle"></div>
    <h3>⚙️ 账号管理</h3>
    <div style="max-height:60vh;overflow:auto">
      <div v-for="a in accounts" :key="a.id" class="acc-row" :class="{ active: String(a.id) === String(activeId) }" style="flex-wrap:wrap">
        <div class="a-info"><b>{{ a.name }}</b><span>{{ a.platform }} · {{ a.status }}</span></div>
        <div style="display:flex;gap:6px;margin-left:auto">
          <button class="bi-use" @click="switchAcc(a.id)">切换</button>
          <button class="bi-use" @click="editing = { id: a.id, name: a.name, code: a.code || '' }">编辑</button>
          <button class="bi-sell" @click="delAcc(a.id)">删除</button>
        </div>
        <div v-if="editing && editing.id === a.id" style="width:100%;margin-top:8px;display:flex;gap:6px">
          <input v-model="editing.name" class="field" placeholder="备注名" style="flex:1">
          <input v-model="editing.code" class="field" placeholder="code（留空不变）" style="flex:2">
          <button class="bi-use" @click="saveEdit">保存</button>
        </div>
      </div>
      <p v-if="!accounts.length" style="text-align:center;color:var(--muted);padding:20px 0">暂无账号</p>
    </div>
    <button class="close" style="margin-top:16px" @click="sheet=''">关闭</button>
  </div>

  <!-- 手动添加 code sheet -->
  <div v-if="sheet==='add'" class="sheet-mask show" @click="sheet=''"></div>
  <div v-if="sheet==='add'" class="sheet show">
    <div class="handle"></div>
    <h3>🔑 手动添加 code</h3>
    <input v-model="addName" class="field" placeholder="备注名（可选）" style="margin-top:12px">
    <input v-model="addCode" class="field" placeholder="粘贴 code（可从登录页抓取）" style="margin-top:8px">
    <div style="display:flex;gap:8px;margin-top:8px">
      <button v-for="ch in ADD_CHS" :key="ch.v" class="chip" :class="{ on: addPlatform === ch.v }" @click="addPlatform = ch.v">{{ ch.l }}</button>
    </div>
    <button class="close" :disabled="addBusy" style="margin-top:16px" @click="addByCode">{{ addBusy ? '提交中…' : '添加账号' }}</button>
  </div>

  <!-- 扫码登录 sheet -->
  <div v-if="sheet==='qr'" class="sheet-mask show" @click="sheet=''; stopQr()"></div>
  <div v-if="sheet==='qr'" class="sheet show">
    <div class="handle"></div>
    <h3>📱 扫码登录</h3>
    <div style="display:flex;flex-direction:column;align-items:center;padding:16px 0;width:100%;max-height:calc(85dvh - 170px);overflow-y:auto;overflow-x:hidden">
      <img v-if="qrUrl" :src="qrUrl" style="width:150px;height:150px;border-radius:12px;background:#fff;object-fit:contain">
      <p v-else-if="qrBusy" style="color:var(--muted)">获取二维码中…</p>
      <p v-else style="color:var(--muted)">点击下方按钮获取二维码</p>
      <p v-if="qrMsg" style="font-size:12px;color:var(--muted);margin-top:8px;text-align:center">{{ qrMsg }}</p>
    </div>

    <!-- 掉线自动重连（对齐 legacy #qrSheet .rc-panel） -->
    <div class="rc-panel" style="margin-top:14px">
      <div class="rc-head">📡 掉线自动重连 <small>断线后延迟换 code 自动重连</small></div>
      <div class="rc-row">
        <span>开启自动重连</span>
        <div class="switch" :class="{ on: rcfg.enabled }" @click="rcfg.enabled = !rcfg.enabled"></div>
      </div>
      <div class="rc-grid">
        <div class="rc-col">
          <label class="rc-label">离线多久重连</label>
          <div class="sec-field">
            <input type="number" v-model.number="rcfg.delay" class="field" min="1">
            <span class="unit">分钟</span>
          </div>
        </div>
        <div class="rc-col">
          <label class="rc-label">失败几次停止</label>
          <div class="sec-field">
            <input type="number" v-model.number="rcfg.max" class="field" min="1">
            <span class="unit">次</span>
          </div>
        </div>
      </div>
      <div class="rc-foot">
        <span class="rc-state">{{ rcState }}</span>
        <div class="rc-btns">
          <button class="chip" :disabled="rcRetryBusy" @click="retryRc">{{ rcRetryBusy ? '重连中…' : '🔁 立即重连' }}</button>
          <button class="chip" @click="saveRc">💾 保存</button>
        </div>
      </div>
    </div>

    <button class="close" v-if="!qrBusy" style="margin-top:4px" @click="startQrLogin">重新获取二维码</button>
    <button class="close" style="margin-top:8px" @click="sheet=''; stopQr()">关闭</button>
  </div>

  <!-- 第三方应用宝登录 sheet（与内置 YYB 互不冲突；自带账号级独立重连配置） -->
  <div v-if="sheet==='thirdparty'" class="sheet-mask show" @click="sheet=''"></div>
  <div v-if="sheet==='thirdparty'" class="sheet show">
    <div class="handle"></div>
    <h3>🔗 第三方应用宝登录</h3>
    <p style="font-size:12px;color:var(--muted);margin-top:8px;line-height:1.5">
      填入第三方 <b>YYB 接口地址、APITOKEN、OPENID</b> 即可获取登录 code 并添加账号。被踢/异地登录后将按下方设置自动重连（与内置 YYB 互不影响）。
    </p>
    <input v-model="t3rdApiBase" class="field" placeholder="接口地址，例如 http://211.154.25.123:28999" style="margin-top:12px">
    <input v-model="t3rdApiToken" class="field" placeholder="第三方接口 APITOKEN" style="margin-top:8px" type="password" autocomplete="off">
    <input v-model="t3rdOpenid" class="field" placeholder="第三方账号 openid" style="margin-top:8px">
    <input v-model="t3rdName" class="field" placeholder="账号备注（可选，留空则用 openid 后四位）" style="margin-top:8px">

    <div class="rc-panel" style="margin-top:14px">
      <div class="rc-head">📡 离线自动重连<small>账号级独立配置，与内置 YYB 不冲突</small></div>
      <div class="rc-row">
        <span>启用离线自动重连</span>
        <div class="switch" :class="{ on: t3rdAuto }" @click="t3rdAuto = !t3rdAuto"></div>
      </div>
      <div class="rc-grid">
        <div class="rc-col">
          <label class="rc-label">离线几分钟后重连</label>
          <div class="sec-field">
            <input type="number" v-model.number="t3rdDelay" class="field" min="1">
            <span class="unit">分钟</span>
          </div>
        </div>
        <div class="rc-col">
          <label class="rc-label">失败几次后停止</label>
          <div class="sec-field">
            <input type="number" v-model.number="t3rdMax" class="field" min="1">
            <span class="unit">次</span>
          </div>
        </div>
      </div>
    </div>

    <p v-if="t3rdErr" style="font-size:12px;color:var(--danger,#e5484d);margin-top:10px">{{ t3rdErr }}</p>

    <button class="close" :disabled="t3rdBusy" style="margin-top:16px" @click="addByThirdpartyYyb">{{ t3rdBusy ? '添加并登录中…' : '添加并登录' }}</button>
  </div>
</template>

<style scoped>
.bi-use,
.bi-sell {
  border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground);
  border-radius: 6px; font-size: 11px; line-height: 1; font-weight: 700; cursor: pointer; padding: 6px 9px;
}
.bi-use:active, .bi-sell:active { transform: scale(.95); }
.bi-sell { color: var(--danger, #e5484d); border-color: color-mix(in oklch, var(--danger, #e5484d) 40%, transparent); }
</style>
