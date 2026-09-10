package main

// ============================================================
// 配置同步器(2026-09-01 重写: 官方 CDN TextAsset 源)
// 来源: 游戏官方 CDN cdn-resource.nqf.qq.com(与素材同步同源)
//   mainscene bundle 内嵌 81 个 cc.TextAsset 配置资源(官方加密 JSON)。
//   加密算法: Cocos 序列化数组 [5][0][2] 字段为 base64 → 循环 XOR
//   密钥 "NQF_SHANGXIANDAMAI_#2026_SECURE" → 明文 JSON。
// 同步 ItemInfo.json / Plant.json / MutantEffect.json 三个运行时热重载配置表。
// (RoleLevel 为 go:embed 编译期嵌入且极少变化, 不纳入同步。)
// 合并策略(字段级增量、不删本地):
//   - 远端(CDN)独有 id → 追加
//   - 共有 id → 以远端为准覆盖; 但远端缺失字段(如 price/price_id)保留本地值
//   - 本地独有 id → 保留
// 增量判断: .config-sync.json 记录每个文件 sha256, 一致跳过; 变化才下载+合并。
// 写盘后调用 initGameConfig 热重载, 无需重启 bot。
// 与素材同步一致: HTTP 接口立即返回 + goroutine 后台任务, 不阻塞主线程。
// ============================================================

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	configSyncStateFile = ".config-sync.json"
	configSyncMaxNewIDs = 30 // 报告里新增 id 展示上限

	// configXorKey 官方 CDN TextAsset 文本加密密钥(循环 XOR)。
	// 破译自 mainscene bundle TextAsset: 明文 = base64解码(密文) XOR KEY[i % len(KEY)]
	configXorKey = "NQF_SHANGXIANDAMAI_#2026_SECURE"
)

// configSyncFiles 需要同步的配置表: 文件名 -> TextAsset 路径名
// 只同步协议需要的三张表: 物品分类(ItemInfo) / 种植参数(Plant) / 变异效果(MutantEffect)
var configSyncFiles = map[string]string{
	"ItemInfo.json":     "config/ItemInfo",
	"Plant.json":        "config/Plant",
	"MutantEffect.json": "config/MutantEffect",
}

// configSyncState 增量状态文件结构
type configSyncState struct {
	Files    map[string]string `json:"files"` // 文件名 -> sha256
	SyncedAt string            `json:"syncedAt,omitempty"`
	Source   string            `json:"source"`
}

// configTask 后台任务状态(与 assetsTask 对称)
type configTask struct {
	mu       sync.Mutex
	running  bool
	current  string
	lastErr  string
	finished time.Time
	last     *configReport
}

// configReport 一次同步结果
type configReport struct {
	Checked int            `json:"checked"`
	Added   int            `json:"added"`   // 远端独有 id 追加
	Updated int            `json:"updated"` // 共有 id 覆盖
	Kept    int            `json:"kept"`    // 本地独有 id 保留
	Skipped int            `json:"skipped"` // hash 一致跳过
	Failed  int            `json:"failed"`
	Files   []configDetail `json:"files"`
	NewIDs  []int          `json:"newIds,omitempty"`
	Err     string         `json:"error,omitempty"`
}

