package main

// 素材增量同步器（本地私有，不进开源仓库）
// 链路（已真实验证）：game-config/cdn-settings.json 提供 CDN server + bundle 版本号
//   → 下载 remote/{bundle}/config.{ver}.json（manifest）
//   → 建 paths 索引
//   → ItemInfo 条目：asset_name → model/v4/{asset_name}_Seed；icon_res → …/spriteFrame
//   → 解析 cc.ImageAsset（含 redirect 链）→ uuid/hash → 拼 native PNG URL
//   → 与 game-config/.assets-sync.json 增量对比（uuid+hash 一致跳过）
//   → 限并发下载 + PNG 校验 → 命名写入 seed_images_named → InitImageMap 热重载

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
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
	syncCDNSettingsFile = "cdn-settings.json" // server + bundles 版本快照（game-config 下）
	syncStateFile       = ".assets-sync.json" // 增量状态（game-config 下）
	syncImagesDir       = "seed_images_named" // 素材目录（game-config 下）
	syncItemInfoFile    = "ItemInfo.json"     // 物品表（game-config 下）
	syncMaxBytes        = 16 * 1024 * 1024    // 单文件上限
	syncConcurrency     = 4                   // 下载/解码并发
	syncTimeout         = 25 * time.Second    // 单请求超时
	syncRetries         = 2                   // 可重试次数
)

var (
	uuidDashRe  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	uuidB64Re   = regexp.MustCompile(`^[A-Za-z0-9+/]{22}$`)
	b64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	// URL 不安全字符(# 会截断 URL fragment)与空格全部替换为 _
	illegalNameRe = regexp.MustCompile(`[/\\:*?"<>|\r\n\t#%&+= '"]`)
	hashRe        = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

// ---------- 配置与状态 ----------

type cdnSettings struct {
	Server  string            `json:"server"`
	Bundles map[string]string `json:"bundles"` // bundle 名 → 版本 hash
}

type assetState struct {
	SyncedAt string                 `json:"syncedAt"`
	Bundles  map[string]string      `json:"bundles"` // 同步时的 bundle 版本
	Items    map[string]assetStItem `json:"items"`   // itemID → 状态
}

type assetStItem struct {
	UUID string `json:"uuid"`
	Hash string `json:"hash"`
	File string `json:"file"`
}

// ---------- manifest ----------

type bundleManifest struct {
	Name           string
	Server         string
	UUIDs          []string
	NativeBase     string
	NativeVersions map[int]string
	ExtensionMap   map[int]string // index → 扩展名（含点）
	Redirect       map[int]int    // index → deps 序号
	Deps           []string
	Types          []string
	Paths          map[int][]string // index → [path, typeIdx]
	uuidLookup     map[string][]int // 归一化 uuid → index 列表
}

type assetCandidate struct {
	Bundle string
	Index  int
	Path   string
	Type   string
}

// ---------- 版本号自动探测 ----------
//
// 官方 CDN 版本号的唯一可靠来源是客户端小程序包内的 src/settings.{hash}.json
// 的 assets.bundleVers(官方构建时写死, version.json 索引长期不更新不可靠)。
// 本机解包后的目录结构: wxgame_out/<日期>/__APP__/src/settings.*.json。
// probeBundleVersions 扫描本地抓包目录, 取最新一份 settings 的 bundleVers。

// syncProbeRoots 候选抓包根目录(按优先级, 存在即用)
// out_dec_app 是本机抓包解密工具每次覆盖写的最新解包目录, 放最前
var syncProbeRoots = []string{
	"C:/Users/18726/AppData/Local/Temp/out_dec_app",
	"wxgame_out",
	"D:/worker/wxgame_out",
	"D:/worker/QQ-farm-BOT-GO/wxgame_out",
}

type probeHit struct {
	Path  string
	Mod   time.Time
	Bunds map[string]string
}

func probeBundleVersions() (map[string]string, string, error) {
	var hits []probeHit
	for _, root := range syncProbeRoots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue // 根目录不存在/无权限, 跳过
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			sub := filepath.Join(root, e.Name())
			// 递归找 __APP__/src/settings.*.json
			_ = filepath.WalkDir(sub, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				base := filepath.Base(p)
				if !strings.HasPrefix(base, "settings.") || !strings.HasSuffix(base, ".json") {
					return nil
				}
				data, rerr := os.ReadFile(p)
				if rerr != nil {
					return nil
				}
				var st struct {
					Assets struct {
						BundleVers map[string]string `json:"bundleVers"`
					} `json:"assets"`
				}
				if json.Unmarshal(data, &st) != nil || len(st.Assets.BundleVers) == 0 {
					return nil
				}
				if fi, ferr := os.Stat(p); ferr == nil {
					hits = append(hits, probeHit{Path: p, Mod: fi.ModTime(), Bunds: st.Assets.BundleVers})
				}
				return nil
			})
		}
	}
	if len(hits) == 0 {
		return nil, "", fmt.Errorf("未找到抓包 settings.json(需先解包最新小程序)")
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Mod.After(hits[j].Mod) })
	best := hits[0]
	return best.Bunds, best.Path, nil
}

