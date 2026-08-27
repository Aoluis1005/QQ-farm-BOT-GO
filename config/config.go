package config

// ============================================================
// 对应 Node core/src/config/config.js + models/store.js
// 严格复刻 DEFAULT_ACCOUNT_CONFIG 全部字段
// ============================================================

// AutomationConfig 自动化开关全集（对标 Node DEFAULT_AUTOMATION）
type AutomationConfig struct {
	Farm                    bool `json:"farm"`                      // 自动种植收获
	FarmPush                bool `json:"farm_push"`                // 推送触发巡田
	LandUpgrade             bool `json:"land_upgrade"`            // 自动升级土地
	Friend                  bool `json:"friend"`                  // 自动好友互动
	FriendHelpExpLimit      bool `json:"friend_help_exp_limit"` // 经验满只帮护主犬
	FriendTurboMode         bool   `json:"friend_turbo_mode"`      // 极速务农
	FriendTurboScheduled    bool   `json:"friend_turbo_scheduled"`  // 定时极速务农（仅定时段内生效）
	FriendTurboScheduleTime string `json:"friend_turbo_schedule_time"` // 极速时段 "HH:mm-HH:mm"（北京时间）
	FriendSteal             bool `json:"friend_steal"`            // 自动偷菜
	FriendHelp              bool `json:"friend_help"`             // 自动帮忙
	FriendBad               bool `json:"friend_bad"`              // 自动捣乱
	FriendGoldenBug         bool `json:"friend_golden_bug"`     // 自动放黄金虫
	Task                    bool `json:"task"`                     // 自动做任务
	FertilizerGift          bool `json:"fertilizer_gift"`       // 自动填充化肥
	FertilizerBuyOrganic    bool `json:"fertilizer_buy_organic"` // 自动购买有机化肥
	FertilizerBuyNormal     bool `json:"fertilizer_buy_normal"`  // 自动购买无机化肥
	MysteryAutoBuy          bool `json:"mystery_auto_buy"`      // 神秘商人自动购买
	Sell                    bool `json:"sell"`                     // 自动卖果实
	Fertilizer              string `json:"fertilizer"`           // 施肥模式 (smart_normal/smart_only/both/organic...)
	FertilizerMultiSeason   bool   `json:"fertilizer_multi_season"` // 多季补肥
	FertilizerSmartSeconds  int    `json:"fertilizer_smart_seconds"`  // 快成熟判定秒数
	FertilizerLandTypes     []string `json:"fertilizer_land_types"`   // 施肥范围土地类型
	SkipOwnWeedBug          bool   `json:"skip_own_weed_bug"`   // 不除自己草虫
	GoldenBugClear          bool   `json:"golden_bug_clear"`    // 自动清除黄金虫
}

// FertilizerLandTypes 施肥范围土地类型
var DefaultFertilizerLandTypes = []string{"purple", "gold", "black", "red", "normal"}

// IntervalsConfig 巡查间隔配置
type IntervalsConfig struct {
	Farm    int `json:"farm"`     // 农场巡查 min
	FarmMin int `json:"farmMin"`  // 农场巡查 min
	FarmMax int `json:"farmMax"`  // 农场巡查 max
	HelpMin int `json:"helpMin"`  // 帮忙巡查 min
	HelpMax int `json:"helpMax"`  // 帮忙巡查 max
	StealMin int `json:"stealMin"` // 偷菜巡查 min
	StealMax int `json:"stealMax"` // 偷菜巡查 max
}

// QuietHoursConfig 静默时段配置
type QuietHoursConfig struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"` // "01:00"
	End     string `json:"end"`   // "07:30"
}

// AutoCodeRefreshConfig 自动刷新 Code 配置
type AutoCodeRefreshConfig struct {
	Enabled        bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"` // 默认 60
}

// AutoReconnectConfig 掉线自动重连配置（字段名）
// 断线后延迟 reconnectDelayMin 分钟，走内置 YYB 换 code 重建连接；
// 重连计数器只在「手动停止/踢下线/删除账号」时清零，自动重连成功不清零；
// 调度前 current >= reconnectMaxAttempts 则停止重连。
// 默认值：开启、3 分钟延迟、3 次失败停止（用户可在扫码弹窗内修改）。
type AutoReconnectConfig struct {
	Enabled             bool `json:"enabled"`
	ReconnectDelayMin   int  `json:"reconnectDelayMin"`  // 默认 3：离线延迟分钟数
	ReconnectMaxAttempts int `json:"reconnectMaxAttempts"` // 默认 3：最多重连次数
}

