package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 好友申请状态机：待发送 → 发送中 → 已发送 / 失败
const (
	applyPending = "pending"
	applySending = "sending"
	applySent    = "sent"
	applyFailed  = "failed"
)

// applyItem 单条待加好友（按 uid/gid 去重）
type applyItem struct {
	GID      int64  `json:"gid,string"`
	OpenID   string `json:"openid"`
	ShareKey string `json:"shareKey"`
	Status   string `json:"status"`
	Err      string `json:"error,omitempty"`
}

// applyQueue 按账号隔离的串行发送队列 + 单 worker goroutine 消费
type applyQueue struct {
	mu      sync.Mutex
	items   map[int64]*applyItem
	order   []int64
	cancels map[int64]context.CancelFunc // 在途请求的取消函数，cancel 时调用以立即中止
	cond    *sync.Cond
	accID   string
	stop    bool
}

var (
	applyQueuesMu sync.Mutex
	applyQueues   = map[string]*applyQueue{}
)

// getApplyQueue 懒加载并按账号启动唯一 worker（串行消费，永远只占网关 1 个槽，不触发 10s 超时关连接）
func getApplyQueue(accountID string) *applyQueue {
	applyQueuesMu.Lock()
	defer applyQueuesMu.Unlock()
	q, ok := applyQueues[accountID]
	if !ok {
		q = &applyQueue{items: make(map[int64]*applyItem), cancels: make(map[int64]context.CancelFunc), accID: accountID}
		q.cond = sync.NewCond(&q.mu)
		applyQueues[accountID] = q
		go q.worker()
	}
	return q
}

// worker 串行消费：取第一条 pending → 置 sending → ReportArkClick → 置 sent/failed → 间隔 300ms
func (q *applyQueue) worker() {
	for {
		q.mu.Lock()
		if q.stop {
			q.mu.Unlock()
			return
		}
		gid := int64(-1)
		for _, g := range q.order {
			if it, ok := q.items[g]; ok && it.Status == applyPending {
				gid = g
				break
			}
		}
		if gid < 0 {
			q.cond.Wait() // 释放锁等待入队信号；唤醒后重新持锁
			q.mu.Unlock()
			continue
		}
		it := q.items[gid]
		it.Status = applySending
		it.Err = ""
		openID, shareKey := it.OpenID, it.ShareKey
		ctx, cf := context.WithTimeout(context.Background(), 8*time.Second)
		q.cancels[gid] = cf
		q.mu.Unlock()

		c, err := clientPool.Get(q.accID)
		if err != nil {
			cf()
			q.mu.Lock()
			delete(q.cancels, gid)
			gone := false
			if e, ok := q.items[gid]; ok {
				e.Status = applyFailed
				e.Err = "网关未连接: " + err.Error()
			} else {
				gone = true
			}
			q.mu.Unlock()
			if !gone {
				appendOpLog(q.accID, "加好友", fmt.Sprintf("向UID %d 发送好友申请失败：网关未连接", gid))
			}
			time.Sleep(300 * time.Millisecond)
			continue
		}
		serr := c.ReportArkClick(ctx, gid, openID, shareKey)
		cf()
		q.mu.Lock()
		delete(q.cancels, gid)
		gone := false
		if e, ok := q.items[gid]; ok {
			if serr != nil {
				e.Status = applyFailed
				e.Err = serr.Error()
			} else {
				e.Status = applySent
			}
		} else {
			gone = true // 该项已被取消删除，丢弃结果、不写日志
		}
		q.mu.Unlock()
		if !gone {
			if serr != nil {
				appendOpLog(q.accID, "加好友", fmt.Sprintf("向UID %d 发送好友申请失败：%s", gid, serr.Error()))
			} else {
				appendOpLog(q.accID, "加好友", fmt.Sprintf("向UID %d 发送好友申请成功", gid))
			}
		}
		time.Sleep(300 * time.Millisecond) // 频率间隔：避开服务端频限 + 永远单在途，杜绝掉线
	}
}