// configDetail 单个配置表同步明细
type configDetail struct {
	File      string `json:"file"`
	Status    string `json:"status"` // added/updated/skipped/failed
	Size      int    `json:"size"`
	RemoteIDs int    `json:"remoteIds"`
	LocalIDs  int    `json:"localIds"`
	NewIDs    []int  `json:"newIds,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// configTask 全局任务(与 assetsTask 同名冲突检查: 已用 configTask 类型)
var configSyncTask configTask

// loadConfigSyncState 读取增量状态
func loadConfigSyncState(dir string) *configSyncState {
	st := &configSyncState{Files: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(dir, configSyncStateFile))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(data, st)
	if st.Files == nil {
		st.Files = map[string]string{}
	}
	return st
}

// saveConfigSyncState 写增量状态
func saveConfigSyncState(dir string, st *configSyncState) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, configSyncStateFile), data)
}

// sha256Hex 计算文件内容哈希
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// configDecryptText 解密 CDN TextAsset 文本字段:
// Cocos 序列化数组 [ver,0,0,[types],... ,[[0,name,b64text]]] 的 [5][0][2] 是 base64 密文
func configDecryptText(raw []byte) ([]byte, error) {
	var arr []any
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("TextAsset 序列化数组解析失败: %v", err)
	}
	if len(arr) < 6 {
		return nil, fmt.Errorf("TextAsset 结构不完整(len=%d)", len(arr))
	}
	// arr[5] = [[0, name, b64text, ...]]
	obj5, ok := arr[5].([]any)
	if !ok || len(obj5) == 0 {
		return nil, fmt.Errorf("TextAsset 文本字段缺失")
	}
	first, ok := obj5[0].([]any)
	if !ok || len(first) < 3 {
		return nil, fmt.Errorf("TextAsset 文本条目结构异常")
	}
	b64text, ok := first[2].(string)
	if !ok {
		return nil, fmt.Errorf("TextAsset 文本不是字符串")
	}
	cipher, err := base64.StdEncoding.DecodeString(b64text)
	if err != nil {
		return nil, fmt.Errorf("base64 解码失败: %v", err)
	}
	// 循环 XOR 解密
	key := []byte(configXorKey)
	plain := make([]byte, len(cipher))
	for i, b := range cipher {
		plain[i] = b ^ key[i%len(key)]
	}
	return plain, nil
}

// fetchConfigTextAsset 从 mainscene bundle 下载并解密指定 TextAsset, 返回明文 JSON 内容
func fetchConfigTextAsset(server, ver, assetPath string) ([]byte, error) {
	raw, err := fetchJSON(fmt.Sprintf("%sremote/mainscene/config.%s.json", server, ver))
	if err != nil {
		return nil, fmt.Errorf("mainscene manifest 拉取失败: %v", err)
	}
	var doc struct {
		UUIDs      []string                   `json:"uuids"`
		Types      []string                   `json:"types"`
		Paths      map[string]json.RawMessage `json:"paths"`
		Versions   map[string]json.RawMessage `json:"versions"`
		ImportBase string                     `json:"importBase"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("manifest 解析失败: %v", err)
	}
	// 在 paths 里找 assetPath
	idx := -1
	for idxText, entryRaw := range doc.Paths {
		var entry []json.RawMessage
		if json.Unmarshal(entryRaw, &entry) != nil || len(entry) < 2 {
			continue
		}
		if rawStr(entry[0]) == assetPath {
			if n, err := atoiSafe(idxText); err == nil {
				idx = n
				break
			}
		}
	}
	if idx < 0 || idx >= len(doc.UUIDs) {
		return nil, fmt.Errorf("TextAsset %s 不在 mainscene manifest", assetPath)
	}
	// import 表 flat [idx, hash, ...]
	impHash := ""
	if impRaw, ok := doc.Versions["import"]; ok {
		var flat []json.RawMessage
		if json.Unmarshal(impRaw, &flat) == nil {
			for i := 0; i+1 < len(flat); i += 2 {
				if n, err := atoiSafe(rawStr(flat[i])); err == nil && n == idx {
					impHash = strings.SplitN(rawStr(flat[i+1]), "@", 2)[0]
					break
				}
			}
		}
	}
	if impHash == "" {
		return nil, fmt.Errorf("TextAsset %s import hash 未找到(idx=%d)", assetPath, idx)
	}
	uuid, err := decodeCocosUuid(doc.UUIDs[idx])
	if err != nil {
		return nil, fmt.Errorf("uuid 解码失败: %v", err)
	}
	importBase := doc.ImportBase
	if importBase == "" {
		importBase = "import"
	}
	url := fmt.Sprintf("%sremote/mainscene/%s/%s/%s.%s.json", server, importBase, uuid[:2], uuid, impHash)
	assetRaw, err := fetchJSON(url)
	if err != nil {
		return nil, fmt.Errorf("TextAsset 下载失败(%s): %v", assetPath, err)
	}
	return configDecryptText(assetRaw)
}

// richTextRe 匹配官方文案富文本标签: <outline color=#3194CB width=2 > <b> <color=#c6ff00> </b> </outline> 等。
var richTextRe = regexp.MustCompile(`<[^>]*>`)

