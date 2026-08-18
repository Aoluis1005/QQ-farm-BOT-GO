# ⚠️ 测试版 QQ 农场 Bot（Go）

> **本版本为测试版，可能存在 Bug**——遇到问题请 [提 Issue](https://github.com/Aoluis1005/QQ-farm-BOT-GO/issues)，我会尽快修复。
>
> ✅ 支持最新**七夕活动「鹊桥寄情」**：灵露喷洒、筑建鹊桥、档位奖励已可用
> ⚠️ **送香囊功能未完成**（协议已搭好，缺 cmd 参数，待真机抓包补全）

QQ 农场微信小游戏协议自动化挂机 Bot（**Go 单文件二进制版**，免 Node 环境）。

## 🚀 一键部署

> 只需两步，无需手动配环境/素材，部署完即可使用（图片、背包名称全部正常）。

```bash
git clone https://github.com/Aoluis1005/QQ-farm-BOT-GO.git
cd QQ-farm-BOT-GO
sudo bash install.sh
```

`install.sh` 会自动完成：编译程序 → 安装到 `/opt/go-farm-bot`（带 `game-config` 图片素材）→ 注册并启动 systemd 服务 → 打印访问地址。

部署完成后浏览器打开 **`http://<服务器IP>:3009`** 即可。后续重新部署/更新直接再跑一次 `sudo bash install.sh`。

## ✨ 功能特性

### 🌾 农场自动化
- 自动种植（多种策略：背包优先 / 最高等级 / 最大经验时 / 最大普肥经验时 / 收益最优）
- 自动收获 + 自动卖果实（分批出售，跳过不可售物品）
- 智能施肥（多季 / 快成熟 / 自动购买化肥）
- 除草 / 除虫 / 浇水 / 一键务农
- 土地升级 / 解锁

### 👥 好友自动化
- 巡查偷菜（支持偷菜黑名单 / 白名单）
- 帮忙务农（浇水 / 除草 / 除虫，每日经验上限自动停止）
- 放虫 / 放草 / 捉虫
- 护主犬同气礼包自动领取
- 好友农场拜访（Enter / Leave）

### 🎯 活动自动化
- 每日任务自动领取（成长任务 / 每日任务 / 主线任务）
- 活跃度奖励自动领取
- 图鉴一键购 + 点券奖励自动领取
- **商城免费礼包** + **每日分享礼包**自动领取
- 神秘商人自动购买（点券 / 金豆豆 / 金币）
- 自动接好友申请（P2 规划中）

### 🎋 七夕「鹊桥寄情」
| 功能 | 状态 |
|---|---|
| 灵露喷洒（自动触发鹊羽效果，收获鹊羽） | ✅ 可用 |
| 筑建鹊桥（3 档奖励：香囊 / 礼包 / 铭牌） | ✅ 可用 |
| 档位已领标记（前端正确显示可筑档） | ✅ 可用 |
| 送香囊 | ⚠️ 未完成（待真机抓包补全 cmd） |

### 🔧 系统能力
- Web 管理面板（首页 / 好友 / 商城 / 活动 / 分析 / 每日任务 / 设置）
- 自动重连（断线自动恢复）
- 网关心跳保活
- 操作日志（面板实时查看）
- 今日收益统计（金币 / 经验 / 操作次数）

## 🛠️ 技术栈

- **语言**：Go（单文件二进制，`CGO_ENABLED=0` 交叉编译）
- **协议**：QQ 农场微信小游戏网关（WebSocket + Protobuf）
- **前端**：Vue3 + Vite（`//go:embed` 打入二进制）
- **部署**：systemd / Docker / 裸跑均可

## 📦 快速开始

### 编译

```bash
# Go 1.20+
CGO_ENABLED=0 go build -o go-farm-bot .
```

### 运行

```bash
./go-farm-bot
# 默认监听 :3009
```

### 首次配置

1. 浏览器打开 `http://<服务器IP>:3009`
2. 进入管理面板 → **设置** → 设置管理密码（`/api/admin/setup`）
3. 添加账号 → 微信扫码 / 授权登录（YYB 渠道 = wx 平台）
4. 打开自动化开关，Bot 即开始挂机

### systemd 部署（推荐）

```ini
# /etc/systemd/system/go-farm-bot.service
[Unit]
Description=QQ Farm Bot (Go)
After=network.target

[Service]
WorkingDirectory=/opt/go-farm-bot
ExecStart=/opt/go-farm-bot/go-farm-bot
Environment=ADMIN_PORT=3009
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl enable --now go-farm-bot
```

## 📁 项目结构

```
.
├── main.go            # 入口 + 路由注册
├── gw/                # 网关连接（WebSocket / 心跳 / 重连 / 推送）
├── proto/             # Protobuf 编解码（对齐 Node proto/*.proto）
├── automation.go      # 自动化调度（种植 / 好友 / 任务循环）
├── task_auto.go       # 每日任务自动领取
├── activity_api.go    # 活动接口（含七夕：喷洒 / 筑桥 / 送香囊）
├── shop_api.go        # 商城（种子 / 宠物 / 道具）
├── home_api.go        # 首页数据（资料卡 / 今日收益 / 日志）
├── stats.go           # 今日收益统计
└── web/               # Vue3 前端源码 + dist（embed 进二进制）
```

## ⚙️ 配置说明

- **账号配置**：管理面板逐账号设置（自动化开关 / 种植策略 / 偷菜黑名单 / 间隔）
- **自动化开关**：农场 / 好友 / 每日任务 / 化肥 / 神秘商人 / 卖果实 独立控制
- **种植策略**：`bag_priority`（背包优先）/ `level` / `max_exp` / `max_fert_exp` / `max_profit` / `max_fert_profit` / `preferred`
- **活动种子排除**：活动种子（如星语铃花）自动排除，不会误买导致种植卡死

## ❓ 常见问题

**Q：管理面板登录提示"请输入密码"？**
首次访问先到「设置」页面设置密码（`/api/admin/setup`），之后用该密码登录。

**Q：首页 / 背包 / 好友无数据？**
检查账号是否已连接（首页「连接状态」）。WebSocket 网关 `platform` 只接受 `qq` / `wx`（YYB 扫码请填 `wx`）。

**Q：种植一直失败？**
检查金币是否足够；活动种子已自动排除；如仍失败请提 Issue 附上操作日志。

**Q：如何查看运行日志？**
管理面板「日志」页实时查看；systemd 部署可用 `journalctl -u go-farm-bot -f`。

## ⚠️ 免责声明

- 本仓库**不含**任何逆向工具、解包产物或敏感凭据（openid / 加密材料），请勿提交此类文件
- 仅用于学习与个人自动化研究，请遵守游戏用户协议
- 测试版随时可能有 Bug，请以 [Issue](https://github.com/Aoluis1005/QQ-farm-BOT-GO/issues) 反馈
- 本项目完全免费 · 开源共享 · 拒绝一切收费

## 📝 相关项目

- [Node 版（原版）](https://github.com/Aoluis1005/qq-farm-bot) —— 本项目的协议参考与功能对照