// autoUpdateCDNSettings 用最新抓包探测到的 bundleVers 刷新 cdn-settings.json。
// 探测失败或版本无变化时保持现状。
func autoUpdateCDNSettings(dir string) {
	vers, path, err := probeBundleVersions()
	if err != nil {
		log.Printf("[cdn-sync] 版本自动探测跳过: %v", err)
		return
	}
	cur, err := loadCDNSettings(dir)
	if err != nil || cur.Server == "" {
		cur = &cdnSettings{Server: "https://cdn-resource.nqf.qq.com/release/"}
	}
	// 只保留同步用的 8 个 bundle
	want := []string{"mainscene", "extraRes", "aiHead", "audio", "delayRes", "petdog", "plant", "weather"}
	changed := false
	for _, b := range want {
		nv, ok := vers[b]
		if !ok {
			continue
		}
		if cur.Bundles == nil {
			cur.Bundles = map[string]string{}
		}
		if cur.Bundles[b] != nv {
			cur.Bundles[b] = nv
			changed = true
		}
	}
	if !changed {
		log.Printf("[cdn-sync] 版本已是最新(%s)", cur.Bundles["mainscene"])
		return
	}
	out, _ := json.MarshalIndent(cur, "", " ")
	if err := writeAtomic(filepath.Join(dir, syncCDNSettingsFile), out); err != nil {
		log.Printf("[cdn-sync] cdn-settings.json 写入失败: %v", err)
		return
	}
	log.Printf("[cdn-sync] 版本自动更新 mainscene=%s (来源 %s)", cur.Bundles["mainscene"], path)
}

func loadCDNSettings(dir string) (*cdnSettings, error) {
	data, err := os.ReadFile(filepath.Join(dir, syncCDNSettingsFile))
	if err != nil {
		return nil, fmt.Errorf("读取 cdn-settings.json 失败（需游戏更新时同步版本快照）: %w", err)
	}
	var s cdnSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("cdn-settings.json 解析失败: %w", err)
	}
	if s.Server == "" || len(s.Bundles) == 0 {
		return nil, fmt.Errorf("cdn-settings.json 缺少 server 或 bundles")
	}
	if !strings.HasSuffix(s.Server, "/") {
		s.Server += "/"
	}
	return &s, nil
}

func loadItemInfo(dir string) ([]map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(dir, syncItemInfoFile))
	if err != nil {
		return nil, fmt.Errorf("读取 ItemInfo.json 失败: %w", err)
	}
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("ItemInfo.json 解析失败: %w", err)
	}
	return items, nil
}

// ---------- Cocos UUID ----------

func decodeCocosUuid(v string) (string, error) {
	base, suffix := v, ""
	if at := strings.IndexByte(v, '@'); at >= 0 {
		base, suffix = v[:at], v[at:]
	}
	if uuidDashRe.MatchString(base) {
		return strings.ToLower(base) + suffix, nil
	}
	if len(base) != 22 || !uuidB64Re.MatchString(base) {
		return "", fmt.Errorf("无效 Cocos UUID: %q", v)
	}
	var sb strings.Builder
	sb.WriteString(base[:2])
	for i := 2; i < 22; i += 2 {
		l := strings.IndexByte(b64Alphabet, base[i])
		r := strings.IndexByte(b64Alphabet, base[i+1])
		if l < 0 || r < 0 {
			return "", fmt.Errorf("无效 Cocos UUID: %q", v)
		}
		sb.WriteString(fmt.Sprintf("%x", l>>2))
		sb.WriteString(fmt.Sprintf("%x", ((l&3)<<2)|(r>>4)))
		sb.WriteString(fmt.Sprintf("%x", r&15))
	}
	hex := sb.String()
	return hex[:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:] + suffix, nil
}

