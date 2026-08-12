# QQ 农场 BOT：Go 版相对 Node 参考版的功能缺口分析（NODE-GO-GAP）

分析日期：2026-08-12
范围：用户可见、自用的核心游戏功能；已排除后台/多用户/运营向（卡密、多用户、两级管理员、公告、登录日志、设备协议、wxlogin-config、anti-resale、proxy、yyb 资源）。
结论均经两端源码逐项核对。

---

## A. 用户可见的核心功能缺口（建议补）

| # | 功能 | Node 接口/位置 | Go 现状 | 说明/优先级 |
|---|------|----------------|---------|-------------|
| 1 | **护主犬同气礼包：查询+领取** | `GET /api/dog/gifts` + `POST /api/dog/gifts/claim`（`controllers/admin-dog-routes.js`；service `services/dog-gifts.js`：`DogService.GetDogInfo` f7 可领数量 / `DogService.ClaimSkillGifts` f3 领取数） | **无任何对应**。Go 仅在 `stats.go` 记录 `TongQiGift`(物品101351) 被动计数，前端仅收入卡片显示数量（`web/index.html:875 🎁 同气礼包`），**无领取入口、无后端查询/领取接口** | 高。用户可直接领取同气礼包的功能整体缺失 |
| 2 | **抓包 capture 全套（含账号"抓包登录"接入方式）** | `GET/POST /api/capture/config`、`/api/capture/start`、`/api/capture/stop`、`/api/capture/sessions`、`/api/capture/sessions/:flowId/complete`（`controllers/admin-capture-routes.js`）+ 公网 `/api/public/capture-certificate/:flowId/:token`；前端接入 `components/AccountModal.vue`"抓包登录" + `views/AdminPanel.vue` 配置 | **config 仅有定义无实现**。`config/config.go:203` 定义了 `type CaptureConfig` 及 `DefaultCaptureConfig()`，但全仓**零路由、零实现**（grep capture 只在 config），前端 `web/index.html` 无抓包 | 高。用户关注的账号接入方式之一与活动/资源包抓取能力被砍 |
| 3 | **化肥手动购买** | `POST /api/fertilizer/buy` + `POST /api/fertilizer/check-and-buy`（`controllers/admin-farm-resource-routes.js`，buyFertilizer type/count） | **无手动购买路由**。Go 只有自动买逻辑（`automation.go:1280 doCheckAndBuyFertilizer` / `buyMallFertilizer`），用户无法手动补化肥 | 中。自动买虽覆盖常规，但缺少用户主动购买入口 |
| 4 | **奇遇礼莲抽奖（helu/draw）** | `POST /api/activity/helu/draw`（`controllers/admin-helu-activity-routes.js`；`services/activity.js:1708 drawHeluGiftLotus`，免费+付费、每日"今日剩余/已抽"次数管理）；store `web/src/stores/activity.ts:314 drawHelu` | **无**。Go 活动路由（`activity_api.go:33-45`）无 helu/draw；全仓无 `HELU_DRAW`/draw_result 协议处理 | 中。注意：Node 前端 `components/activity/HeluDrawPanel.vue` 虽已创建但全项目**未挂载**（views 无引用），即 Node UI 本身也未最终接入；后端协议能力 Go 同样缺失 |
| 5 | **每日礼包 daily-gifts** | `GET /api/daily-gifts`（`controllers/admin-bag-routes.js:179`，provider.getDailyGifts） | **无** | 中。每日签到/礼包自用有价值 |
| 6 | **设置项缺失** | `POST /api/settings/auto-code-refresh`、`/api/settings/auto-code-refresh/run`、`/api/settings/offline-reminder`、`/api/settings/offline-reminder/test`、`GET /api/settings/default`（`controllers/admin-settings-routes.js`） | Go `settings_api.go:22-29` 仅有 `/api/settings`、`/save`、`/automation`、`/default-plan(+import/apply/reset)`、`/seeds`；**缺以上 5 个** | 中-低。auto-code-refresh 对扫码登录自动刷新 QQ code 有运维价值；offline-reminder 为离线推送提醒 |

---

## B. 可能是组织结构差异，需确认（功能等价 / API 组织形式不同）