// stripRichText 去除富文本标签, 保留纯文本。官方 effectDesc 常混入样式标签(如
// "<outline color=#3194CB width=2 ><b>在<color=#c6ff00>雷雨状态的好友家</color>使用可得天气瓶。</b>").
func stripRichText(s string) string {
	s = richTextRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// cleanConfigItem 单条配置清洗: 富文本字段去标签, 空 name 兜底为 id。
func cleanConfigItem(it map[string]any, id int) map[string]any {
	if v, ok := it["name"].(string); ok {
		if v2 := stripRichText(v); v2 != "" {
			it["name"] = v2
		} else {
			it["name"] = fmt.Sprintf("物品%d", id)
		}
	}
	for _, f := range []string{"effectDesc", "desc", "description", "tips"} {
		if v, ok := it[f].(string); ok {
			it[f] = stripRichText(v)
		}
	}
	return it
}

// configMergeJSON 字段级合并: 以远端(CDN)为主, 远端缺失字段保留本地值。
// 筛选规则: 丢弃无有效 id 的垃圾条目; 富文本字段统一去标签清洗。
// 同步的三张表(ItemInfo/Plant/MutantEffect)均带 id 字段。
// 返回 (合并后 JSON 字节, 新增id数, 更新id数, 保留id数)
func configMergeJSON(remoteJSON, localJSON []byte) ([]byte, int, int, int, error) {
	var remote []map[string]any
	var local []map[string]any
	if err := json.Unmarshal(remoteJSON, &remote); err != nil {
		return nil, 0, 0, 0, fmt.Errorf("远端配置解析失败: %v", err)
	}
	_ = json.Unmarshal(localJSON, &local) // 本地缺失时 local 为空

	// 同步的三张表均带 id 字段, 无需无 id 特判。

	// id -> 本地条目
	localByID := map[int]map[string]any{}
	for _, it := range local {
		if id, ok := asInt(it["id"]); ok {
			localByID[id] = it
		}
	}

	added, updated, kept := 0, 0, 0
	merged := make([]map[string]any, 0, len(remote)+len(local))
	seen := map[int]bool{}
	for _, r := range remote {
		id, ok := asInt(r["id"])
		if !ok || id <= 0 {
			continue // 筛选: 丢弃无有效 id 的垃圾条目
		}
		seen[id] = true
		if lb, ok := localByID[id]; ok {
			// 共有: 以远端为主, 补本地独有字段
			for k, v := range lb {
				if _, exists := r[k]; !exists {
					r[k] = v
				}
			}
			updated++
		} else {
			added++
		}
		merged = append(merged, cleanConfigItem(r, id))
	}
	// 本地独有 id 保留
	for _, l := range local {
		id, ok := asInt(l["id"])
		if !ok || id <= 0 || seen[id] {
			continue
		}
		merged = append(merged, cleanConfigItem(l, id))
		kept++
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return out, added, updated, kept, nil
}

// runConfigSync 执行一次配置同步(与 runAssetsSync 对称)
func runConfigSync(gameConfigDir string) *configReport {
	rep := &configReport{Files: []configDetail{}}
	// 同步前自动探测本地最新抓包的 bundle 版本, 刷新 cdn-settings.json
	autoUpdateCDNSettings(gameConfigDir)
	settings, err := loadCDNSettings(gameConfigDir)
	if err != nil {
		rep.Err = err.Error()
		rep.Failed = len(configSyncFiles)
		return rep
	}
	ver, ok := settings.Bundles["mainscene"]
	if !ok || ver == "" {
		rep.Err = "cdn-settings.json 缺少 mainscene 版本"
		rep.Failed = len(configSyncFiles)
		return rep
	}
	server := settings.Server

	// 文件名顺序固定
	names := make([]string, 0, len(configSyncFiles))
	for n := range configSyncFiles {
		names = append(names, n)
	}
	sort.Strings(names)

	st := loadConfigSyncState(gameConfigDir)
	manifest, err := fetchManifest(server, "mainscene", ver)
	if err == nil {
		rep.Checked = len(names)
		_ = manifest
	}

	for _, name := range names {
		configSyncTask.mu.Lock()
		configSyncTask.current = name
		configSyncTask.mu.Unlock()

		remoteJSON, err := fetchConfigTextAsset(server, ver, configSyncFiles[name])
		if err != nil {
			rep.Failed++
			rep.Files = append(rep.Files, configDetail{File: name, Status: "failed", Reason: err.Error()})
			continue
		}
		// 增量: 与本地缓存 hash 对比
		h := sha256Hex(remoteJSON)
		if st.Files[name] == h {
			rep.Skipped++
			rep.Files = append(rep.Files, configDetail{File: name, Status: "skipped", Size: len(remoteJSON)})
			continue
		}
		// 读取本地当前文件做字段级合并
		localPath := filepath.Join(gameConfigDir, name)
		var localJSON []byte
		if lr, err := os.ReadFile(localPath); err == nil {
			localJSON = lr
		}
		merged, added, updated, kept, err := configMergeJSON(remoteJSON, localJSON)
		if err != nil {
			rep.Failed++
			rep.Files = append(rep.Files, configDetail{File: name, Status: "failed", Reason: err.Error()})
			continue
		}
		if err := writeAtomic(localPath, merged); err != nil {
			rep.Failed++
			rep.Files = append(rep.Files, configDetail{File: name, Status: "failed", Reason: err.Error()})
			continue
		}
		st.Files[name] = h
		rep.Added += added
		rep.Updated += updated
		rep.Kept += kept
		d := configDetail{File: name, Status: "updated", Size: len(merged)}
		if added > 0 || updated > 0 {
			d.NewIDs = extractNewIDs(remoteJSON, localJSON, configSyncMaxNewIDs)
			rep.NewIDs = append(rep.NewIDs, d.NewIDs...)
		}
		rep.Files = append(rep.Files, d)
	}

	st.SyncedAt = time.Now().Format("2006-01-02 15:04:05")
	st.Source = "cdnsync"
	if err := saveConfigSyncState(gameConfigDir, st); err != nil {
		log.Printf("[config-sync] 状态文件写入失败: %v", err)
	}

	// 有变化才热重载
	if rep.Added+rep.Updated > 0 {
		initGameConfig(gameConfigDir)
		log.Printf("[config-sync] 配置已热重载: +%d 新 id, 更新 %d 条", rep.Added, rep.Updated)
	}

	configSyncTask.mu.Lock()
	configSyncTask.last = rep
	configSyncTask.finished = time.Now()
	configSyncTask.current = ""
	configSyncTask.mu.Unlock()
	return rep
}

// extractNewIDs 提取远端独有 id(限制数量)
func extractNewIDs(remoteJSON, localJSON []byte, limit int) []int {
	var remote, local []map[string]any
	_ = json.Unmarshal(remoteJSON, &remote)
	_ = json.Unmarshal(localJSON, &local)
	localSet := map[int]bool{}
	for _, it := range local {
		if id, ok := asInt(it["id"]); ok {
			localSet[id] = true
		}
	}
	var out []int
	for _, it := range remote {
		id, ok := asInt(it["id"])
		if ok && !localSet[id] {
			out = append(out, id)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// handleConfigCheck POST /api/sync/config/check 触发后台配置同步
func handleConfigCheck(w http.ResponseWriter, r *http.Request) {
	configSyncTask.mu.Lock()
	if configSyncTask.running {
		configSyncTask.mu.Unlock()
		writeJSON(w, map[string]any{"ok": false, "started": false, "error": "配置同步正在进行中"})
		return
	}
	configSyncTask.running = true
	configSyncTask.lastErr = ""
	configSyncTask.mu.Unlock()

	dir := "game-config"
	go func() {
		rep := runConfigSync(dir)
		configSyncTask.mu.Lock()
		configSyncTask.running = false
		if rep.Err != "" {
			configSyncTask.lastErr = rep.Err
		}
		configSyncTask.mu.Unlock()
	}()
	writeJSON(w, map[string]any{"ok": true, "started": true})
}

// configStatus 返回配置同步状态
func configStatus() map[string]any {
	configSyncTask.mu.Lock()
	defer configSyncTask.mu.Unlock()
	out := map[string]any{
		"running": configSyncTask.running,
		"current": configSyncTask.current,
	}
	if configSyncTask.lastErr != "" {
		out["error"] = configSyncTask.lastErr
	}
	if !configSyncTask.finished.IsZero() {
		out["lastAt"] = configSyncTask.finished.Format("2006-01-02 15:04:05")
	}
	if configSyncTask.last != nil {
		out["last"] = configSyncTask.last
	}
	return out
}
