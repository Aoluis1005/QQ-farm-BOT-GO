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
const notifyOn = ref(false)
const notifyNick = ref('')
const notifyCooldown = ref(10)
const savingNotify = ref(false)
const reportOn = ref(false)
const reportTime = ref('21:00')
const savingReport = ref(false)
const mainTab = ref('version') // version | notify
const subTab = ref('meow')     // meow | bark
const barkOn = ref(false)
const barkKey = ref('')
const savingBark = ref(false)

async function load() {
  try {
    const { data } = await api.get('/api/admin/system-config')
    if (data?.ok && data.data) {
      curVer.value = data.data.clientVersion || '-'
      verInput.value = data.data.clientVersion || ''
      notifyOn.value = !!data.data.offlineNotifyEnabled
      notifyNick.value = data.data.offlineNotifyNick || ''
      notifyCooldown.value = data.data.offlineNotifyCooldownMin || 10
      reportOn.value = !!data.data.dailyReportEnabled
      reportTime.value = data.data.dailyReportTime || '21:00'
      barkOn.value = !!data.data.barkEnabled
      barkKey.value = data.data.barkKey || ''
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
async function saveNotify() {
  const nick = notifyNick.value.trim()
  if (notifyOn.value && !nick) { app.error('请先填写 MeoW 昵称'); return }
  savingNotify.value = true
  try {
    const { data } = await api.post('/api/admin/system-config', {
      offlineNotifyEnabled: notifyOn.value,
      offlineNotifyNick: nick,
      offlineNotifyCooldownMin: Number(notifyCooldown.value) || 10,
    })
    if (data?.ok) app.success('离线通知设置已保存')
    else app.error(data?.error || '保存失败')
  } catch (e) { app.error(e.response?.data?.error || '保存失败') } finally { savingNotify.value = false }
}
async function saveReport() {
  const t = reportTime.value.trim()
  if (reportOn.value && !/^\d{1,2}:\d{2}$/.test(t)) { app.error('请填写推送时间，格式 HH:MM（北京时间）'); return }
  savingReport.value = true
  try {
    const { data } = await api.post('/api/admin/system-config', {
      dailyReportEnabled: reportOn.value,
      dailyReportTime: t || '21:00',
    })
    if (data?.ok) app.success('定时收益推送已保存')
    else app.error(data?.error || '保存失败')
  } catch (e) { app.error(e.response?.data?.error || '保存失败') } finally { savingReport.value = false }
}
async function saveBark() {
  const key = barkKey.value.trim()
  if (barkOn.value && !key) { app.error('请先填写 Bark 设备 key'); return }
  savingBark.value = true
  try {
    const { data } = await api.post('/api/admin/system-config', {
      barkEnabled: barkOn.value,
      barkKey,
      offlineNotifyCooldownMin: Number(notifyCooldown.value) || 10,
    })
    if (data?.ok) app.success('Bark 设置已保存')
    else app.error(data?.error || '保存失败')
  } catch (e) { app.error(e.response?.data?.error || '保存失败') } finally { savingBark.value = false }
}

onMounted(load)
</script>

<template>
  <div>
    <div class="subbar"><button class="icon-btn" @click="router.push('/more')">‹</button><h3>设置</h3></div>
    <div style="padding:12px;">
      <!-- 一级 tab -->
      <div class="seg" style="margin-bottom:12px;">
        <button class="seg-btn" :class="{ active: mainTab === 'version' }" @click="mainTab = 'version'">📱 客户端版本</button>
        <button class="seg-btn" :class="{ active: mainTab === 'notify' }" @click="mainTab = 'notify'">🔔 离线通知</button>
      </div>

      <!-- 客户端版本 -->
      <div v-show="mainTab === 'version'">
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

      <!-- 离线通知 -->
      <div v-show="mainTab === 'notify'">
        <div class="seg" style="margin-bottom:12px;">
          <button class="seg-btn" :class="{ active: subTab === 'meow' }" @click="subTab = 'meow'">🐱 MeoW</button>
          <button class="seg-btn" :class="{ active: subTab === 'bark' }" @click="subTab = 'bark'">🍎 Bark</button>
        </div>

        <!-- MeoW 子页 -->
        <div v-show="subTab === 'meow'">
          <div style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px;">
            <div style="font-size:13px;font-weight:700;margin-bottom:4px;">离线通知（MeoW）</div>
            <div style="font-size:11px;color:var(--muted);margin-bottom:12px;line-height:1.6;">
              账号掉线或自动重连成功时，通过 MeoW 推送到手机 App。同一账号在限流分钟内只推第一条掉线提醒，恢复提醒不受限流。
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
              <span style="font-size:13px;">启用离线通知</span>
              <div class="switch" :class="{ on: notifyOn }" @click="notifyOn = !notifyOn" style="flex:none;"></div>
            </div>
            <input v-model="notifyNick" class="field" type="text" placeholder="MeoW 昵称，如 JohnDoe" :disabled="!notifyOn" style="width:100%;box-sizing:border-box;margin-bottom:10px;">
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px;">
              <input v-model.number="notifyCooldown" class="field" type="number" min="1" placeholder="10" :disabled="!notifyOn" style="width:120px;box-sizing:border-box;flex-shrink:0;">
              <span style="font-size:12px;color:var(--muted);">限流（分钟）</span>
            </div>
            <button :disabled="savingNotify" style="width:100%;padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="saveNotify">{{ savingNotify ? '保存中…' : '保存离线通知' }}</button>
          </div>

          <div style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px;margin-top:12px;">
            <div style="font-size:13px;font-weight:700;margin-bottom:4px;">定时收益推送</div>
            <div style="font-size:11px;color:var(--muted);margin-bottom:12px;line-height:1.6;">
              每天北京时间指定时刻，通过 MeoW 推送一次今日收益汇总（账号金币收益 + 同气礼盒数量）。
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
              <span style="font-size:13px;">启用定时推送</span>
              <div class="switch" :class="{ on: reportOn }" @click="reportOn = !reportOn" style="flex:none;"></div>
            </div>
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px;">
              <input v-model="reportTime" class="field" type="time" :disabled="!reportOn" style="width:140px;box-sizing:border-box;flex-shrink:0;">
              <span style="font-size:12px;color:var(--muted);">北京时间</span>
            </div>
            <button :disabled="savingReport" style="width:100%;padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="saveReport">{{ savingReport ? '保存中…' : '保存定时推送' }}</button>
          </div>
        </div>

        <!-- Bark 子页（与 MeoW 功能对齐） -->
        <div v-show="subTab === 'bark'">
          <div style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px;">
            <div style="font-size:13px;font-weight:700;margin-bottom:4px;">离线通知（Bark）</div>
            <div style="font-size:11px;color:var(--muted);margin-bottom:12px;line-height:1.6;">
              账号掉线或自动重连成功时，通过 Bark 推送到 iPhone。同一账号在限流分钟内只推第一条掉线提醒，恢复提醒不受限流。
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
              <span style="font-size:13px;">启用离线通知</span>
              <div class="switch" :class="{ on: barkOn }" @click="barkOn = !barkOn" style="flex:none;"></div>
            </div>
            <input v-model="barkKey" class="field" type="text" placeholder="Bark 设备 key（App 里复制）" :disabled="!barkOn" style="width:100%;box-sizing:border-box;margin-bottom:10px;">
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px;">
              <input v-model.number="notifyCooldown" class="field" type="number" min="1" placeholder="10" :disabled="!barkOn" style="width:120px;box-sizing:border-box;flex-shrink:0;">
              <span style="font-size:12px;color:var(--muted);">限流（分钟）</span>
            </div>
            <button :disabled="savingBark" style="width:100%;padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="saveBark">{{ savingBark ? '保存中…' : '保存离线通知' }}</button>
          </div>

          <div style="background:var(--card);border:1px solid var(--border);border-radius:14px;padding:14px;margin-top:12px;">
            <div style="font-size:13px;font-weight:700;margin-bottom:4px;">定时收益推送</div>
            <div style="font-size:11px;color:var(--muted);margin-bottom:12px;line-height:1.6;">
              每天北京时间指定时刻，通过 Bark 推送一次今日收益汇总（账号金币收益 + 同气礼盒数量）。同时启用 MeoW 时两个通道都会收到。
            </div>
            <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
              <span style="font-size:13px;">启用定时推送</span>
              <div class="switch" :class="{ on: reportOn }" @click="reportOn = !reportOn" style="flex:none;"></div>
            </div>
            <div style="display:flex;align-items:center;gap:10px;margin-bottom:10px;">
              <input v-model="reportTime" class="field" type="time" :disabled="!reportOn" style="width:140px;box-sizing:border-box;flex-shrink:0;">
              <span style="font-size:12px;color:var(--muted);">北京时间</span>
            </div>
            <button :disabled="savingReport" style="width:100%;padding:12px;border-radius:10px;background:var(--primary,#3b82f6);color:#fff;border:none;font-size:15px;font-weight:700;cursor:pointer;" @click="saveReport">{{ savingReport ? '保存中…' : '保存定时推送' }}</button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