| # | 功能 | Node 接口/位置 | Go 现状 | 说明 |
|---|------|----------------|---------|------|
| 1 | 千星游记/荷风游记（passport/同气礼包战令）领取 | `POST /api/activity/helu/passport/claim`（`claimSeasonPassportRewards`，`services/activity.js:1513`） | `/api/activity/season/claim`（`activity_api.go:36`，`SeasonService.ClaimBattlePassRewards`，before/after `claimedLevels` 差额逻辑一致） | 等价，只是路径/前缀不同 |
| 2 | 节令小札（notes） | `POST /api/activity/helu/solar/claim`（`claimSolarTermsReward`） | `/api/activity/solar/claim`（`activity_api.go:38`，`ClaimSolarTerms` termId） | 等价 |
| 3 | 星砂商店兑换 | `POST /api/activity/helu/exchange`（`exchangeHeluShopItem`，`HELU_EXCHANGE_ACTIVITY_ID` + cmd=1 exchange_shop_operate） | `/api/activity/shop/exchange`（`activity_api.go:42`，`actExchangeActID=2026072702` 注释即 HELU_EXCHANGE，星砂1023，同 Operate cmd=1） | 等价。注意：Go 的 `/api/activity/shop`=星砂商店；而 Node 的 `/api/activity/shop`=南瓜随机店（见 C-1），语义不同但 Go 已实现 helu 兑换 |
| 4 | 观星礼录 | `GET /api/activity/guanxing` + `/api/activity/guanxing/claim`（`admin-guanxing-routes.js`；前端 `GuanxingActivityPanel`） | `/api/activity/guanxing` + `/api/activity/guanxing/claim`（`activity_api.go:39-40`） | 等价 |
| 5 | 青梅酿万金 | `POST /api/activity/qingmei/claim`、`/api/activity/qingmei/wine/sell`（`admin-helu-activity-routes.js`） | `/api/activity/qingmei`、`/api/activity/qingmei/claim`、`/api/activity/qingmei/wine`（`activity_api.go:43-45`；青酿换万金，领种子+酿酒出售） | 等价 |
| 6 | 单好友地块明细 | `GET /api/friend/:gid/lands`（`admin-friend-routes.js:136`） | `/api/friends/lands?gid=`（`profile_api.go:987 handleFriendLandsRoute`） | 等价，参数化路径 |
| 7 | 单好友护主犬信息 | `GET /api/friend/:gid/dog`（`admin-friend-routes.js:164`） | `/api/friend/dog/{gid}`（`friend_extra_api.go:109 handleFriendDogRoute`） | 等价，仅路径 gid/dog 先后不同 |
| 8 | 账号备注 | `POST /api/account/remark`（`admin-account-routes.js:137`，addOrUpdateAccount name + setRuntimeAccountName） | 经 `/api/accounts/{id}` PUT 保存 `acc.Name`（`account_api.go:180`）；前端 index.html 含"备注"UI | 等价 |
| 9 | 好友列表 | `GET /api/friends`（`admin-friend-routes.js:79`） | `/api/friends/list` + `/api/friends/requests` + `/api/friends/visitors`（`profile_api.go`） | 等价，拆分为多个子接口 |
| 10 | 账号状态 | `GET /api/status`（`admin-farm-resource-routes.js:49`，withLevelProgress） | `/api/home/profile` + `/api/accounts/active`（`home_api.go`/`account_api.go`） | 等价，组织方式不同 |
| 11 | 游戏素材/图鉴图片（CDN 代理） | `GET /api/game-asset`（`admin.js:391`，代理 CDN seed_images） | Go 用 `/game-config/` 静态目录 + `InitImageMap`（`game_images.go`，itemId→图片URL）+ `/api/open/v1/farm/code`（`game_config.go`） | 等价（本地资源替代 CDN 代理），满足前端配图 |
| 12 | 活动列表/分组 | `GET /api/activity/list`、`GET /api/activity/group/:id` | `/api/activity/list`、`/api/activity/group`(id 走 query)（`activity_api.go:33-34`） | 等价；另 Go 多出 `/api/activity/season`、`/api/activity/solar` 容器接口（Node 合并进 helu） |

---

## C. 自用可忽略（低价值 / 运维 / 前端休眠）

| # | 功能 | Node 位置 | Go 现状 | 说明 |
|---|------|-----------|---------|------|
| 1 | 南瓜随机活动商店 `buy`/`refresh` | `POST /api/activity/shop/buy`、`POST /api/activity/shop/refresh`（`admin-nangua-activity-routes.js`） | Go 无 buy/refresh（其 activity/shop 已用作星砂商店） | Node 注释明确 **"intentionally dormant on the frontend / 前端休眠"**，前端未接入，可忽略 |
| 2 | 主题切换 `POST /api/settings/theme` | `admin-settings-routes.js:283` | Go 无该后端接口 | Go 单页前端自带本地主题切换（index.html"主题"×6），无需后端；纯外观 |
| 3 | 调度器状态 `GET /api/scheduler` | `admin-public-info-routes.js` | Go 无 | 运维监控信息 |
| 4 | 版本/更新日志 `server-version`、`game-version`、`changelog` | `admin-helu-activity-routes.js`(`/api/server-version`)、`admin-public-info-routes.js`(`/api/game-version`、`/api/changelog`) | Go 无 | 元信息；Go 单页前端无更新检测需求 |
| 5 | 互动记录 `GET /api/interact-records` | `admin-friend-routes.js:122` | Go 无 | 历史互动回看，非核心 |
| 6 | 好友缓存清空 `POST /api/friends/clear-cache` | `admin-friend-routes.js:95` | Go 无 | 运维排障用 |
| 7 | 抓包会话 `GET /api/sessions` | `admin-capture-routes.js` | Go 无 | 属 capture 能力一并缺失（见 A-2） |

---

## 附加说明（非缺口，供参考）
- 自动化引擎：Node `core/worker.js` / `services/farming-orchestrator.js` 等 → Go `automation.go`（统一串行调度器、智能施肥、自动买化肥、护主犬巡查、神秘商人自动购买 `mystery_autobuy.go`）对标实现，未发现核心缺失。
- 化肥礼包自动开启：Go `fertilizer_gift.go` `runFertilizerGiftOnce` 已实现（Node 在 farm tick 内静默开启），等价。
- 离线自动重连：Go `reconnect.go` + `gwpool.go`（StartAutoReconnect）已实现，等价。

## 最关键 Top5 缺失
1. 护主犬同气礼包 查询+领取（`/api/dog/gifts` + `/claim`）— Go 完全无，仅被动计数展示
2. 抓包 capture 全套（config/start/stop/sessions/certificate）— Go 仅 config 定义，零实现，账号"抓包登录"接入方式整体缺失
3. 化肥手动购买（`/api/fertilizer/buy` + `check-and-buy`）— Go 仅自动买，无手动入口
4. 奇遇礼莲抽奖（`/api/activity/helu/draw`）— Go 无抽奖协议处理（注：Node 前端该面板亦未挂载 UI）
5. 每日礼包 `daily-gifts` + 设置增强项（auto-code-refresh/offline-reminder/default）— Go 缺失
