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

function logout() {
  account.logout()
  location.reload()
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
          <button class="btn sm ghost" @click="del(a.id)">删除</button>
        </div>
      </div>
      <div v-if="!accounts.length" class="empty-tip">暂无账号</div>
    </div>

    <div class="sec-title"><span>添加账号</span></div>
    <div class="add-box glass">
      <input class="field" v-model="name" placeholder="备注名（可选）" />
      <input class="field" v-model="code" placeholder="账号 code（必填）" />
      <div class="row">
        <select class="field" v-model="platform">
          <option value="qq">QQ</option>
          <option value="wx">微信/应用宝</option>
        </select>
        <button class="btn primary" :disabled="adding" @click="addAccount">添加</button>
      </div>
      <div class="hint">应用宝扫码登录请在原服务 /api/yyb 流程中完成，这里支持手动添加 code。</div>
    </div>

    <button class="logout" @click="logout">退出登录</button>
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
.field { padding: 10px 12px; border-radius: var(--radius-md); border: 1px solid var(--border); background: var(--card-strong); color: var(--foreground); font-size: 14px; font-family: inherit; }
.row { display: flex; gap: 10px; }
.row .field { flex: 1; }
.hint { font-size: 12px; color: var(--muted); }
.logout { margin-top: 4px; padding: 12px; border-radius: var(--radius-md); border: 1px solid var(--border); background: transparent; color: var(--danger); font-size: 14px; cursor: pointer; }
.empty-tip { color: var(--muted); font-size: 13px; padding: 8px 0; }
</style>