func normalizeBaseUuid(v string) string {
	base := v
	if at := strings.IndexByte(v, '@'); at >= 0 {
		base = v[:at]
	}
	if uuidDashRe.MatchString(base) || uuidB64Re.MatchString(base) {
		if d, err := decodeCocosUuid(base); err == nil {
			return d
		}
	}
	return base
}

// normalizeCocosUuid 与脚本一致:保留 @suffix,仅做格式归一(用于 uuidLookup key)
func normalizeCocosUuid(v string) string {
	base, suffix := v, ""
	if at := strings.IndexByte(v, '@'); at >= 0 {
		base, suffix = v[:at], v[at:]
	}
	if uuidDashRe.MatchString(base) || uuidB64Re.MatchString(base) {
		if d, err := decodeCocosUuid(base); err == nil {
			return d + suffix
		}
	}
	return v
}

// ---------- manifest 下载与解析 ----------

func fetchJSON(url string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= syncRetries; attempt++ {
		client := &http.Client{Timeout: syncTimeout}
		resp, err := client.Get(url)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, syncMaxBytes))
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
			// 4xx 不重试
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				return nil, lastErr
			}
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

func fetchManifest(server, bundle, ver string) (*bundleManifest, error) {
	raw, err := fetchJSON(fmt.Sprintf("%sremote/%s/config.%s.json", server, bundle, ver))
	if err != nil {
		return nil, err
	}
	var doc struct {
		Name       string                     `json:"name"`
		NativeBase string                     `json:"nativeBase"`
		UUIDs      []string                   `json:"uuids"`
		Types      []string                   `json:"types"`
		Deps       []string                   `json:"deps"`
		Paths      map[string]json.RawMessage `json:"paths"`
		Versions   map[string]json.RawMessage `json:"versions"`
		Redirect   json.RawMessage            `json:"redirect"`
		ExtMap     map[string]json.RawMessage `json:"extensionMap"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("manifest 解析失败: %w", err)
	}
	m := &bundleManifest{
		Name:           doc.Name,
		Server:         server,
		UUIDs:          doc.UUIDs,
		NativeBase:     doc.NativeBase,
		NativeVersions: map[int]string{},
		ExtensionMap:   map[int]string{},
		Redirect:       map[int]int{},
		Deps:           doc.Deps,
		Types:          doc.Types,
		Paths:          map[int][]string{},
		uuidLookup:     map[string][]int{},
	}
	if m.NativeBase == "" {
		m.NativeBase = "native"
	}
	// versions.native: flat [idx, hash, ...]
	if nvRaw, ok := doc.Versions["native"]; ok {
		var flat []json.RawMessage
		if err := json.Unmarshal(nvRaw, &flat); err == nil {
			for i := 0; i+1 < len(flat); i += 2 {
				idx, hash := rawStr(flat[i]), rawStr(flat[i+1])
				if n, err := atoiSafe(idx); err == nil && hashRe.MatchString(hash) {
					m.NativeVersions[n] = hash
				}
			}
		}
	}
	// extensionMap: {".png": [idx...]}
	for ext, idxRaw := range doc.ExtMap {
		var idxs []json.RawMessage
		if json.Unmarshal(idxRaw, &idxs) != nil {
			continue
		}
		for _, r := range idxs {
			if n, err := atoiSafe(rawStr(r)); err == nil {
				if _, ok := m.ExtensionMap[n]; !ok {
					m.ExtensionMap[n] = strings.ToLower(ext)
				}
			}
		}
	}
	// redirect: flat [idx, depOrdinal, ...]
	if len(doc.Redirect) > 0 && string(doc.Redirect) != "null" {
		var flat []json.RawMessage
		if json.Unmarshal(doc.Redirect, &flat) == nil {
			for i := 0; i+1 < len(flat); i += 2 {
				if n, err1 := atoiSafe(rawStr(flat[i])); err1 == nil {
					if o, err2 := atoiSafe(rawStr(flat[i+1])); err2 == nil && o >= 0 && o < len(m.Deps) {
						m.Redirect[n] = o
					}
				}
			}
		}
	}
	// paths: {indexText: [path, typeIdx, ...]}
	for idxText, entryRaw := range doc.Paths {
		var entry []json.RawMessage
		if json.Unmarshal(entryRaw, &entry) != nil || len(entry) < 2 {
			continue
		}
		p := rawStr(entry[0])
		n, err := atoiSafe(idxText)
		if err != nil || p == "" {
			continue
		}
		m.Paths[n] = []string{p, rawStr(entry[1])}
	}
	// uuidLookup
	for i, u := range m.UUIDs {
		key := normalizeCocosUuid(u)
		m.uuidLookup[key] = append(m.uuidLookup[key], i)
	}
	return m, nil
}

func rawStr(r json.RawMessage) string {
	var s string
	if json.Unmarshal(r, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(r, &n) == nil {
		return n.String()
	}
	return ""
}

func atoiSafe(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// ---------- 索引与解析 ----------

type pathIndex struct {
	global   map[string][]assetCandidate
	byBundle map[string]map[string][]assetCandidate
}

func buildPathIndex(manifests []*bundleManifest) *pathIndex {
	pi := &pathIndex{global: map[string][]assetCandidate{}, byBundle: map[string]map[string][]assetCandidate{}}
	for _, m := range manifests {
		one := map[string][]assetCandidate{}
		for idx, entry := range m.Paths {
			t := ""
			if ti, err := atoiSafe(entry[1]); err == nil && ti >= 0 && ti < len(m.Types) {
				t = m.Types[ti]
			}
			c := assetCandidate{Bundle: m.Name, Index: idx, Path: entry[0], Type: t}
			one[entry[0]] = append(one[entry[0]], c)
			pi.global[entry[0]] = append(pi.global[entry[0]], c)
		}
		pi.byBundle[m.Name] = one
	}
	return pi
}

func uniqueCandidate(list []assetCandidate, path string, wantType string) (assetCandidate, error) {
	if len(list) == 0 {
		return assetCandidate{}, fmt.Errorf("资源路径不存在: %s", path)
	}
	if len(list) != 1 {
		return assetCandidate{}, fmt.Errorf("资源路径不唯一 (%d): %s", len(list), path)
	}
	c := list[0]
	if wantType != "" && c.Type != wantType {
		return assetCandidate{}, fmt.Errorf("资源类型错误: %s 期望 %s 实际 %s", path, wantType, c.Type)
	}
	return c, nil
}

func (pi *pathIndex) resolveImageCandidate(manifestPath, mode string, manifests map[string]*bundleManifest) (assetCandidate, error) {
	if mode == "icon" {
		iconPath := manifestPath
		if !strings.HasSuffix(iconPath, "/spriteFrame") {
			iconPath += "/spriteFrame"
		}
		sprite, err := uniqueCandidate(pi.global[iconPath], iconPath, "cc.SpriteFrame")
		if err != nil {
			return assetCandidate{}, err
		}
		rootPath := strings.TrimSuffix(iconPath, "/spriteFrame")
		rootList := pi.byBundle[sprite.Bundle][rootPath]
		root, err := uniqueCandidate(rootList, sprite.Bundle+":"+rootPath, "cc.ImageAsset")
		if err != nil {
			return assetCandidate{}, err
		}
		sm := manifests[sprite.Bundle]
		if normalizeBaseUuid(sm.UUIDs[sprite.Index]) != normalizeBaseUuid(sm.UUIDs[root.Index]) {
			return assetCandidate{}, fmt.Errorf("SpriteFrame 与 ImageAsset UUID 不同组: %s", manifestPath)
		}
		return root, nil
	}
	return uniqueCandidate(pi.global[manifestPath], manifestPath, "cc.ImageAsset")
}

// resolveNative 处理 redirect 链并返回原生资源信息
type nativeRes struct {
	UUID string
	Hash string
	Ext  string
	URL  string
}

func resolveNative(c *assetCandidate, manifests map[string]*bundleManifest) (nativeRes, error) {
	m := manifests[c.Bundle]
	idx := c.Index
	visited := map[string]bool{}
	for depth := 0; depth <= 16; depth++ {
		key := fmt.Sprintf("%s:%d", m.Name, idx)
		if visited[key] {
			return nativeRes{}, fmt.Errorf("redirect 循环: %s", key)
		}
		visited[key] = true
		ord, ok := m.Redirect[idx]
		if !ok {
			break
		}
		if ord >= len(m.Deps) {
			return nativeRes{}, fmt.Errorf("redirect 序号越界")
		}
		target, ok := manifests[m.Deps[ord]]
		if !ok {
			return nativeRes{}, fmt.Errorf("redirect 指向未加载 bundle: %s", m.Deps[ord])
		}
		srcUUID := normalizeCocosUuid(m.UUIDs[idx])
		matches := target.uuidLookup[srcUUID]
		if len(matches) != 1 {
			return nativeRes{}, fmt.Errorf("redirect UUID 在 %s 匹配 %d 项", target.Name, len(matches))
		}
		m = target
		idx = matches[0]
	}
	uuid := normalizeBaseUuid(m.UUIDs[idx])
	hash, ok := m.NativeVersions[idx]
	if !ok || !hashRe.MatchString(hash) {
		return nativeRes{}, fmt.Errorf("缺少 native hash: %s[%d]", m.Name, idx)
	}
	ext := m.ExtensionMap[idx]
	if ext == "" {
		ext = ".png"
	}
	if ext != ".png" {
		return nativeRes{}, fmt.Errorf("原生资源非 PNG (%s): %s[%d]", ext, m.Name, idx)
	}
	nb := strings.Trim(m.NativeBase, "/")
	rel := fmt.Sprintf("remote/%s/%s/%s/%s.%s%s", m.Name, nb, uuid[:2], uuid, hash, ext)
	return nativeRes{UUID: uuid, Hash: hash, Ext: ext, URL: m.Server + rel}, nil
}

// ---------- 下载与写入 ----------

func downloadPNG(url string) ([]byte, error) {
	body, err := fetchJSON(url)
	if err != nil {
		return nil, err
	}
	if len(body) < 8 || !bytes.Equal(body[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}) {
		return nil, fmt.Errorf("非 PNG 签名: %s", url)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(body)); err != nil {
		return nil, fmt.Errorf("PNG 解码失败: %w", err)
	}
	return body, nil
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sanitizeName(s string) string {
	s = illegalNameRe.ReplaceAllString(s, "_")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "item"
	}
	if len([]rune(s)) > 40 {
		r := []rune(s)
		s = string(r[:40])
	}
	return s
}

func assetDisplayName(item map[string]any) string {
	// 优先物品真名 name; effectDesc 是效果描述, 可能含富文本样式标签(<outline color=...> 等), 仅作兜底
	if n, ok := item["name"].(string); ok && strings.TrimSpace(n) != "" {
		return sanitizeName(n)
	}
	if e, ok := item["effectDesc"].(string); ok && strings.TrimSpace(e) != "" {
		return sanitizeName(e)
	}
	return "item"
}

// ---------- 同步报告与任务状态 ----------

type assetReport struct {
	Checked    int               `json:"checked"`
	Added      int               `json:"added"`
	Updated    int               `json:"updated"`
	Skipped    int               `json:"skipped"`
	Failed     int               `json:"failed"`
	Bundles    map[string]string `json:"bundles"`
	Detail     []assetDetail     `json:"detail"`
	StartedAt  time.Time         `json:"startedAt"`
	FinishedAt time.Time         `json:"finishedAt"`
}

type assetDetail struct {
	ItemID int    `json:"itemId"`
	Name   string `json:"name"`
	Status string `json:"status"` // added/updated/skipped/failed
	File   string `json:"file,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type syncTask struct {
	mu       sync.Mutex
	running  bool
	total    int
	done     int
	current  string
	last     *assetReport
	lastErr  string
	finished time.Time
}

var assetsTask = &syncTask{}

// ---------- 主同步流程 ----------

func runAssetsSync(gameConfigDir string) *assetReport {
	report := &assetReport{
		Bundles:   map[string]string{},
		StartedAt: time.Now(),
		Detail:    []assetDetail{},
	}
	log.Printf("[assets-sync] 开始素材增量同步")

	// 同步前自动探测本地最新抓包的 bundle 版本, 刷新 cdn-settings.json
	autoUpdateCDNSettings(gameConfigDir)

	settings, err := loadCDNSettings(gameConfigDir)
	if err != nil {
		report.Failed++
		report.Detail = append(report.Detail, assetDetail{Status: "failed", Reason: err.Error()})
		report.FinishedAt = time.Now()
		return report
	}

	// 1. 下载全部 bundle manifest
	var manifests = map[string]*bundleManifest{}
	var mlist []*bundleManifest
	for name, ver := range settings.Bundles {
		m, err := fetchManifest(settings.Server, name, ver)
		if err != nil {
			log.Printf("[assets-sync] bundle %s 清单失败: %v", name, err)
			continue
		}
		manifests[name] = m
		mlist = append(mlist, m)
		report.Bundles[name] = ver
	}
	if len(manifests) == 0 {
		report.Failed++
		report.Detail = append(report.Detail, assetDetail{Status: "failed", Reason: "所有 bundle 清单下载失败"})
		report.FinishedAt = time.Now()
		return report
	}
	pi := buildPathIndex(mlist)

	// 2. 读取 ItemInfo
	items, err := loadItemInfo(gameConfigDir)
	if err != nil {
		report.Failed++
		report.Detail = append(report.Detail, assetDetail{Status: "failed", Reason: err.Error()})
		report.FinishedAt = time.Now()
		return report
	}

	// 3. 读取状态
	state := assetState{Items: map[string]assetStItem{}}
	if data, err := os.ReadFile(filepath.Join(gameConfigDir, syncStateFile)); err == nil {
		json.Unmarshal(data, &state)
		if state.Items == nil {
			state.Items = map[string]assetStItem{}
		}
	}

	// 4. 逐条解析 native
	// 先按 ID 去重: 同一物品在 ItemInfo 可能有多条, 只保留一张图(asset_name 优先, 其次 icon_res),
	// 避免同一 ID 生成多个文件造成垃圾(如 1001_金币.png 与 1001_金币_golds.png 并存)。
	type job struct {
		itemID int
		name   string
		file   string
		nr     nativeRes
		reason string
	}
	var jobs []job
	var jobsMu sync.Mutex
	var detailMu sync.Mutex

	bestByID := map[int]map[string]any{}
	for _, item := range items {
		id, ok := asInt(item["id"])
		if !ok || id <= 0 {
			continue
		}
		if old, seen := bestByID[id]; seen {
			oldIsAsset := strOr(old["asset_name"]) != ""
			oldIsIcon := strOr(old["icon_res"]) != ""
			curIsAsset := strOr(item["asset_name"]) != ""
			// 新条目 asset 更优, 或旧的既无 asset 也无 icon 而新条目有
			if (curIsAsset && !oldIsAsset) || (!oldIsAsset && !oldIsIcon && strOr(item["icon_res"]) != "") {
				bestByID[id] = item
			}
			continue
		}
		bestByID[id] = item
	}
	report.Checked = len(bestByID)

	// 4.1 逐条解析出 native 信息(解析失败的不进 jobs, 单独记 failed)
	type parsed struct {
		item map[string]any
		id   int
		mode string
		nr   nativeRes
	}
	var parsedList []parsed
	for _, item := range bestByID {
		id, _ := asInt(item["id"])
		var manifestPath, mode string
		if a, ok := item["asset_name"].(string); ok && strings.TrimSpace(a) != "" {
			manifestPath = "model/v4/" + strings.TrimSpace(a) + "_Seed"
			mode = "asset"
		} else if ir, ok := item["icon_res"].(string); ok && strings.TrimSpace(ir) != "" {
			manifestPath = strings.TrimSpace(ir)
			mode = "icon"
		} else {
			report.Failed++
			report.Detail = append(report.Detail, assetDetail{ItemID: id, Name: itemName(item), Status: "failed", Reason: "无 asset_name/icon_res"})
			continue
		}
		c, err := pi.resolveImageCandidate(manifestPath, mode, manifests)
		if err != nil {
			report.Failed++
			report.Detail = append(report.Detail, assetDetail{ItemID: id, Name: itemName(item), Status: "failed", Reason: err.Error()})
			continue
		}
		nr, err := resolveNative(&c, manifests)
		if err != nil {
			report.Failed++
			report.Detail = append(report.Detail, assetDetail{ItemID: id, Name: itemName(item), Status: "failed", Reason: err.Error()})
			continue
		}
		parsedList = append(parsedList, parsed{item: item, id: id, mode: mode, nr: nr})
	}

	// 4.2 按内容(uuid+hash)去重: 同一张图(如种子与果实共用 Crop_883)只保留一个代表物品下载,
	// 其余 ID 复用代表文件, 不再生成重复文件。
	// 代表选取优先级: 有 asset_name 且 ID 为种子(2xxxx) > 解锁卡(3xxxx) > 果实/其它。
	type rep struct {
		item    map[string]any
		id      int
		mode    string
		nr      nativeRes
		sharers []parsed // 复用该文件的其他 ID
	}
	reps := map[string]*rep{} // key = uuid + "|" + hash
	repsOrder := []string{}
	for _, p := range parsedList {
		key := p.nr.UUID + "|" + p.nr.Hash
		r, ok := reps[key]
		if !ok {
			reps[key] = &rep{item: p.item, id: p.id, mode: p.mode, nr: p.nr}
			repsOrder = append(repsOrder, key)
			continue
		}
		// 已有代表, 判断是否应被当前 item 替换(种子优先)
		if shouldRep(p.item, r.item, r.mode) {
			r.sharers = append(r.sharers, parsed{item: r.item, id: r.id, mode: r.mode, nr: r.nr})
			r.item, r.id, r.mode = p.item, p.id, p.mode
		} else {
			r.sharers = append(r.sharers, p)
		}
	}

	for _, key := range repsOrder {
		r := reps[key]
		jobs = append(jobs, job{itemID: r.id, name: itemName(r.item), file: imageFileName(r.item, r.mode), nr: r.nr})
		// 共享同一内容的 ID: 状态里记同 uuid/hash, 指向代表文件
		for _, s := range r.sharers {
			jobsMu.Lock()
			jobs = append(jobs, job{itemID: s.id, name: itemName(s.item), file: imageFileName(r.item, r.mode), nr: s.nr, reason: "shared:" + imageFileName(r.item, r.mode)})
			jobsMu.Unlock()
		}
	}
	assetsTask.mu.Lock()
	assetsTask.total = len(jobs)
	assetsTask.done = 0
	assetsTask.mu.Unlock()

	sem := make(chan struct{}, syncConcurrency)
	var dlWg sync.WaitGroup
	newState := assetState{
		SyncedAt: time.Now().Format(time.RFC3339),
		Bundles:  map[string]string{},
		Items:    map[string]assetStItem{},
	}
	for name, ver := range report.Bundles {
		newState.Bundles[name] = ver
	}

	// 4.3 共享内容的 ID 不再实际下载, 状态直接标记为 skipped(与代表文件同内容)
	for _, j := range jobs {
		if j.reason != "" {
			prev, seen := state.Items[fmt.Sprintf("%d", j.itemID)]
			if seen && prev.UUID == j.nr.UUID && prev.Hash == j.nr.Hash {
				report.Skipped++
				report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: "skipped", File: prev.File})
				newState.Items[fmt.Sprintf("%d", j.itemID)] = prev
				continue
			}
			report.Skipped++
			st := assetStItem{UUID: j.nr.UUID, Hash: j.nr.Hash, File: j.file}
			report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: "skipped", File: j.file, Reason: "与 " + j.file + " 共用同一素材"})
			newState.Items[fmt.Sprintf("%d", j.itemID)] = st
			continue
		}
		prev, seen := state.Items[fmt.Sprintf("%d", j.itemID)]
		if seen && prev.UUID == j.nr.UUID && prev.Hash == j.nr.Hash {
			report.Skipped++
			report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: "skipped", File: prev.File})
			newState.Items[fmt.Sprintf("%d", j.itemID)] = prev
			continue
		}
		dlWg.Add(1)
		go func(j job) {
			defer dlWg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			assetsTask.mu.Lock()
			assetsTask.current = j.name
			assetsTask.mu.Unlock()

			body, err := downloadPNG(j.nr.URL)
			if err != nil {
				detailMu.Lock()
				report.Failed++
				report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: "failed", Reason: err.Error()})
				detailMu.Unlock()
			} else {
				dest := filepath.Join(gameConfigDir, syncImagesDir, j.file)
				if err := writeAtomic(dest, body); err != nil {
					detailMu.Lock()
					report.Failed++
					report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: "failed", Reason: "写入失败: " + err.Error()})
					detailMu.Unlock()
				} else {
					st := assetStItem{UUID: j.nr.UUID, Hash: j.nr.Hash, File: j.file}
					detailMu.Lock()
					if seen {
						report.Updated++
					} else {
						report.Added++
					}
					report.Detail = append(report.Detail, assetDetail{ItemID: j.itemID, Name: j.name, Status: statusWord(seen), File: j.file})
					detailMu.Unlock()
					newState.Items[fmt.Sprintf("%d", j.itemID)] = st
				}
			}

			assetsTask.mu.Lock()
			assetsTask.done++
			assetsTask.mu.Unlock()
		}(j)
	}
	dlWg.Wait()

	// 6. 写状态 + 热重载
	if data, err := json.MarshalIndent(newState, "", "  "); err == nil {
		writeAtomic(filepath.Join(gameConfigDir, syncStateFile), data)
	}
	if report.Added+report.Updated > 0 {
		InitImageMap(gameConfigDir)
	}

	report.FinishedAt = time.Now()
	log.Printf("[assets-sync] 完成: 检查 %d, 新增 %d, 更新 %d, 跳过 %d, 失败 %d",
		report.Checked, report.Added, report.Updated, report.Skipped, report.Failed)
	return report
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case int:
		return t, true
	case string:
		n, err := atoiSafe(t)
		return n, err == nil
	}
	return 0, false
}