// AccountConfig 单个账号的完整配置
type AccountConfig struct {
	Automation                    AutomationConfig  `json:"automation"`
	AutoCodeRefresh               AutoCodeRefreshConfig `json:"autoCodeRefresh"`
	AutoReconnect                 AutoReconnectConfig `json:"autoReconnect"`
	PlantingStrategy              string   `json:"plantingStrategy"`              // preferred/level/max_exp/...
	PreferredSeedID               int      `json:"preferredSeedId"`
	Prioritize2x2Crops          bool     `json:"prioritize2x2Crops"`
	FriendBadRetryDate           string   `json:"friendBadRetryDate"`
	Intervals                    IntervalsConfig `json:"intervals"`
	FriendQuietHours             QuietHoursConfig `json:"friendQuietHours"`
	KnownFriendGIDs              []int64  `json:"knownFriendGids"`
	FriendBlacklist              []int64  `json:"friendBlacklist"`
	PlantBlacklist               []int    `json:"plantBlacklist"`
	StealDelaySeconds            int      `json:"stealDelaySeconds"`            // 默认 1
	PlantOrderRandom             bool     `json:"plantOrderRandom"`             // 默认 true
	PlantDelaySeconds            int      `json:"plantDelaySeconds"`            // 默认 2
	FertilizerBuyOrganicCount    int      `json:"fertilizerBuyOrganicCount"`    // 默认 1
	FertilizerBuyOrganicThresholdHours int  `json:"fertilizerBuyOrganicThresholdHours"` // 默认 10
	FertilizerBuyNormalCount     int      `json:"fertilizerBuyNormalCount"`     // 默认 1
	FertilizerBuyNormalThresholdHours  int  `json:"fertilizerBuyNormalThresholdHours"`  // 默认 10
	FertilizerBuyCheckIntervalMinutes int  `json:"fertilizerBuyCheckIntervalMinutes"` // 默认 60
	MysteryAutoBuyCurrencies     []int    `json:"mysteryAutoBuyCurrencies"`
	BagSeedPriority              []int    `json:"bagSeedPriority"`
	BagSeedFallbackStrategy      string   `json:"bagSeedFallbackStrategy"`      // level/max_exp/...
	BagPriorityLandTypes         []string `json:"bagPriorityLandTypes"`
	AutoAcceptFriendMinLevel     int      `json:"autoAcceptFriendMinLevel"`     // 默认 0
	GoldenBugKeepCount           int      `json:"goldenBugKeepCount"`
	GoldenBugRoundLimit          int      `json:"goldenBugRoundLimit"`          // 默认 24
	FriendHelpExpExhausted       bool     `json:"friendHelpExpExhausted"`
}

// DefaultAccountConfig 返回默认账号配置（对标 Node DEFAULT_ACCOUNT_CONFIG）
func DefaultAccountConfig() AccountConfig {
	return AccountConfig{
		Automation: AutomationConfig{
			Farm:                   true,
			FarmPush:               true,
			LandUpgrade:            true,
			Friend:                 true,
			FriendHelpExpLimit:     true,
			FriendTurboMode:        false,
			FriendTurboScheduled:   false,
			FriendTurboScheduleTime: "",
			FriendSteal:            true,
			FriendHelp:             true,
			FriendBad:              false,
			FriendGoldenBug:        false,
			Task:                   true,
			FertilizerGift:         false,
			FertilizerBuyOrganic:   false,
			FertilizerBuyNormal:    false,
			MysteryAutoBuy:         false,
			Sell:                   true,
			Fertilizer:             "smart_normal",
			FertilizerMultiSeason:  true,
			FertilizerSmartSeconds: 300,
			SkipOwnWeedBug:         true,
			GoldenBugClear:         true,
			FertilizerLandTypes:    []string{"purple", "gold", "black", "red", "normal"},
		},
		AutoCodeRefresh: AutoCodeRefreshConfig{
			Enabled:        false,
			IntervalMinutes: 60,
		},
		AutoReconnect: AutoReconnectConfig{
			Enabled:             true,
			ReconnectDelayMin:   3,
			ReconnectMaxAttempts: 3,
		},
		PlantingStrategy:              "max_exp",
		PreferredSeedID:               0,
		Prioritize2x2Crops:          false,
		FriendBadRetryDate:           "",
		Intervals: IntervalsConfig{
			Farm:    2,
			FarmMin: 2,
			FarmMax: 5,
			HelpMin: 3,
			HelpMax: 5,
			StealMin: 3,
			StealMax: 5,
		},
		FriendQuietHours: QuietHoursConfig{
			Enabled: false,
			Start:   "01:00",
			End:     "07:30",
		},
		KnownFriendGIDs:              []int64{},
		FriendBlacklist:              []int64{},
		PlantBlacklist:               []int{20002, 20003, 20059, 20065, 20064, 20060, 20061, 29999},
		StealDelaySeconds:            1,
		PlantOrderRandom:             true,
		PlantDelaySeconds:            2,
		FertilizerBuyOrganicCount:    1,
		FertilizerBuyOrganicThresholdHours: 10,
		FertilizerBuyNormalCount:     1,
		FertilizerBuyNormalThresholdHours:  10,
		FertilizerBuyCheckIntervalMinutes: 60,
		MysteryAutoBuyCurrencies:     []int{},
		BagSeedPriority:              []int{},
		BagSeedFallbackStrategy:      "level",
		BagPriorityLandTypes:         DefaultFertilizerLandTypes,
		AutoAcceptFriendMinLevel:     0,
		GoldenBugKeepCount:           0,
		GoldenBugRoundLimit:          24,
		FriendHelpExpExhausted:       false,
	}
}

