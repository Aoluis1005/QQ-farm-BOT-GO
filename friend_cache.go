package main

import (
	"sync"
	"time"

	"github.com/Aoluis1005/go-farm-bot/gw"
	"github.com/Aoluis1005/go-farm-bot/proto"
)

// ============================================================
// 好友列表展示缓存（对齐 liyangpengs friendsListCache + TTL）。
// 仅用于「展示」接口（/api/friends/list）；巡查 checkFriends 仍走实时 fetchAllFriends（选人需最新）。
// 缓存的是 fetchAllFriends 的原始好友列表，组装/过滤在请求时实时做，
// 因此黑名单、护主犬缓存、myGID 等变化仍能正确反映，无需主动失效（对齐对方：靠 TTL + forceSync）。
// ============================================================

// friendsListCacheTtlSec 好友列表展示缓存 TTL（秒）。写死默认 60s，对齐对方 friendsListCacheTtlSec 默认值。
const friendsListCacheTtlSec = 60

type friendsListCacheItem struct {
	friends []*proto.GameFriend
	exp     int64 // Unix 秒
}

var (
	friendsListCacheMu   sync.Mutex
	friendsListCacheData = map[string]*friendsListCacheItem{}
)

// getAllFriendsCached 获取好友列表（展示用途，带 TTL 缓存）。
// forceSync=true 时强制绕过缓存拉最新并刷新缓存。
// 返回的切片为共享只读数据，调用方不得修改。
func getAllFriendsCached(c *gw.Client, accountID, platform string, knownGids []int64, forceSync bool) ([]*proto.GameFriend, error) {
	now := time.Now().Unix()
	if !forceSync {
		friendsListCacheMu.Lock()
		it, ok := friendsListCacheData[accountID]
		if ok && it.exp > now {
			friendsListCacheMu.Unlock()
			return it.friends, nil
		}
		friendsListCacheMu.Unlock()
	}
	friends, err := fetchAllFriends(c, platform, knownGids)
	if err != nil {
		// 拉取失败：不清缓存（保留旧数据兜底，让请求方能继续看到上次的列表）
		return nil, err
	}
	friendsListCacheMu.Lock()
	friendsListCacheData[accountID] = &friendsListCacheItem{friends: friends, exp: now + friendsListCacheTtlSec}
	friendsListCacheMu.Unlock()
	return friends, nil
}

// clearFriendsListCache 清空指定账号的好友列表展示缓存。
func clearFriendsListCache(accountID string) {
	friendsListCacheMu.Lock()
	delete(friendsListCacheData, accountID)
	friendsListCacheMu.Unlock()
}