func itemName(item map[string]any) string {
	if n, ok := item["name"].(string); ok {
		return n
	}
	return ""
}

func imageFileName(item map[string]any, mode string) string {
	display := assetDisplayName(item)
	id, _ := asInt(item["id"])
	if mode == "asset" {
		if a, ok := item["asset_name"].(string); ok && strings.TrimSpace(a) != "" {
			return fmt.Sprintf("%d_%s_%s_Seed.png", id, display, sanitizeName(strings.TrimSpace(a)))
		}
	}
	return fmt.Sprintf("%d_%s.png", id, display)
}

func strOr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func statusWord(seen bool) string {
	if seen {
		return "updated"
	}
	return "added"
}

// shouldRep 判断 item 是否应取代当前代表 repItem(同一 uuid+hash 组内)。
// 规则: asset 模式优先于 icon; asset 模式下 ID 前缀 2(种子) > 3(解锁卡) > 4/1(果实等)。
func shouldRep(item map[string]any, repItem map[string]any, repMode string) bool {
	curIsAsset := strOr(item["asset_name"]) != ""
	if repMode == "icon" && curIsAsset {
		return true
	}
	if repMode == "asset" && !curIsAsset {
		return false
	}
	curID, _ := asInt(item["id"])
	repID, _ := asInt(repItem["id"])
	return idPriority(curID) < idPriority(repID)
}