// GlobalConfig 全局配置（对标 Node store.js globalConfig）
type GlobalConfig struct {
	AccountConfigs           map[string]AccountConfig `json:"accountConfigs"`
	DefaultAccountConfig      AccountConfig         `json:"defaultAccountConfig"`
	UserDefaultAccountPlans  map[string]AccountConfig `json:"userDefaultAccountPlans"`
	DefaultPlanEnabled       bool                 `json:"defaultPlanEnabled"`       // 新账号自动应用默认方案
	DefaultPlanUpdatedAt     int64                `json:"defaultPlanUpdatedAt"`     // 默认方案最后保存时间
	AdminPasswordHash        string               `json:"adminPasswordHash"`
	SystemConfig             SystemConfig         `json:"systemConfig"`
	CaptureConfig           CaptureConfig        `json:"captureConfig"`
	GlobalWxConfig          GlobalWxConfig       `json:"globalWxConfig"`
	DeviceProtocol          DeviceProtocol       `json:"deviceProtocol"`
	ActiveAccountID         string               `json:"activeAccountId"`
}

// SystemConfig 系统配置
type SystemConfig struct {
	ServerURL    string `json:"serverUrl"`
	ClientVersion string `json:"clientVersion"`
	Platform     string `json:"platform"`
	OS           string `json:"os"`
	// 离线通知（MeoW）：账号掉线/自动重连成功时推送手机
	OfflineNotifyEnabled   bool   `json:"offlineNotifyEnabled"`
	OfflineNotifyNick      string `json:"offlineNotifyNick"`
	OfflineNotifyCooldownMin int  `json:"offlineNotifyCooldownMin"`
	// 定时收益推送（MeoW）：每天北京时间指定时刻推一次今日收益
	DailyReportEnabled bool   `json:"dailyReportEnabled"`
	DailyReportTime    string `json:"dailyReportTime"` // 格式 HH:MM（北京时间）
	// Bark 推送（可选）：与 MeoW 并行，同一触发事件（掉线/重连/日报）推送到 iPhone
	BarkEnabled bool   `json:"barkEnabled"`
	BarkKey     string `json:"barkKey"`
}

// CaptureConfig 抓包服务配置
type CaptureConfig struct {
	Enabled         bool   `json:"enabled"`
	APIBase         string `json:"apiBase"`
	APIToken        string `json:"apiToken"`
	AutoImportQqGids bool   `json:"autoImportQqGids"`
}

// GlobalWxConfig 微信配置
type GlobalWxConfig struct {
	Enabled            bool   `json:"enabled"`
	APIBase            string `json:"apiBase"`
	APIKey             string `json:"apiKey"`
	ProxyAPIUrl        string `json:"proxyApiUrl"`
	AppID              string `json:"appId"`
	AutoAddAccount     bool   `json:"autoAddAccount"`
	UserIsolation      bool   `json:"userIsolation"`
	AutoReconnect      bool   `json:"autoReconnect"`
	ReconnectDelayMin  int    `json:"reconnectDelayMin"`
	ReconnectMaxAttempts int  `json:"reconnectMaxAttempts"`
}

