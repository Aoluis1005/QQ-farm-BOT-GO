package models

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Aoluis1005/go-farm-bot/config"
)

// ============================================================
// 对应 Node models/store.js + services/json-db.js
// 全局配置持久化（原子 JSON 写入）
// ============================================================

var (
	globalConfig config.GlobalConfig
	accounts     []Account
	mu           sync.RWMutex
	dataDir      string
)

// Account 账号数据结构（对标 Node store.js addOrUpdateAccount）
type Account struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Code      string `json:"code"`
	Platform  string `json:"platform"` // "qq" / "wx"
	QQ        string `json:"qq"`
	UIN       string `json:"uin"`
	GID       string `json:"gid"`
	OpenID    string `json:"openId"`
	Avatar    string `json:"avatar"`
	Status    string `json:"status"` // "online" / "offline"
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// InitStore 初始化数据目录与全局配置
func InitStore(dir string) {
	dataDir = dir
	os.MkdirAll(dataDir, 0755)
	globalConfig = config.DefaultGlobalConfig()
	loadGlobalConfig()
	loadAccounts()
}

// ---- JSON 原子读写（对标 Node json-db.js） ----

func readJSONFile(filePath string, fallback interface{}) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, fallback)
}

func writeJSONFileAtomic(filePath string, data interface{}) error {
	tmpPath := filePath + ".tmp"
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmpPath, jsonData, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, filePath)
}

func storeFilePath() string {
	return filepath.Join(dataDir, "store.json")
}

func accountsFilePath() string {
	return filepath.Join(dataDir, "accounts.json")
}

// ---- 全局配置加载/保存 ----

func loadGlobalConfig() {
	filePath := storeFilePath()
	var cfg config.GlobalConfig
	if err := readJSONFile(filePath, &cfg); err != nil {
		globalConfig = config.DefaultGlobalConfig()
		return
	}
	// 合并默认值
	if cfg.AccountConfigs == nil {
		cfg.AccountConfigs = make(map[string]config.AccountConfig)
	}
	if cfg.UserDefaultAccountPlans == nil {
		cfg.UserDefaultAccountPlans = make(map[string]config.AccountConfig)
	}
	if cfg.SystemConfig.ClientVersion == "" {
		cfg.SystemConfig = config.DefaultSystemConfig()
	}
	if cfg.CaptureConfig.APIBase == "" {
		cfg.CaptureConfig = config.DefaultCaptureConfig()
	}
	if cfg.GlobalWxConfig.APIBase == "" {
		cfg.GlobalWxConfig = config.DefaultGlobalWxConfig()
	}
	if cfg.DeviceProtocol.UserAgent == "" {
		cfg.DeviceProtocol = config.DefaultDeviceProtocol()
	}
	globalConfig = cfg
}

func saveGlobalConfig() error {
	return writeJSONFileAtomic(storeFilePath(), globalConfig)
}

// GetGlobalConfig 获取全局配置
func GetGlobalConfig() config.GlobalConfig {
	mu.RLock()
	defer mu.RUnlock()
	return globalConfig
}

// GetAccountConfig 获取指定账号配置（不存在则返回默认值）
func GetAccountConfig(accountID string) config.AccountConfig {
	mu.RLock()
	defer mu.RUnlock()
	cfg, ok := globalConfig.AccountConfigs[accountID]
	if !ok {
		return globalConfig.DefaultAccountConfig
	}
	return cfg
}

// GetAutomation 获取自动化开关
func GetAutomation(accountID string) config.AutomationConfig {
	return GetAccountConfig(accountID).Automation
}

// GetIntervals 获取巡查间隔
func GetIntervals(accountID string) config.IntervalsConfig {
	return GetAccountConfig(accountID).Intervals
}