// idPriority 物品 ID 优先级别, 越小越优先: 2xxxx 种子 > 3xxxx 解锁卡 > 其它
func idPriority(id int) int {
	switch {
	case id >= 20000 && id < 30000:
		return 0
	case id >= 30000 && id < 40000:
		return 1
	default:
		return 2
	}
}

// ---------- HTTP API ----------

func registerAssetsSyncAPI(api *http.ServeMux) {
	api.HandleFunc("/api/sync/status", handleSyncStatus)
	api.HandleFunc("/api/sync/assets/check", handleAssetsCheck)
	api.HandleFunc("/api/sync/config/check", handleConfigCheck)
}

func handleSyncStatus(w http.ResponseWriter, r *http.Request) {
	assetsTask.mu.Lock()
	defer assetsTask.mu.Unlock()
	writeJSON(w, map[string]any{
		"ok": true,
		"assets": map[string]any{
			"syncing":   assetsTask.running,
			"progress":  assetsTask.progress(),
			"current":   assetsTask.current,
			"last":      assetsTask.last,
			"lastErr":   assetsTask.lastErr,
			"lastSync":  assetsTask.finished,
			"stateFile": syncStateFile,
		},
		"config": configStatus(),
	})
}

func handleAssetsCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	assetsTask.mu.Lock()
	if assetsTask.running {
		assetsTask.mu.Unlock()
		writeError(w, http.StatusConflict, "素材同步已在运行")
		return
	}
	assetsTask.running = true
	assetsTask.done = 0
	assetsTask.total = 0
	assetsTask.current = ""
	assetsTask.lastErr = ""
	assetsTask.mu.Unlock()

	go func() {
		report := runAssetsSync("game-config")
		assetsTask.mu.Lock()
		assetsTask.running = false
		assetsTask.last = report
		assetsTask.finished = time.Now()
		if report.Failed > 0 {
			assetsTask.lastErr = "部分失败"
		}
		assetsTask.mu.Unlock()
	}()

	writeJSON(w, map[string]any{"ok": true, "started": true})
}

func (t *syncTask) progress() map[string]any {
	pct := 0.0
	if t.total > 0 {
		pct = float64(t.done) / float64(t.total) * 100
	}
	return map[string]any{
		"done":  t.done,
		"total": t.total,
		"pct":   int(pct),
	}
}
