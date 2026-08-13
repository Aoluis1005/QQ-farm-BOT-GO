<script setup>
import { ref } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'
import { useRouter } from 'vue-router'

const app = useAppStore()
const router = useRouter()
const oldPwd = ref(''); const newPwd = ref(''); const newPwd2 = ref('')
const busy = ref(false)

async function change() {
  if (!oldPwd.value) { app.error('请输入原密码'); return }
  if (!newPwd.value || newPwd.value.length < 6) { app.error('新密码至少 6 位'); return }
  if (newPwd.value !== newPwd2.value) { app.error('两次新密码不一致'); return }
  busy.value = true
  try {
    const { data } = await api.post('/api/admin/change-password', { oldPassword: oldPwd.value, newPassword: newPwd.value })
    if (data?.ok) { app.success('密码修改成功'); oldPwd.value = ''; newPwd.value = ''; newPwd2.value = '' }
    else app.error(data?.error || '修改失败')
  } catch (e) { app.error(e.response?.data?.error || '网络错误') } finally { busy.value = false }
}
</script>

<template>
  <div>
    <div class="subbar"><button class="icon-btn" @click="router.push('/more')">‹</button><h3>后台</h3></div>
    <div style="padding:12px;">
      <div class="sec-title" style="margin:2px 0 12px"><span>修改后台登录密码</span></div>
      <div style="display:flex;flex-direction:column;gap:12px;">
        <input v-model="oldPwd" class="field" type="password" placeholder="原密码" autocomplete="current-password">
        <input v-model="newPwd" class="field" type="password" placeholder="新密码（至少 6 位）" autocomplete="new-password">
        <input v-model="newPwd2" class="field" type="password" placeholder="确认新密码" autocomplete="new-password">
        <button :disabled="busy" style="padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="change">{{ busy ? '提交中…' : '修改密码' }}</button>
      </div>
    </div>
  </div>
</template>