// enqueue 入队并去重。返回 "queued"(新入队/重试) 或 "skipped"(已发送/发送中/已在队列)
func (q *applyQueue) enqueue(gid int64, openID, shareKey string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	if it, ok := q.items[gid]; ok {
		switch it.Status {
		case applySent, applySending:
			return "skipped"
		case applyPending:
			return "queued"
		}
		// applyFailed → 允许重试
		it.OpenID, it.ShareKey, it.Status, it.Err = openID, shareKey, applyPending, ""
		q.cond.Signal()
		return "queued"
	}
	q.items[gid] = &applyItem{GID: gid, OpenID: openID, ShareKey: shareKey, Status: applyPending}
	q.order = append(q.order, gid)
	q.cond.Signal()
	return "queued"
}

// cancel 立即移除选中项（任何状态），并调用在途请求的 cancel 函数中止发送。
//  worker 对在途被取消的项不写日志、不计入结果，因此点取消后日志立刻停止。
func (q *applyQueue) cancel(gids []string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	set := make(map[int64]bool, len(gids))
	for _, g := range gids {
		gid, e := strconv.ParseInt(g, 10, 64)
		if e != nil {
			continue
		}
		set[gid] = true
	}
	n := 0
	kept := q.order[:0]
	for _, g := range q.order {
		if set[g] {
			if cf, ok := q.cancels[g]; ok {
				cf() // 中止在途 HTTP 请求
				delete(q.cancels, g)
			}
			delete(q.items, g)
			n++
			continue
		}
		kept = append(kept, g)
	}
	q.order = kept
	return n
}

// snapshot 返回当前队列快照（按入队顺序）
func (q *applyQueue) snapshot() []applyItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]applyItem, 0, len(q.order))
	for _, g := range q.order {
		if it, ok := q.items[g]; ok {
			out = append(out, *it)
		}
	}
	return out
}

// POST /api/friend/apply/batch  body: { items: [{gid, openid, shareKey}] }
// 批量入队（按 gid 去重 + 32位 hex 校验），立即返回，不阻塞前端。
func handleFriendApplyBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		Items []struct {
			GID      string `json:"gid"`
			OpenID   string `json:"openid"`
			ShareKey string `json:"shareKey"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	q := getApplyQueue(accountID)
	re := regexp.MustCompile(`^[0-9a-f]{32}$`)
	accepted, skipped, invalid := 0, 0, 0
	for _, it := range req.Items {
		gid, perr := strconv.ParseInt(it.GID, 10, 64)
		openID := strings.TrimSpace(it.OpenID)
		shareKey := strings.ToLower(strings.TrimSpace(it.ShareKey))
		if perr != nil || gid <= 0 || openID == "" || !re.MatchString(shareKey) {
			invalid++
			continue
		}
		if q.enqueue(gid, openID, shareKey) == "queued" {
			accepted++
		} else {
			skipped++
		}
	}
	writeJSON(w, map[string]interface{}{"ok": true, "accepted": accepted, "skipped": skipped, "invalid": invalid, "total": len(req.Items)})
}

// GET /api/friend/apply/status  返回当前账号队列内每条的状态（前端轮询合并到卡片）
func handleFriendApplyStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "method not allowed")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	q := getApplyQueue(accountID)
	writeJSON(w, map[string]interface{}{"ok": true, "items": q.snapshot()})
}

// POST /api/friend/apply/cancel  body: { gids: [...] }  取消仍为 pending 的发送
func handleFriendApplyCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, 405, "method not allowed")
		return
	}
	var req struct {
		GIDs []string `json:"gids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "bad json")
		return
	}
	accountID := resolveAccountID(r.URL.Query().Get("accountId"))
	q := getApplyQueue(accountID)
	n := q.cancel(req.GIDs)
	writeJSON(w, map[string]interface{}{"ok": true, "cancelled": n})
}
