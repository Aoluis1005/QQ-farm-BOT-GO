# QQ Farm Bot Go - Docker 构建（不影响 install.sh 一键部署）
# 用法：
#   docker build --build-arg VERSION=<git短哈希> -t go-farm-bot .
#   docker run -d --name go-farm-bot -p 3009:3009 -e ADMIN_PORT=3009 \
#     -v qq-farm-bot-data:/root/.qq-farm-bot go-farm-bot
#
# 说明：
# - 前端 web/dist 已入库（go:embed 打进二进制），无需在容器内构建前端；
# - 运行数据全部在 $HOME/.qq-farm-bot（账号/配置/日志），挂 volume 持久化；
# - game-config（素材配置）与 yyb-resource（YYB 扫码）相对工作目录读取，随镜像复制。

# ---- 构建阶段 ----
FROM golang:1.25-alpine AS build
ARG VERSION=dev
# 国内服务器访问 proxy.golang.org 会超时（实测 i/o timeout），改用国内代理
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /src
# 先拷全量源码（go.mod 有 replace yyb_go => ./yyb_go 本地模块，download 前必须存在）
COPY . .
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