// SetAutomation 设置自动化开关
func SetAutomation(accountID string, key string, value bool) error {
	mu.Lock()
	defer mu.Unlock()
	cfg, ok := globalConfig.AccountConfigs[accountID]
	if !ok {
		cfg = globalConfig.DefaultAccountConfig
	}
	switch key {
	case "farm":
		cfg.Automation.Farm = value
	case "friend":
		cfg.Automation.Friend = value
	case "friend_steal":
		cfg.Automation.FriendSteal = value
	case "friend_help":
		cfg.Automation.FriendHelp = value
	case "task":
		cfg.Automation.Task = value
	default:
		return fmt.Errorf("unknown automation key: %s", key)
	}
	globalConfig.AccountConfigs[accountID] = cfg
	return saveGlobalConfig()
}

// SetIntervals 保存巡查间隔（key: steal/help/farm, value[0]=min,value[1]=max）
func SetIntervals(accountID string, key string, minVal, maxVal int) error {
	mu.Lock()
	defer mu.Unlock()
	cfg, ok := globalConfig.AccountConfigs[accountID]
	if !ok {
		cfg = globalConfig.DefaultAccountConfig
	}
	switch key {
	case "steal":
		cfg.Intervals.StealMin = minVal
		cfg.Intervals.StealMax = maxVal
	case "help":
		cfg.Intervals.HelpMin = minVal
		cfg.Intervals.HelpMax = maxVal
	case "farm":
		cfg.Intervals.FarmMin = minVal
		cfg.Intervals.FarmMax = maxVal
	default:
		return fmt.Errorf("unknown interval key: %s", key)
	}
	globalConfig.AccountConfigs[accountID] = cfg
	return saveGlobalConfig()
}

