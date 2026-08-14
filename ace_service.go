package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// ACE 反作弊上报服务（对齐 Node services/ace-service.js + utils/network.js startAceService）
//
// 与 Node worker 对齐：ACE 的周期任务不再由独立 goroutine 驱动，而是由【账号唯一的
// 统一串行执行线 automationLoop】通过 tick(now) 调度（等价 Node 中 AceService 的 scheduler
// 与自动化 scheduler 跑在同一个账号 worker 线程的单线程事件循环里）。由此保证同一时刻
// 只有一个 goroutine 访问 TSDK（wasm），消除并发导致的 out of bounds memory access。
//
// 调度周期（对齐 Node）：
//   - process_received_data  每 5s  处理下行数据队列（wasm processReceivedData）
//   - heartbeat_tick         每 25s TSDK 心跳（wasm sendHeartbeatTick）
//   - speed_hack_check       每 30s 速度检测（wasm detectSpeedHack）
//   - ace_poll               每 5s  getDataToServer → AntiData 上报 → sendDataFromServer 回灌
//   - send_status            150s 后一次性状态上报（wasm sendStatus）
//   - function_check         180s 后一次性完整性校验（wasm checkFuncArray）
//
// 失败退避：1s,2s,4s,8s,16s,32s 封顶（对齐 Node MAX_BACKOFF_MS=30000、failures 计数）。
// 随连接断开自动停止：连接关闭回调调用 stop() 并从账号注册表移除。
// ============================================================

const (
	aceProcessIntervalMs   = 5000
	aceHeartbeatIntervalMs = 25000
	acePollIntervalMs      = 5000
	aceSpeedCheckInterval  = 30 * time.Second
	aceStatusDelay         = 150 * time.Second
	aceFunctionCheckDelay  = 180 * time.Second
	aceMaxBackoffMs        = 30000
)

// 账号 → ACE 服务 注册表（登录成功后注册，连接关闭移除），供 automationLoop 取用驱动 tick。
var (
	aceServicesMu sync.Mutex
	aceServices   = map[string]*AceService{}
)

func registerAceService(accountID string, s *AceService) {
	aceServicesMu.Lock()
	aceServices[accountID] = s
	aceServicesMu.Unlock()
}

func getAceService(accountID string) *AceService {
	aceServicesMu.Lock()
	defer aceServicesMu.Unlock()
	return aceServices[accountID]
}

func removeAceService(accountID string) {
	aceServicesMu.Lock()
	delete(aceServices, accountID)
	aceServicesMu.Unlock()
}

// AceService ACE 上报服务（对齐 Node AceService class）
type AceService struct {
	client *gw.Client
	accID  string

	mu        sync.Mutex
	stopped   bool
	inFlight  bool
	failures  int
	lastError string
	uploaded  int

	// 各周期下一执行时刻，由【账号统一串行线】tick(now) 读取并推进
	nextProcess   time.Time
	nextHeartbeat time.Time
	nextSpeed     time.Time
	nextPoll      time.Time
	nextStatus    time.Time
	nextFuncCheck time.Time
	lastSpeedAt   time.Time
	statusDone    bool
	funcDone      bool
}

// startAceService 登录成功后创建 ACE 服务并注册（不再启动独立 goroutine；
// 由 automationLoop 的 tick 驱动，对齐 Node 同一账号单线程语义）。
func startAceService(c *gw.Client, accountID string) *AceService {
	now := time.Now()
	s := &AceService{
		client:        c,
		accID:         accountID,
		nextProcess:   now.Add(aceProcessIntervalMs * time.Millisecond),
		nextHeartbeat: now.Add(aceHeartbeatIntervalMs * time.Millisecond),
		nextSpeed:     now.Add(aceSpeedCheckInterval),
		nextPoll:      now.Add(acePollIntervalMs * time.Millisecond), // 对齐 Node 首 poll 稍后
		nextStatus:    now.Add(aceStatusDelay),
		nextFuncCheck: now.Add(aceFunctionCheckDelay),
		lastSpeedAt:   now,
	}
	registerAceService(accountID, s)
	log.Printf("[ace] 账号 %s ACE 上报服务已启动", accountID)
	return s
}

// stop 停止 ACE 服务（对齐 Node AceService.stop）；由连接关闭回调调用。幂等。
func (s *AceService) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	log.Printf("[ace] 账号 %s ACE 服务已停止", s.accID)
}

