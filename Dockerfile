# QQ Farm Bot Go - Docker 构建（不影响 install.sh 一键部署）
# 用法：
#   docker build --build-arg VERSION=<git短哈希> -t go-farm-bot .
#   docker run -d --name go-farm-bot -p 3009:3009 -e ADMIN_PORT=3009 \
#     -v qq-farm-bot-data:/root/.qq-farm-bot go-farm-bot
#
# 说明：
# - 前端在镜像内用 Node 重新构建（vite build → 新 web/dist），
#   保证打进二进制的始终是当前源码的最新前端，杜绝旧 dist（与 install.sh 行为一致）；
# - 运行数据全部在 $HOME/.qq-farm-bot（账号/配置/日志），挂 volume 持久化；
# - game-config（素材配置）与 yyb-resource（YYB 扫码）相对工作目录读取，随镜像复制。

# ---- 前端构建阶段（每次构建都重建前端，防旧 dist 进镜像） ----
FROM node:22-alpine AS web-build
WORKDIR /web
# 国内服务器访问 npm 官方源可能超时，改用国内镜像（与 Go 的 goproxy.cn 同理）
RUN npm config set registry https://registry.npmmirror.com
COPY web/package.json web/package-lock.json ./
RUN npm ci
# 拷贝前端源码与配置（.dockerignore 已放行 web/src）
COPY web/index.html web/vite.config.js ./
COPY web/src ./src
RUN npm run build
# 产物：/web/dist（新构建的前端）

# ---- Go 构建阶段 ----
FROM golang:1.25-alpine AS build
ARG VERSION=dev
# 国内服务器访问 proxy.golang.org 会超时（实测 i/o timeout），改用国内代理
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
# 先拷全量源码（go.mod 有 replace yyb_go => ./yyb_go 本地模块，download 前必须存在）
COPY . .
# 用刚构建的前端覆盖仓库内 web/dist（杜绝旧前端进二进制）
COPY --from=web-build /web/dist /src/web/dist
RUN go mod download
# 注入版本号（覆盖仓库默认 dev），与服务器发布流程一致
RUN printf 'package main\n\nvar buildVersion = "%s"\nvar buildTime = "%s"\n' "$VERSION" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > version.go
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -ldflags="-s -w" -o /out/go-farm-bot .

# ---- 运行阶段 ----
FROM alpine:3.20
ENV HOME=/root
WORKDIR /app
COPY --from=build /out/go-farm-bot /app/go-farm-bot
COPY game-config /app/game-config
COPY yyb-resource /app/yyb-resource
VOLUME ["/root/.qq-farm-bot"]
EXPOSE 3009
CMD ["/app/go-farm-bot"]
