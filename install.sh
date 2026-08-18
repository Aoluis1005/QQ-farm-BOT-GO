#!/usr/bin/env bash
# ============================================================
#  QQ Farm Bot GO 一键部署脚本
#
#  用法:  sudo bash install.sh
#
#  脚本会自动完成:
#    1. 编译程序（目录里已有可执行文件则直接复用）
#    2. 安装到 /opt/go-farm-bot（自动带上 game-config 图片素材）
#    3. 注册并启动 systemd 服务
#    4. 打印访问地址
#  用户无需配置任何东西，部署完即可使用。
# ============================================================
set -e

cd "$(dirname "$0")"
SRC="$(pwd)"
DES=/opt/go-farm-bot

echo "=========================================="
echo " QQ Farm Bot GO 一键部署"
echo "=========================================="

# ---- 1. 编译 ----
if [ -x "./go-farm-bot" ] && [ -s "./go-farm-bot" ]; then
  echo "[1/4] 使用已有程序文件: ./go-farm-bot"
  BIN="$SRC/go-farm-bot"
else
  echo "[1/4] 编译程序..."
  if ! command -v go >/dev/null 2>&1; then
    echo "未检测到 Go，尝试自动安装..."
    sudo apt-get update -y >/dev/null 2>&1 || true
    sudo apt-get install -y golang-go >/dev/null 2>&1
  fi
  go build -o go-farm-bot .
  BIN="$SRC/go-farm-bot"
fi

# ---- 2. 安装程序 + 图片素材 ----
echo "[2/4] 安装程序与素材到 $DES ..."
sudo mkdir -p "$DES"
sudo cp -rf "$BIN" "$SRC/game-config" "$DES/"
sudo chmod +x "$DES/go-farm-bot"

# ---- 3. 注册系统服务 ----
echo "[3/4] 注册并启动系统服务..."
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

# ---- 4. 完成 ----
echo "[4/4] 部署完成！"
IP=$(hostname -I 2>/dev/null | awk '{print $1}')
echo "访问地址: http://${IP:-<服务器IP>}:3009"
echo ""
echo "提示: 后续更新可重新运行  sudo bash install.sh "
