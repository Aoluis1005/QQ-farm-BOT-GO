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
// 登录成功后启动（对齐 Node：登录回调里 startAceService()），随连接断开自动停止：
//   - process_received_data  每 5s  处理下行数据队列（wasm processReceivedData）
//   - heartbeat_tick         每 25s TSDK 心跳（wasm sendHeartbeatTick）
//   - speed_hack_check       每 30s 速度检测（wasm detectSpeedHack）
//   - ace_poll               每 5s  getDataToServer → AceService/AntiData 上报 → sendDataFromServer 回灌
//   - send_status            150s 后一次性状态上报（wasm sendStatus）
//   - function_check         180s 后一次性完整性校验（wasm checkFuncArray）
//
// 失败退避：1s,2s,4s,8s,16s,32s 封顶（对齐 Node MAX_BACKOFF_MS=30000、failures 计数）。
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

// AceService ACE 上报服务（对齐 Node AceService class）
type AceService struct {
	client *gw.Client
	accID  string

	mu        sync.Mutex
	stopCh    chan struct{}
	stopped   bool
	stopping  bool
	inFlight  bool
	failures  int
	lastError string
	uploaded  int
}

// startAceService 登录成功后启动 ACE 服务（对齐 Node network.js startAceService）
func startAceService(c *gw.Client, accountID string) *AceService {
	s := &AceService{client: c, accID: accountID, stopCh: make(chan struct{})}
	go s.run()
	return s
}

// stop 停止 ACE 服务（对齐 Node AceService.stop）
func (s *AceService) stop() {
	s.mu.Lock()
	if s.stopping || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopping = true
	close(s.stopCh)
	s.mu.Unlock()
}

// isStopped 服务是否已停止
func (s *AceService) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// run 主调度循环（对齐 Node AceService.start 的多个 scheduler 任务；用单一 goroutine + ticker 等价实现）
func (s *AceService) run() {
	processTicker := time.NewTicker(aceProcessIntervalMs * time.Millisecond)
	hbTicker := time.NewTicker(aceHeartbeatIntervalMs * time.Millisecond)
	speedTicker := time.NewTicker(aceSpeedCheckInterval)
	statusTimer := time.NewTimer(aceStatusDelay)
	funcTimer := time.NewTimer(aceFunctionCheckDelay)
	defer func() {
		processTicker.Stop()
		hbTicker.Stop()
		speedTicker.Stop()
		statusTimer.Stop()
		funcTimer.Stop()
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
	}()

	// 首个 poll 稍等 1s（对齐 Node schedulePoll(DEFAULT_POLL_INTERVAL_MS) 首次延迟）
	pollDelay := acePollIntervalMs * time.Millisecond
	pollTimer := time.NewTimer(pollDelay)

	log.Printf("[ace] 账号 %s ACE 上报服务已启动", s.accID)

	for {
		select {
		case <-s.stopCh:
			log.Printf("[ace] 账号 %s ACE 服务已停止", s.accID)
			return

		case <-processTicker.C:
			s.processReceivedData()

		case <-hbTicker.C:
			s.heartbeatTick()

		case <-speedTicker.C:
			s.detectSpeedHack(aceSpeedCheckInterval.Milliseconds())

		case <-statusTimer.C:
			s.sendStatus()

		case <-funcTimer.C:
			s.checkFunctionArray()

		case <-pollTimer.C:
			s.poll()
			// 下一次 poll 延迟由 poll() 根据失败退避重排
		}
	}
}

// schedulePoll 安排下一次轮询（对齐 Node schedulePoll，含失败退避）
func (s *AceService) schedulePoll(delay time.Duration) {
	if s.isStopped() {
		return
	}
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		s.poll()
	})
}

// processReceivedData 处理下行数据队列（对齐 Node process_received_data 任务）
func (s *AceService) processReceivedData() {
	if s.client == nil || s.client.IsClosed() {
		return
	}
	// 注意：wasm runtime 是共享的，读循环可能也在调用；此处仅透传调用（Node 同样直接调）
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

// poll 拉取待上报数据并发送 AntiData（对齐 Node AceService.poll）
func (s *AceService) poll() {
	s.mu.Lock()
	if s.stopped || s.inFlight || s.client == nil || s.client.IsClosed() {
		s.mu.Unlock()
		s.schedulePoll(acePollIntervalMs * time.Millisecond)
		return
	}
	s.inFlight = true
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.inFlight = false
		s.mu.Unlock()
	}()

	data, err := s.client.ACEGetDataToServer()
	if err != nil {
		s.onPollFailure(err)
		return
	}
	if len(data) == 0 {
		s.schedulePoll(acePollIntervalMs * time.Millisecond)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rep, err := s.client.Request(ctx, "gamepb.acepb.AceService", "AntiData",
		proto.EncodeAntiDataRequest(data), 10*time.Second)
	if err != nil {
		s.onPollFailure(err)
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
	log.Printf("[ace] 账号 %s AntiData 上报成功：发送 %d 字节，回灌 %d 字节", s.accID, len(data), len(serverData))
	s.schedulePoll(acePollIntervalMs * time.Millisecond)
}

// onPollFailure 上报失败：退避重试（对齐 Node poll catch 分支）
func (s *AceService) onPollFailure(err error) {
	s.mu.Lock()
	s.failures++
	s.lastError = err.Error()
	f := s.failures
	s.mu.Unlock()
	delay := aceMaxBackoffMs
	if f < 5 {
		delay = int(1000 * (1 << uint(f))) // 1s,2s,4s,8s,16s
	}
	log.Printf("[ace] 账号 %s AntiData 上报失败: %v，%dms 后重试", s.accID, err, delay)
	s.schedulePoll(time.Duration(delay) * time.Millisecond)
}

// setLastError 记录最近错误
func (s *AceService) setLastError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}