// isStopped 服务是否已停止
func (s *AceService) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// tick 由账号统一串行线 automationLoop 每次驱动调用：检查并执行所有到期的周期任务。
// 仅在本 goroutine 内访问 TSDK，保证与业务请求（同线）串行，绝不并发。
func (s *AceService) tick(now time.Time) {
	if s.isStopped() {
		return
	}

	if now.After(s.nextProcess) {
		s.processReceivedData()
		s.nextProcess = now.Add(aceProcessIntervalMs * time.Millisecond)
	}
	if now.After(s.nextHeartbeat) {
		s.heartbeatTick()
		s.nextHeartbeat = now.Add(aceHeartbeatIntervalMs * time.Millisecond)
	}
	if now.After(s.nextSpeed) {
		s.detectSpeedHack(now.Sub(s.lastSpeedAt).Milliseconds())
		s.lastSpeedAt = now
		s.nextSpeed = now.Add(aceSpeedCheckInterval)
	}
	if !s.statusDone && now.After(s.nextStatus) {
		s.sendStatus()
		s.statusDone = true
	}
	if !s.funcDone && now.After(s.nextFuncCheck) {
		s.checkFunctionArray()
		s.funcDone = true
	}
	if now.After(s.nextPoll) {
		s.poll(now)
	}
}

// nearestTick 返回最近一次到期的周期时刻，供 automationLoop 计算睡眠（对齐 scheduleUnifiedNextTick 取 nearest）。
func (s *AceService) nearestTick() time.Time {
	if s.isStopped() {
		return time.Time{} // 已停止：由 automationLoop 判空处理
	}
	n := s.nextProcess
	for _, t := range []time.Time{s.nextHeartbeat, s.nextSpeed, s.nextPoll} {
		if t.Before(n) {
			n = t
		}
	}
	if !s.statusDone && s.nextStatus.Before(n) {
		n = s.nextStatus
	}
	if !s.funcDone && s.nextFuncCheck.Before(n) {
		n = s.nextFuncCheck
	}
	return n
}

// processReceivedData 处理下行数据队列（对齐 Node process_received_data 任务）
func (s *AceService) processReceivedData() {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	if err := s.client.ACEProcessReceivedData(); err != nil {
		s.setLastError(err)
	}
}

// heartbeatTick TSDK 心跳（对齐 Node heartbeat_tick 任务）
func (s *AceService) heartbeatTick() {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	if err := s.client.ACEHeartbeatTick(); err != nil {
		s.setLastError(err)
	}
}

// detectSpeedHack 速度检测（对齐 Node speed_hack_check 任务）
func (s *AceService) detectSpeedHack(elapsedMs int64) {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	if err := s.client.ACEDetectSpeedHack(elapsedMs); err != nil {
		s.setLastError(err)
	}
}

// sendStatus 状态上报（对齐 Node send_status 一次性任务）
func (s *AceService) sendStatus() {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	if err := s.client.ACESendStatus(); err != nil {
		s.setLastError(err)
	}
}

// checkFunctionArray 完整性校验（对齐 Node function_check 一次性任务）
func (s *AceService) checkFunctionArray() {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	names := []string{"processReceivedData", "heartbeatTick", "getDataToServer", "sendDataFromServer"}
	if err := s.client.ACECheckFunctionArray(names, 0); err != nil {
		s.setLastError(err)
	}
}

// poll 拉取待上报数据并发送 AntiData（对齐 Node AceService.poll）。
// 本方法运行在账号统一串行线上；结束后通过 nextPoll 安排下次轮询（含失败退避）。
func (s *AceService) poll(now time.Time) {
	if s.client == nil || s.client.IsClosed() {
		s.nextPoll = now.Add(acePollIntervalMs * time.Millisecond)
		return
	}
	data, err := s.client.ACEGetDataToServer()
	if err != nil {
		s.onPollFailure(now, err)
		return
	}
	if len(data) == 0 {
		s.nextPoll = now.Add(acePollIntervalMs * time.Millisecond)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rep, err := s.client.Request(ctx, "gamepb.acepb.AceService", "AntiData",
		proto.EncodeAntiDataRequest(data), 10*time.Second)
	if err != nil {
		s.onPollFailure(now, err)
		return
	}
	serverData := proto.DecodeAntiDataReply(rep.Body)
	if len(serverData) > 0 {
		_ = s.client.ACESendDataFromServer(serverData)
	}

	s.mu.Lock()
	s.failures = 0
	s.uploaded++
	s.mu.Unlock()
	s.nextPoll = now.Add(acePollIntervalMs * time.Millisecond)
	log.Printf("[ace] 账号 %s AntiData 上报成功：发送 %d 字节，回灌 %d 字节", s.accID, len(data), len(serverData))
}

// onPollFailure 上报失败：退避重排 nextPoll（对齐 Node poll catch 分支）
func (s *AceService) onPollFailure(now time.Time, err error) {
	s.mu.Lock()
	s.failures++
	s.lastError = err.Error()
	f := s.failures
	s.mu.Unlock()
	delay := aceMaxBackoffMs
	if f < 5 {
		delay = int(1000 * (1 << uint(f))) // 1s,2s,4s,8s,16s
	}
	s.nextPoll = now.Add(time.Duration(delay) * time.Millisecond)
	log.Printf("[ace] 账号 %s AntiData 上报失败: %v，%dms 后重试", s.accID, err, delay)
}

// setLastError 记录最近错误
func (s *AceService) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}
