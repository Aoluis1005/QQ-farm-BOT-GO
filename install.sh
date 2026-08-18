#!/usr/bin/env bash
# ============================================================
#  QQ Farm Bot GO 一键部署脚本
#
#  用法:  sudo bash install.sh
#
#  脚本会自动完成:
#    1. 构建前端（保证 embed 进二进制的是仓库当前版本，杜绝旧 dist 导致的样式丢失）
#    2. 编译后端程序
#    3. 安装到 /opt/go-farm-bot（自动带上 game-config 图片素材）
#    4. 注册并启动 systemd 服务
#    5. 打印访问地址
#  用户无需配置任何东西，部署完即可使用。
# ============================================================
set -e

cd "$(dirname "$0")"
SRC="$(pwd)"
DES=/opt/go-farm-bot

echo "=========================================="
echo " QQ Farm Bot GO 一键部署"
echo "=========================================="

# ---- 1. 构建前端（强制，不复用旧产物） ----
echo "[1/5] 构建前端..."
if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
  echo "  未检测到 Node.js，尝试自动安装..."
  sudo apt-get update -y >/dev/null 2>&1 || true
  sudo apt-get install -y nodejs npm >/dev/null 2>&1 || { echo "Node.js 安装失败，无法构建前端"; exit 1; }
fi
cd "$SRC/web"
npm ci >/dev/null 2>&1 || npm install >/dev/null 2>&1
npm run build
cd "$SRC"

# ---- 2. 编译后端程序（每次都重新编译，确保 embed 的就是刚构建的前端） ----
echo "[2/5] 编译程序..."
if ! command -v go >/dev/null 2>&1; then
  echo "  未检测到 Go，尝试自动安装..."
  sudo apt-get update -y >/dev/null 2>&1 || true
  sudo apt-get install -y golang-go >/dev/null 2>&1
fi
go build -o go-farm-bot .
BIN="$SRC/go-farm-bot"

# ---- 3. 安装程序 + 图片素材 ----
echo "[3/5] 安装程序与素材到 $DES ..."
sudo mkdir -p "$DES"
sudo cp -rf "$BIN" "$SRC/game-config" "$DES/"
sudo chmod +x "$DES/go-farm-bot"

# ---- 4. 注册系统服务 ----
echo "[4/5] 注册并启动系统服务..."
sudo tee /etc/systemd/system/go-farm-bot.service >/dev/null <<'SVC'
[Unit]
Description=QQ Farm Bot Go
After=network.target
[Service]
Type=simple
User=root
WorkingDirectory=/opt/go-farm-bot
ExecStart=/opt/go-farm-bot/go-farm-bot
Environment=ADMIN_PORT=3009
Restart=on-failure
RestartSec=5
[Install]
WantedBy=multi-user.target
SVC
sudo systemctl daemon-reload
sudo systemctl enable --now go-farm-bot

# ---- 5. 完成 ----
echo "[5/5] 部署完成！"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
echo "访问地址: http://${IP:-<服务器IP>}:3009"
echo ""
echo "提示: 后续更新可重新运行  sudo bash install.sh "