// DeviceProtocol 设备协议配置
type DeviceProtocol struct {
	Enabled     bool   `json:"enabled"`
	UserAgent   string `json:"userAgent"`
	DeviceModel string `json:"deviceModel"`
	DeviceBrand string `json:"deviceBrand"`
	DeviceMac   string `json:"deviceMac"`
	DeviceID    string `json:"deviceId"`
	IMEI        string `json:"imei"`
}

// GatewayConfig 网关连接配置（对标 Node config.js）
type GatewayConfig struct {
	WsURL             string `json:"wsUrl"`             // wss://gate-obt.nqf.qq.com/prod/ws
	ClientVersion      string `json:"clientVersion"`      // 1.13.3.14_20260827
	HeartbeatIntervalMs int    `json:"heartbeatIntervalMs"` // 25000
	FarmCheckIntervalMs int    `json:"farmCheckIntervalMs"` // 3000
	FriendCheckIntervalMs int   `json:"friendCheckIntervalMs"` // 8000
	TSDKEnabled        bool   `json:"tsdkEnabled"`
	ACEEabled          bool   `json:"aceEnabled"`
	TSDKGameID         string `json:"tsdkGameId"`
	TSDKAppKey         string `json:"tsdkAppKey"`
	AdminPort          int    `json:"adminPort"`      // 默认 3007
	AdminPassword      string `json:"adminPassword"`
}

// DefaultGatewayConfig 返回默认网关配置（对标 Node config.js）
func DefaultGatewayConfig() GatewayConfig {
	return GatewayConfig{
		WsURL:              "wss://gate-obt.nqf.qq.com/prod/ws",
		ClientVersion:       "1.13.3.14_20260827",
		HeartbeatIntervalMs: 25000,
		FarmCheckIntervalMs: 3000,
		FriendCheckIntervalMs: 8000,
		TSDKEnabled:         true,
		ACEEabled:          true,
		TSDKGameID:         "", // 从环境变量 FARM_TSDK_GAME_ID
		TSDKAppKey:         "", // 从环境变量 FARM_TSDK_APP_KEY
		AdminPort:           3007,
		AdminPassword:       "admin",
	}
}

// DefaultSystemConfig 默认系统配置
func DefaultSystemConfig() SystemConfig {
	return SystemConfig{
		ServerURL:    "",
		ClientVersion: "1.13.3.14_20260827",
		Platform:     "qq",
		OS:           "iOS",
		OfflineNotifyEnabled: false,
		OfflineNotifyNick:    "",
		OfflineNotifyCooldownMin: 10,
		DailyReportEnabled: false,
		DailyReportTime:    "21:00",
	}
}

// DefaultCaptureConfig 默认抓包配置
func DefaultCaptureConfig() CaptureConfig {
	return CaptureConfig{
		Enabled:         false,
		APIBase:         "https://api.example.com",
		APIToken:        "",
		AutoImportQqGids: true,
	}
}

// DefaultGlobalWxConfig 默认微信配置
func DefaultGlobalWxConfig() GlobalWxConfig {
	return GlobalWxConfig{
		Enabled:            false,
		APIBase:            "https://code.z74d.top/api",
		APIKey:             "",
		ProxyAPIUrl:        "https://code.z74d.top/api",
		AppID:              "wx5306c5978fdb76e4",
		AutoAddAccount:     true,
		UserIsolation:      false,
		AutoReconnect:      false,
		ReconnectDelayMin:  5,
		ReconnectMaxAttempts: 3,
	}
}

// DefaultDeviceProtocol 默认设备协议
func DefaultDeviceProtocol() DeviceProtocol {
	return DeviceProtocol{
		Enabled:     false,
		UserAgent:   "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15",
		DeviceModel: "iPhone14,2",
		DeviceBrand: "Apple",
		DeviceMac:   "",
		DeviceID:    "",
		IMEI:        "",
	}
}

// DefaultGlobalConfig 返回默认全局配置
func DefaultGlobalConfig() GlobalConfig {
	return GlobalConfig{
		AccountConfigs:          make(map[string]AccountConfig),
		DefaultAccountConfig:     DefaultAccountConfig(),
		UserDefaultAccountPlans: make(map[string]AccountConfig),
		DefaultPlanEnabled:      true, // 新账号自动应用默认方案
		SystemConfig:            DefaultSystemConfig(),
		CaptureConfig:           DefaultCaptureConfig(),
		GlobalWxConfig:          DefaultGlobalWxConfig(),
		DeviceProtocol:          DefaultDeviceProtocol(),
	}
}
