<script setup>
import { ref, onMounted } from 'vue'
import api from '@/api'
import { useAppStore } from '@/stores/app'
import { useRouter } from 'vue-router'

const app = useAppStore()
const router = useRouter()
const curVer = ref('加载中…')
const verInput = ref('')
const saving = ref(false)

async function load() {
  try {
    const { data } = await api.get('/api/admin/system-config')
    if (data?.ok && data.data) {
      curVer.value = data.data.clientVersion || '-'
      verInput.value = data.data.clientVersion || ''
    }
  } catch (e) {}
}
async function save() {
  const v = verInput.value.trim()
  if (!v) { app.error('请输入客户端版本号'); return }
  if (!confirm(`确认将客户端版本号改为：${v} ？保存后立即使所有已连接账号生效。`)) return
  saving.value = true
  try {
    const { data } = await api.post('/api/admin/system-config', { clientVersion: v })
    if (data?.ok) { curVer.value = v; app.success('已保存并热更新') }
    else app.error(data?.error || '保存失败')
  } catch (e) { app.error(e.response?.data?.error || '保存失败') } finally { saving.value = false }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="subbar"><button class="icon-btn" @click="router.push('/more')">‹</button><h3>设置</h3></div>
    <div style="padding:12px;">
      <div class="sec-title" style="margin:2px 0 12px"><span>系统配置</span></div>
      <div style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:10px;">
          <span style="font-size:12px;color:var(--muted);">当前客户端版本</span>
          <span style="font-size:13px;font-weight:700;color:var(--primary);">{{ curVer }}</span>
        </div>
        <div style="font-size:11px;color:var(--muted);margin-bottom:10px;line-height:1.6;">
          客户端版本号用于网关登录与心跳。保存后对所有已连接账号<strong style="color:#e8a23d;">秒级生效，无需重启</strong>。
          版本与服务器地址通常需成对调整，请勿随意改动，建议先在测试环境验证再同步生产。
        </div>
        <input v-model="verInput" class="field" type="text" placeholder="1.13.2.10_20260723" style="width:100%;box-sizing:border-box;margin-bottom:10px;">
        <button :disabled="saving" style="width:100%;padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="save">{{ saving ? '保存中…' : '保存客户端版本' }}</button>
      </div>
    </div>
  </div>
</template>