// SetFriendBlacklist 设置好友拉黑列表（对齐 Node store.js setFriendBlacklist），
// 纯 gid 列表；返回是否包含（用于前端黑名单明细）。
func SetFriendBlacklist(accountID string, gids []int64) error {
	mu.Lock()
	defer mu.Unlock()
	cfg, ok := globalConfig.AccountConfigs[accountID]
	if !ok {
		cfg = globalConfig.DefaultAccountConfig
	}
	// 去重排序
	seen := map[int64]bool{}
	out := make([]int64, 0, len(gids))
	for _, g := range gids {
		if g <= 0 || seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	cfg.FriendBlacklist = out
	globalConfig.AccountConfigs[accountID] = cfg
	return saveGlobalConfig()
}

// ToggleFriendBlacklist 切换某一好友拉黑状态；返回操作后的 gid 列表与是否新增（true=已拉黑）。
func ToggleFriendBlacklist(accountID string, gid int64) ([]int64, bool, error) {
	cfg := GetAccountConfig(accountID)
	list := cfg.FriendBlacklist
	idx := -1
	for i, g := range list {
		if g == gid {
			idx = i
		}
	}
	if idx >= 0 {
		list = append(list[:idx], list[idx+1:]...)
		return list, false, SetFriendBlacklist(accountID, list)
	}
	list = append(list, gid)
	return list, true, SetFriendBlacklist(accountID, list)
}

// GetAutoReconnect 获取指定账号掉线自动重连配置（不存在则返回默认值）
func GetAutoReconnect(accountID string) config.AutoReconnectConfig {
	return GetAccountConfig(accountID).AutoReconnect
}

// SetAutoReconnect 设置指定账号自动重连配置
func SetAutoReconnect(accountID string, enabled bool, delayMin, maxAttempts int) error {
	mu.Lock()
	defer mu.Unlock()
	cfg, ok := globalConfig.AccountConfigs[accountID]
	if !ok {
		cfg = globalConfig.DefaultAccountConfig
	}
	if delayMin <= 0 {
		delayMin = 3
	}
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	cfg.AutoReconnect = config.AutoReconnectConfig{
		Enabled:             enabled,
		ReconnectDelayMin:   delayMin,
		ReconnectMaxAttempts: maxAttempts,
	}
	globalConfig.AccountConfigs[accountID] = cfg
	return saveGlobalConfig()
}

// ---- 账号管理 ----

func loadAccounts() {
	filePath := accountsFilePath()
	var data struct {
		Accounts []Account `json:"accounts"`
	}
	if err := readJSONFile(filePath, &data); err != nil {
		accounts = []Account{}
		return
	}
	accounts = data.Accounts
	if accounts == nil {
		accounts = []Account{}
	}
}

func saveAccounts() error {
	return writeJSONFileAtomic(accountsFilePath(), struct {
		Accounts []Account `json:"accounts"`
	}{Accounts: accounts})
}

// GetAccounts 获取所有账号
func GetAccounts() []Account {
	mu.RLock()
	defer mu.RUnlock()
	result := make([]Account, len(accounts))
	copy(result, accounts)
	return result
}

// GetAccountByID 按 ID 查找账号
func GetAccountByID(id string) *Account {
	mu.RLock()
	defer mu.RUnlock()
	for i := range accounts {
		if accounts[i].ID == id {
			return &accounts[i]
		}
	}
	return nil
}

// AddOrUpdateAccount 添加或更新账号（对标 Node addOrUpdateAccount）
func AddOrUpdateAccount(acc Account) ([]Account, error) {
	mu.Lock()
	defer mu.Unlock()
	for i := range accounts {
		if accounts[i].ID == acc.ID {
			accounts[i] = acc
			if err := saveAccounts(); err != nil {
				return nil, err
			}
			result := make([]Account, len(accounts))
			copy(result, accounts)
			return result, nil
		}
	}
	accounts = append(accounts, acc)
	if err := saveAccounts(); err != nil {
		return nil, err
	}
	result := make([]Account, len(accounts))
	copy(result, accounts)
	return result, nil
}

// DeleteAccount 删除账号
func DeleteAccount(accountID string) error {
	mu.Lock()
	defer mu.Unlock()
	for i := range accounts {
		if accounts[i].ID == accountID {
			accounts = append(accounts[:i], accounts[i+1:]...)
			return saveAccounts()
		}
	}
	return fmt.Errorf("account not found: %s", accountID)
}

// ---- 系统配置 ----

// GetSystemConfig 获取系统配置
func GetSystemConfig() config.SystemConfig {
	mu.RLock()
	defer mu.RUnlock()
	return globalConfig.SystemConfig
}

// GetGatewayConfig 获取网关配置（环境变量优先）
func GetGatewayConfig() config.GatewayConfig {
	cfg := config.DefaultGatewayConfig()
	if v := os.Getenv("FARM_TSDK_GAME_ID"); v != "" {
		cfg.TSDKGameID = v
	}
	if v := os.Getenv("FARM_TSDK_APP_KEY"); v != "" {
		cfg.TSDKAppKey = v
	}
	if v := os.Getenv("FARM_TSDK_ACE_ENABLED"); v == "false" {
		cfg.ACEEabled = false
	}
	if v := os.Getenv("ADMIN_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.AdminPort)
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" {
		cfg.AdminPassword = v
	}
	return cfg
}

// GetActiveAccountID 返回当前活跃账号ID（空表示未设）
func GetActiveAccountID() string {
	cfg := GetGlobalConfig()
	return cfg.ActiveAccountID
}

// SetActiveAccount 设置当前活跃账号
func SetActiveAccount(accountID string) error {
	mu.Lock()
	defer mu.Unlock()
	globalConfig.ActiveAccountID = accountID
	return saveGlobalConfig()
}

// GetDefaultAccountID 返回默认账号：优先活跃账号，否则第一个账号
func GetDefaultAccountID() string {
	if id := GetActiveAccountID(); id != "" {
		if GetAccountByID(id) != nil {
			return id
		}
	}
	accs := GetAccounts()
	if len(accs) > 0 {
		return accs[0].ID
	}
	return ""
}
